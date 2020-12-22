package job

import (
	"sync"
	"sync/atomic"
	"time"
)

func (t *TaskInfo) GetResult() chan interface{} {
	return t.result
}

func (t *TaskInfo) GetJob() Job {
	return t.job
}

type job struct {
	tasks               []func()
	cancelTasks         []func()
	failedTasksCounter  uint32
	runningTasksCounter int32
	state               JobState
	timedoutFlag        bool
	withSyncCancel		bool
	timeout             time.Duration

	errorChan			chan interface{}
	doneChan    		chan struct{}
	prereqWg    		sync.WaitGroup

	value      			interface{}
	stateMu 			sync.RWMutex
}

func NewJob(value interface{}) *job {
	j := &job{}
	j.state = New
	j.value = value
	j.doneChan = make(chan struct{}, 1)
	return j
}

// A job won't start until all its prerequisites are met
func (j *job) WithPrerequisites(sigs ...<-chan struct{}) *job {
	j.state = WaitingForPrereq
	j.prereqWg.Add(len(sigs))
	for _, sig := range sigs {
		s := sig // Binds loop variable to go closure
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

func (j *job) Assert(err interface{}) {
	if err != nil {
		go func() {
			j.errorChan <- err
			j.Cancel()
		}()
		// Now time to panic to stop normal goroutine execution from which Assert method was called.
		panic(err)
	}
}

func (j *job) AddTask(task JobTask) *TaskInfo {
	taskInfo := &TaskInfo{}
	taskInfo.index = len(j.tasks)
	taskInfo.result =  make(chan interface{}, 1)
	taskBody := func() {
		init, run, cancel := task(j)
		j.cancelTasks = append(j.cancelTasks, cancel)
		go func() {
			defer func() {
				runCount := atomic.AddInt32(&j.runningTasksCounter, -1)
				if r := recover(); r != nil {
					atomic.AddUint32(&j.failedTasksCounter, 1)
				}
				if runCount == 0 {
					go func() {
						j.stateMu.Lock()
						defer j.stateMu.Unlock()
						if j.state == Running && j.failedTasksCounter == 0 {
							j.state = Done
							j.doneChan <- struct{}{}
						}
					}()
				}
			}()
			if init != nil {
				init()
			}
			for {
				result := run()
				j.stateMu.Lock()
				switch {
				case j.state != Running:
					j.stateMu.Unlock()
					taskInfo.result <- result
					return
				default:
					j.stateMu.Unlock()
				}

				if result != nil {
					taskInfo.result <- result
					return
				}
			}
		}()
	}
	j.tasks = append(j.tasks, taskBody)
	return taskInfo
}

// Concurrently executes all tasks in the job.
func (j *job) Run() chan struct{} {
	nTasks := len(j.tasks)
	j.errorChan = make(chan interface{}, nTasks)
	j.runningTasksCounter = int32(nTasks)
	// Start timer that will cancel and mark the job as timed out if needed
	if j.timeout > 0 {
		go func() {
			ch := time.After(j.timeout)
			for {
				select {
				case <-ch:
					if j.state == Running {
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

	j.state = Running

	for _, task := range j.tasks {
		task()
	}
	return j.doneChan
}

func (j *job) Cancel() {
	j.stateMu.RLock()
	switch j.state {
	case Running:
		j.stateMu.RUnlock()
	default:
		j.stateMu.RUnlock()
		return
	}
	j.stateMu.Lock()
	defer j.stateMu.Unlock()
	j.state = Cancelling

	for _, cancel := range j.cancelTasks {
		if cancel != nil {
			go cancel()
		}
	}

	j.state = Cancelled
	j.doneChan <- struct{}{}
}

func (j *job) CancelWithError(err interface{}) {
	j.errorChan <- err
	j.Cancel()
}

func (j *job) WasTimedOut() bool {
	return j.timedoutFlag
}

func (j *job) GetError() chan interface{} {
	return j.errorChan
}

func (j *job) GetFailedTasksNum() uint32 {
	return j.failedTasksCounter
}

func (j *job) GetValue() interface{} {
	return j.value
}

func (j *job) GetState() JobState {
	return j.state
}

func (j *job) IsRunning() bool {
	return j.state == Running
}

func (j *job) IsCancelled() bool {
	return j.state == Cancelled
}

func (j *job) IsDone() bool {
	return j.state == Done
}
