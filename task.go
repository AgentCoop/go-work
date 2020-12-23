package job

import (
	"sync/atomic"
)

func (j *job) hasOneshotTask() bool {
	_, ok := j.taskMap[0]
	return ok
}

func (j *job) createTask(task JobTask, index int, typ TaskType) *TaskInfo {
	taskInfo := &TaskInfo{ typ: typ, index: index }
	taskInfo.result = make(chan interface{}, 1)
	body := func() {
		init, run, cancel := task(j)
		j.cancelMapMu.Lock()
		j.cancelMap[index] = cancel
		j.cancelMapMu.Unlock()
		go func() {
			defer func() {
				runCount := atomic.AddInt32(&j.runningTasksCounter, -1)
				if r := recover(); r != nil {
					//fmt.Printf("err : %s\n", r)
					atomic.AddUint32(&j.failedTasksCounter, 1)
				}
				if runCount == 0 {
					go func() {
						j.stateMu.Lock()
						defer j.stateMu.Unlock()
						if j.state == RecurrentRunning || j.state == OneshotRunning && j.failedTasksCounter == 0 {
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
				j.stateMu.RLock()
				switch j.state {
				case RecurrentRunning:
					j.stateMu.RUnlock()
				default:
					j.stateMu.RUnlock()
					taskInfo.result <- result
					return
				}
				if result != nil {
					taskInfo.result <- result
					return
				}
			}
		}()
	}

	taskInfo.body = body
	j.taskMap[index] = taskInfo

	return taskInfo
}

func (j *job) AddTask(task JobTask) *TaskInfo {
	return j.createTask(task, 1 + len(j.taskMap), Recurrent)
}

// Zero index is reserved for oneshot task
func (j *job) AddOneshotTask(task JobTask) *TaskInfo {
	return j.createTask(task, 0, Oneshot)
}
