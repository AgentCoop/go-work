package job

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	dummyChanCapacity = 100
)

var (
	defaultLogger   = make(LogLevelMap)
	DefaultLogLevel int
	dummyChan       = make(chan interface{}, dummyChanCapacity)
)

type job struct {
	taskMap       TaskMap
	logLevelMap   LogLevelMap
	logLevel      int
	failcount     uint32
	finishedcount uint32
	stateMu       sync.RWMutex
	state         JobState
	finalizeOnce  sync.Once
	timedoutFlag  bool
	timeout       time.Duration
	doneChan      chan struct{}
	oneshotDone   chan struct{}
	prereqWg      sync.WaitGroup
	value         interface{}
	stoponce      sync.Once
	interrby             *task //// task interrupted the job execution
	interrerr            interface{}
}

func (j *job) createTask(taskGen JobTask, index int, typ TaskType) *task {
	task := newTask(j, typ, index)
	init, run, fin := taskGen(j)
	task.init = init
	task.finalize = fin
	task.body = func() {
		go task.thread(func() {
			task.taskLoop(run)
		}, true)
	}
	j.taskMap[index] = task
	return task
}

func (j *job) AddTask(task JobTask) *task {
	return j.createTask(task, 1 + len(j.taskMap), Recurrent)
}

func (j *job) GetTaskByIndex(index int) *task {
	return j.taskMap[index]
}

func (j *job) AddTaskWithIdleTimeout(task JobTask, timeout time.Duration) *task {
	info := j.createTask(task, 1 + len(j.taskMap), Recurrent)
	info.idleTimeout = int64(timeout)
	return info
}

// Zero index is reserved for oneshot task
func (j *job) AddOneshotTask(task JobTask) {
	j.createTask(task, 0, Oneshot)
}

func (j *job) done() {
	j.doneChan <- struct{}{}
}

func (j *job) GetDoneChan() chan struct{} {
	return j.doneChan
}

// A job won't start until all its prerequisites are met
func (j *job) WithPrerequisites(sigs ...<-chan struct{}) *job {
	j.state = WaitingForPrereq
	j.prereqWg.Add(len(sigs))
	for _, sig := range sigs {
		s := sig
		go func() {
			for {
				select {
				case <-s:
					j.prereqWg.Done()
					return
				}
			}
		}()
	}
	return j
}

func (j *job) WithTimeout(t time.Duration) *job {
	j.timeout = t
	return j
}

func (j *job) prerun() {
	// Start timer that will finalize and mark the job as timed out if needed
	if j.timeout > 0 {
		go func() {
			ch := time.After(j.timeout)
			for {
				select {
				case <-ch:
					if j.state == RecurrentRunning || j.state == OneshotRunning {
						j.timedoutFlag = true
						j.Cancel()
					}
					return
				}
			}
		}()
	}
	if j.state == WaitingForPrereq {
		j.prereqWg.Wait()
	}
}

func (j *job) observer() {
	for {
		j.stateMu.RLock()
		if j.state == RecurrentRunning && j.finishedcount == uint32(len(j.taskMap)) {
			j.stateMu.RUnlock()
			j.state = Done
			j.done()
			return
		} else if j.state == Cancelled || j.state == Done {
			j.stateMu.RUnlock()
			return
		}
		j.stateMu.RUnlock()
	}
}

func (j *job) runOneshot() {
	j.state = OneshotRunning
	task := j.taskMap[0]
	if task.init != nil {
		task.thread(func() {
			task.init(task)
		}, false)
	}
	task.body()
}

func (j *job) runRecurrent() {
	j.stateMu.Lock()
	defer j.stateMu.Unlock()

	if j.state == Cancelled { return }
	j.state = RecurrentRunning

	go j.observer()

	for i, task := range j.taskMap {
		if i == 0 { continue } // skip oneshot task
		if task.init != nil {
			task.thread(func() {
				task.init(task)
			}, false)
		}
	}

	if j.state != RecurrentRunning { return }

	for i, task := range j.taskMap {
		if i == 0 { continue }
		task.body()
	}
}

// Concurrently executes all tasks in the job.
func (j *job) Run() chan struct{} {
	j.prerun()
	if j.hasOneshotTask() {
		j.runOneshot()
	} else {
		j.runRecurrent()
	}
	return j.doneChan
}

// Dispatch Done signal as soon as oneshot task finishes itself
func (j *job) RunInBackground() <-chan struct{} {
	doneDup := make(chan struct{})
	go func() {
		j.runOneshot()
		for {
			select {
			case <-j.oneshotDone:
				doneDup <- struct{}{}
				return
			default:
				if atomic.LoadUint32(&j.failcount) > 0 {
					return
				}
			}
		}
	}()
	return doneDup
}

func (j *job) finalize(state JobState) {
	// Run finalize routines
	for idx, task := range j.taskMap {
		fin := task.finalize
		if fin != nil {
			if idx == 0 && j.state == OneshotRunning { // concurrent tasks have not been started
				fin(task)
				break
			}
			fin(task)
		}
	}
	j.state = state
	j.done()
}

func (j *job) Cancel() {
	j.finalizeOnce.Do(func() {
		j.finalize(Cancelled)
	})
}

func (j *job) Finish() {
	j.finalizeOnce.Do(func() {
		j.finalize(Done)
	})
}

func (j *job) RegisterLogger(logger Logger) {
	m := logger()
	j.logLevelMap = m
	for level, item := range m {
		logchan := item.ch
		handler := item.rechandler
		go func() {
			for {
				select {
				case entry := <- logchan:
					handler(entry, level)
				default:
					if j.state == Cancelled || j.state == Done {
						return
					}
				}
			}
		}()
	}
}

func (j *job) Log(level int) chan<- interface{} {
	var m LogLevelMap
	var currlevel int

	if j.logLevelMap != nil {
		m = j.logLevelMap
		currlevel = j.logLevel
	} else {
		m = defaultLogger
		currlevel = DefaultLogLevel
	}

	if level > currlevel {
		if len(dummyChan) == dummyChanCapacity { // Drain the channel before re-using it to prevent blocking
			i := 0
			for _ = range dummyChan {
				i++
				if i == dummyChanCapacity {
					return dummyChan
				}
			}
		}
		return dummyChan
	}

	item, ok := m[level];
	if !ok {
		panic("invalid log level")
	}
	return item.ch
}

func (j *job) WasTimedOut() bool {
	return j.timedoutFlag
}

func (j *job) GetValue() interface{} {
	return j.value
}

func (j *job) SetValue(v interface{}) {
	j.value = v
}

func (j *job) GetState() JobState {
	return j.state
}

func (j *job) IsRunning() bool {
	return j.state == RecurrentRunning
}

func (j *job) IsCancelled() bool {
	return j.state == Cancelled
}

func (j *job) IsDone() bool {
	return j.state == Done
}

func (j *job) GetInterruptedBy() (*task, interface{}) {
	return j.interrby, j.interrerr
}
