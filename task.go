package job

import (
	"errors"
	"sync/atomic"
)

func newTask(job *Job, typ TaskType, index int) *TaskInfo {
	t := &TaskInfo{
		job:      job,
		state: PendingTask,
		typ:      typ,
		index:    index,
		tickChan: make(chan struct{}, 1),
		doneChan: make(chan struct{}, 1),
	}
	return t
}

func (t *TaskInfo) GetIndex() int {
	return t.index
}

func (t *TaskInfo) GetJob() JobInterface {
	return t.job
}

func (t *TaskInfo) GetState() TaskState {
	return t.state
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
	t.tickChan <- struct{}{}
}

func (t *TaskInfo) Done() {
	t.state = FinishedTask
	t.doneChan <- struct{}{}
}

func (t *TaskInfo) GetInterruptedBy() (*TaskInfo, interface{}) {
	return t.job.interrby, t.job.interrerr
}

func (t *TaskInfo) stopexec(err interface{}) {
	t.job.stoponce.Do(func() {
		t.job.interrby = t
		t.job.interrerr = err
		t.job.Cancel()
	})
}

func (t *TaskInfo) Assert(err interface{}) {
	if err != nil {
		t.stopexec(err)
		// Now time to panic to stop normal goroutine execution from which Assert method was called.
		panic(err)
	}
}

func (t *TaskInfo) AssertTrue(cond bool, err string) {
	if cond {
		err := errors.New(err)
		t.stopexec(err)
		panic(err)
	}
}

func (j *Job) hasOneshotTask() bool {
	_, ok := j.taskMap[0]
	return ok
}

func (t *TaskInfo) thread(f func(), finish bool) {
	defer func() {
		job := t.job
		if finish {
			atomic.AddUint32(&job.finishedTasksCounter, 1)
		}
		if r := recover(); r != nil {
			atomic.AddUint32(&job.failedTasksCounter, 1)
			t.stopexec(r)
		}
	}()
	f()
}

func (task *TaskInfo) taskLoop(run Run) {
	task.state = RunningTask
	task.tickChan <- struct{}{}
	job := task.job

	for {
		if job.state == Cancelled || atomic.LoadUint32(&job.failedTasksCounter) > 0 {
			task.state = StoppedTask
			return
		}
		select {
		case <- task.tickChan:
			run(task)
		case <- task.doneChan:
			task.state = FinishedTask
			switch job.state {
			case OneshotRunning:
				job.oneshotDone <- DoneSig
				job.runRecurrent()
			}
			return
		default:
		}
	}
}

func (j *Job) createTask(taskGen JobTask, index int, typ TaskType) *TaskInfo {
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

func (j *Job) AddTask(task JobTask) *TaskInfo {
	return j.createTask(task, 1 + len(j.taskMap), Recurrent)
}

// Zero index is reserved for oneshot task
func (j *Job) AddOneshotTask(task JobTask) {
	j.createTask(task, 0, Oneshot)
}
