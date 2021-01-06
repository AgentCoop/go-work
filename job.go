package job

import (
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

func NewJob(value interface{}) *Job {
	j := &Job{}
	j.state = New
	j.value = value
	j.taskMap = make(TaskMap)
	j.doneChan = make(chan struct{}, 1)
	j.oneshotDone = make(chan struct{}, 1)
	return j
}

func (j *Job) done() {
	j.doneChan <- struct{}{}
}

func (j *Job) GetDoneChan() chan struct{} {
	return j.doneChan
}

// A Job won't start until all its prerequisites are met
func (j *Job) WithPrerequisites(sigs ...<-chan struct{}) *Job {
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

func (j *Job) WithTimeout(t time.Duration) *Job {
	j.timeout = t
	return j
}

func (j *Job) prerun() {
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

func (j *Job) observer() {
	for {
		j.stateMu.RLock()
		if j.state == RecurrentRunning && j.finishedTasksCounter == uint32(len(j.taskMap)) {
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

func (j *Job) runOneshot() {
	j.state = OneshotRunning
	task := j.taskMap[0]
	if task.init != nil { task.init(task) }
	task.body()
}

func (j *Job) runRecurrent() {
	j.stateMu.Lock()
	defer j.stateMu.Unlock()

	if j.state == Cancelled { return }
	j.state = RecurrentRunning

	go j.observer()

	for i, task := range j.taskMap {
		if i == 0 { continue } // skip oneshot task
		if task.init != nil { task.init(task) }
	}

	for i, task := range j.taskMap {
		if i == 0 { continue }
		task.body()
	}
}

// Concurrently executes all tasks in the Job.
func (j *Job) Run() chan struct{} {
	j.prerun()
	if j.hasOneshotTask() {
		j.runOneshot()
	} else {
		j.runRecurrent()
	}
	return j.doneChan
}

// Dispatch Done signal as soon as oneshot task finishes itself
func (j *Job) RunInBackground() <-chan struct{} {
	doneDup := make(chan struct{})
	go func() {
		j.runOneshot()
		select {
		case <- j.oneshotDone:
			doneDup <- struct{}{}
		//default:
		//	if j.failedTasksCounter > 0 {
		//		return
		//	}
		}
	}()
	return doneDup
}

func (j *Job) finalize(state JobState) {
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

func (j *Job) Cancel() {
	j.finalizeOnce.Do(func() {
		j.finalize(Cancelled)
	})
}

func (j *Job) Finish() {
	j.finalizeOnce.Do(func() {
		j.finalize(Done)
	})
}

func RegisterDefaultLogger(logger Logger) {
	m := logger()
	defaultLogger = m
	for level, item := range m {
		logchan := item.ch
		handler := item.rechandler
		l := level
		go func() {
			for {
				select {
				case entry := <- logchan:
					handler(entry, l)
				}
			}
		}()
	}
}

func (j *Job) RegisterLogger(logger Logger) {
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

func (j *Job) Log(level int) chan<- interface{} {
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

func (j *Job) WasTimedOut() bool {
	return j.timedoutFlag
}

func (j *Job) GetValue() interface{} {
	return j.value
}

func (j *Job) SetValue(v interface{}) {
	j.value = v
}

func (j *Job) GetState() JobState {
	return j.state
}

func (j *Job) IsRunning() bool {
	return j.state == RecurrentRunning
}

func (j *Job) IsCancelled() bool {
	return j.state == Cancelled
}

func (j *Job) IsDone() bool {
	return j.state == Done
}

func (j *Job) GetInterruptedBy() (*TaskInfo, interface{}) {
	return j.interrby, j.interrerr
}
