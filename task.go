package job

import (
	"runtime"
	"sync/atomic"
)

func newTask(typ TaskType, index int) *TaskInfo{
	t := &TaskInfo{
		typ: typ,
		index: index,
		depChan: make(chan *TaskInfo),
		depMap: make(depMap),
	}
	return t
}

func (t *TaskInfo) GetJob() JobInterface {
	return t.job
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

func (j *Job) hasOneshotTask() bool {
	_, ok := j.taskMap[0]
	return ok
}

func (t *TaskInfo) notifyDependentTasks(){
	for _, dep := range t.depMap {
		counter := atomic.AddInt32(&dep.depReceivedCounter, 1)
		dep.depChan <- t
		if counter == dep.depCounter {
			close(dep.depChan)
		}
	}
}

func (j *Job) createTask(taskGen JobTask, index int, typ TaskType) *TaskInfo {
	task := newTask(typ, index)
	body := func() {
		init, run, fin := taskGen(j)
		task.finalize = fin
		go func() {
			defer func() {
				if r := recover(); r != nil {
					atomic.AddUint32(&j.failedTasksCounter, 1)
					j.Cancel()
				}
			}()

			atomic.AddInt32(&j.runningTasksCounter, 1)
			if init != nil {
				task.state = RunningTask
				init(task)
			} else {
				task.state = RunningTask
			}

			for {
				if atomic.LoadUint32(&j.failedTasksCounter) > 0 {
					return
				}

				switch task.state {
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
						task.notifyDependentTasks()
						if runCount == 0 {
							j.state = Done
							j.done()
						}
					default:
						j.stateMu.RUnlock()
					}
					return
				}

				run(task)
				runtime.Gosched()
			}
		}()
	}
	task.body = body
	j.taskMap[index] = task
	return task
}

func (j *Job) AddTask(task JobTask) *TaskInfo {
	return j.createTask(task, 1 + len(j.taskMap), Recurrent)
}

// Zero index is reserved for oneshot task
func (j *Job) AddOneshotTask(task JobTask) {
	j.createTask(task, 0, Oneshot)
}
