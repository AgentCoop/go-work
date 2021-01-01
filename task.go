package job

import (
	"sync/atomic"
)

func newTask(job *Job, typ TaskType, index int) *TaskInfo{
	t := &TaskInfo{
		job:      job,
		typ:      typ,
		index:    index,
		tickChan: make(chan struct{}, 1),
		doneChan: make(chan struct{}, 1),
	}
	return t
}

func (t *TaskInfo) GetJob() JobInterface {
	return t.job
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

func (t *TaskInfo) Tick() {
	t.doneChan <- struct{}{}
}

func (t *TaskInfo) Done() {
	t.state = StoppedTask
	t.doneChan <- struct{}{}
}

func (j *Job) hasOneshotTask() bool {
	_, ok := j.taskMap[0]
	return ok
}

func (j *Job) createTask(taskGen JobTask, index int, typ TaskType) *TaskInfo {
	task := newTask(j, typ, index)
	body := func() {
		init, run, fin := taskGen(j)
		task.finalize = fin
		go func() {
			defer func() {
				atomic.AddUint32(&j.finishedTasksCounter, 1)
				if r := recover(); r != nil {
					atomic.AddUint32(&j.failedTasksCounter, 1)
					j.Cancel()
				}
			}()

			if init != nil {
				task.state = RunningTask
				init(task)
			} else {
				task.state = RunningTask
			}

			task.tickChan <- struct{}{}
			for {
				select {
					case <- task.tickChan:
						run(task)
					case <- task.doneChan:
						if atomic.LoadUint32(&j.failedTasksCounter) > 0 {
							return
						}
						switch j.state {
						case OneshotRunning:
							j.oneshotDone <- DoneSig
							j.runRecurrent()
						case RecurrentRunning:
							if atomic.LoadUint32(&j.finishedTasksCounter) == uint32(len(j.taskMap)) - 1 {
								j.state = Done
								j.done()
							}
						default:
						}
						return
				}
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
