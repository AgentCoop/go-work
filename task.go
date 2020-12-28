package job

import (
	"runtime"
	"sync/atomic"
	///"time"
)

func (j *Job) hasOneshotTask() bool {
	_, ok := j.taskMap[0]
	return ok
}

func (j *Job) createTask(task JobTask, index int, typ TaskType) *TaskInfo {
	taskInfo := &TaskInfo{ typ: typ, index: index }
	taskInfo.result = make(chan interface{}, 1)
	body := func() {
		init, run, cancel := task(j)
		j.cancelMapMu.Lock()
		j.cancelMap[index] = cancel
		j.cancelMapMu.Unlock()
		go func() {
			defer func() {
				if r := recover(); r != nil {
					j.Cancel()
					atomic.AddUint32(&j.failedTasksCounter, 1)
					//fmt.Printf("failed %d %s\n", j.failedTasksCounter, r)
				}
				runCount := atomic.AddInt32(&j.runningTasksCounter, -1)
				if  runCount == 0 {
					go func() {
						j.stateMu.Lock()
						defer j.stateMu.Unlock()
						failedCount := atomic.LoadUint32(&j.failedTasksCounter)
						if j.state == RecurrentRunning || j.state == OneshotRunning && failedCount == 0 {
							j.state = Done
							j.done()
						}
					}()
				}
			}()
			if init != nil {
				init()
			}
			for {
				//if failed := atomic.LoadUint32(&j.failedTasksCounter); failed > 0 {
				//	return
				//}
				result := run()
				j.stateMu.RLock()
				switch j.state {
				case OneshotRunning:
					j.stateMu.RUnlock()
					taskInfo.result <- result
					return
				case RecurrentRunning:
					j.stateMu.RUnlock()
					if result != nil {
						taskInfo.result <- result
						return
					}
				default:
					j.stateMu.RUnlock()
					return
				}
				runtime.Gosched()
			}
		}()
	}

	taskInfo.body = body
	j.taskMap[index] = taskInfo

	return taskInfo
}

func (j *Job) AddTask(task JobTask) *TaskInfo {
	return j.createTask(task, 1 + len(j.taskMap), Recurrent)
}

// Zero index is reserved for oneshot task
func (j *Job) AddOneshotTask(task JobTask) *TaskInfo {
	taskInfo := j.createTask(task, 0, Oneshot)
	return taskInfo
}
