package job

import (
	"sync"
	"sync/atomic"
	"time"
)

type jobState int

const (
	dummyChanCapacity = 100
)

var (
	defaultLogger   = make(LogLevelMap)
	dummyChan       = make(chan interface{}, dummyChanCapacity)
)

type job struct {
	taskMap       taskMap
	logLevelMap   LogLevelMap
	logLevel      int
	failcount     uint32
	finishedcount uint32
	statemux      sync.RWMutex
	state         jobState
	runonce       sync.Once
	finonce       sync.Once
	runInBg       bool
	timeout       time.Duration
	donechan      chan struct{}
	observerchan  chan struct{}
	taskdonechan  chan *task
	prereqWg      sync.WaitGroup
	value         interface{}
	stoponce      sync.Once
	interrby      *task // a task interrupted the job execution
	interrerr     interface{}
}

func (j *job) createTask(taskGen JobTask, index int, typ taskType) *task {
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
	j.donechan <- struct{}{}
}

func (j *job) TaskDoneNotify() <-chan *task {
	return j.taskdonechan
}

func (j *job) JobDoneNotify() chan struct{} {
	return j.donechan
}

// A job won't start until all its prerequisites are met
func (j *job) WithPrerequisites(sigs ...<-chan struct{}) {
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
}

func (j *job) WithTimeout(t time.Duration) {
	j.timeout = t
}

func (j *job) init() {
	// Observer's channel must never block a task.thread execution
	j.observerchan = make(chan struct{}, 3 * len(j.taskMap))
	j.taskdonechan = make(chan *task, len(j.taskMap))
}

func (j *job) prerun() {
	// Start timer that will finalize and mark the job as timed out if needed
	if j.timeout > 0 {
		go func() {
			ch := time.After(j.timeout)
			for {
				select {
				case <-ch:
					j.statemux.RLock()
					state := j.state
					j.statemux.RUnlock()
					switch {
					case state == RecurrentRunning, state == OneshotRunning:
						j.Cancel(ErrJobExecTimeout)
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
		select {
		case <- j.observerchan:
			fcount := atomic.LoadUint32(&j.finishedcount)
			j.statemux.RLock()
			state := j.state
			j.statemux.RUnlock()
			switch {
			case state == OneshotRunning && fcount == 1:
				j.runRecurrent()
				if j.runInBg {
					j.donechan <- NotifySig
				}
			case state == RecurrentRunning && fcount == uint32(len(j.taskMap)):
				j.state = Done
				j.done()
				return
			case state == Cancelled, state == Done:
				return
			}
		}
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
	if j.state == Cancelled { return }
	j.state = RecurrentRunning

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
	j.runonce.Do(func() {
		j.init()
		go j.observer()
		j.prerun()
		if j.hasOneshotTask() {
			j.runOneshot()
		} else {
			j.runRecurrent()
		}
	})
	return j.donechan
}

func (j *job) RunInBackground() <-chan struct{} {
	j.runonce.Do(func() {
		j.runInBg = true
		j.init()
		go j.observer()
		j.prerun()
		j.runOneshot()
	})
	return j.donechan
}

func (j *job) finalize(state jobState) {
	j.statemux.Lock()
	defer j.statemux.Unlock()
	prevs := j.state
	j.state = Finalizing
	for idx, task := range j.taskMap {
		fin := task.finalize
		if fin != nil {
			if idx == 0 && prevs == OneshotRunning { // recurrent tasks have not been started
				fin(task)
				break
			}
			fin(task)
		}
	}
	j.state = state
	j.done()
}

func (j *job) cancel(cancelledby *task, err interface{}) {
	j.finonce.Do(func() {
		j.interrby = cancelledby
		j.interrerr = err
		j.finalize(Cancelled)
	})
}

func (j *job) Cancel(err interface{}) {
	j.cancel(nil, err)
}

func (j *job) Finish() {
	j.finonce.Do(func() {
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
					j.statemux.RLock()
					state := j.state
					j.statemux.RUnlock()
					if state == Cancelled || state == Done {
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

func (j *job) GetValue() interface{} {
	return j.value
}

func (j *job) SetValue(v interface{}) {
	j.value = v
}

func (j *job) GetState() jobState {
	j.statemux.RLock()
	defer j.statemux.RUnlock()
	return j.state
}

func (j *job) GetInterruptedBy() (*task, interface{}) {
	return j.interrby, j.interrerr
}
