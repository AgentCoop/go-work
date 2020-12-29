package job

import (
	"fmt"
	"runtime"
	"sync/atomic"
	///"time"
)

func newTask(typ TaskType, index int) *TaskInfo{
	t := &TaskInfo{
		typ: typ,
		index: index,
		doneChan: make(chan struct{}, 1),
		depChan: make(chan *TaskInfo),
		depMap: make(depMap),
		resultChan: make(chan *TaskInfo, 1),
	}
	return t
}

func (t *TaskInfo) GetJob() JobInterface {
	return t.job
}

func (t *TaskInfo) GetDoneChan() chan<- struct{} {
	return t.doneChan
}

func (t *TaskInfo) GetDepChan() chan *TaskInfo {
	return t.depChan
}

func (t *TaskInfo) GetResult() interface{} {
	t.resultMu.RLock()
	defer t.resultMu.RUnlock()
	return t.result
}

func (t *TaskInfo) SetResult(result interface{}) {
	t.resultMu.Lock()
	defer t.resultMu.Unlock()
	t.result = result
}

func (t *TaskInfo) DependsOn(dep *TaskInfo) {
	t.depCounter++
	dep.depMap[t.index] = t
}

func (t *TaskInfo) Done() {
	t.state = StoppedTask
}

func (t *TaskInfo) GetResultChan() <-chan *TaskInfo {
	return t.resultChan
}

func (j *Job) hasOneshotTask() bool {
	_, ok := j.taskMap[0]
	return ok
}

func (t *TaskInfo) notifyDependentTasks(){
	for _, dep := range t.depMap {
		counter := atomic.AddInt32(&dep.depReceivedCounter, 1)
		dep.depChan <- t
		//fmt.Printf("done counter %d %d\n", dep.depreceivc, len(dep.depMap))
		if counter == dep.depCounter {
			fmt.Printf("close dep chan\n")
			close(dep.depChan)
		}
	}
}

func (j *Job) createTask(task JobTask, index int, typ TaskType) *TaskInfo {
	taskInfo := newTask(typ, index)
	body := func() {
		init, run, cancel := task(j)
		j.cancelMapMu.Lock()
		j.cancelMap[index] = cancel
		j.cancelMapMu.Unlock()
		go func() {
			defer func() {
				if r := recover(); r != nil {
					atomic.AddUint32(&j.failedTasksCounter, 1)
					j.Cancel()
				}
			}()

			atomic.AddInt32(&j.runningTasksCounter, 1)

			if init != nil {
				taskInfo.state = RunningTask
				init(taskInfo)
			} else {
				taskInfo.state = RunningTask
			}

			for {
				if atomic.LoadUint32(&j.failedTasksCounter) > 0 {
					return
				}

				switch taskInfo.state {
				case StoppedTask:
					j.stateMu.RLock()
					switch j.state {
					case OneshotRunning:
						atomic.AddInt32(&j.runningTasksCounter, -1)
						j.oneshotDone <- DoneSig
						j.stateMu.RUnlock()
						j.runRecurrent()
					case RecurrentRunning:
						runCount := atomic.AddInt32(&j.runningTasksCounter, -1)
						j.stateMu.RUnlock()
						taskInfo.notifyDependentTasks()
						if runCount == 0 {
							j.state = Done
							j.done()
						}
					default:
						j.stateMu.RUnlock()
					}
					return
				}

				run(taskInfo)
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
