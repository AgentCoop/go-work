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
	j.doneChan = make(chan struct{})
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

func (j *Job) runOneshot() {
	j.state = OneshotRunning
	info := j.taskMap[0]
	info.body()
}

func (j *Job) runRecurrent() {
	j.stateMu.Lock()
	defer j.stateMu.Unlock()
	if j.state == Cancelled { return }
	j.state = RecurrentRunning
	for i, info := range j.taskMap {
		if i == 0 { // skip oneshot task
			continue
		}
		info.body()
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
		}
	}()
	return doneDup
}

func (j *Job) finalize(state JobState) {
	j.stateMu.RLock()
	if j.state != OneshotRunning && j.state != RecurrentRunning { return }
	j.stateMu.RUnlock()

	j.stateMu.Lock()
	j.state = state
	j.stateMu.Unlock()

	// Run finalize routines
	for idx, task := range j.taskMap {
		fin := task.finalize
		if fin != nil {
			if idx == 0 && j.state == OneshotRunning { // concurrent tasks have not been started
				go fin()
				break
			}
			go fin()
		}
	}
	j.doneChan <- struct{}{}
}

func (j *Job) Cancel() {
	j.finalize(Cancelled)
}

func (j *Job) Finish() {
	j.finalize(Done)
}

func RegisterDefaultLogger(logger Logger) {
	m := logger()
	defaultLogger = m
	for _, item := range m {
		logchan := item.ch
		handler := item.rechandler
		go func() {
			for {
				select {
				case entry := <- logchan:
					handler(entry)
				}
			}
		}()
	}
}

func (j *Job) RegisterLogger(logger Logger) {
	m := logger()
	j.logLevelMap = m
	for _, item := range m {
		logchan := item.ch
		handler := item.rechandler
		go func() {
			for {
				select {
				case entry := <- logchan:
					handler(entry)
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
