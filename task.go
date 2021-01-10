package job

import (
	"errors"
	//"runtime"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrTaskIdleTimeout = errors.New("task idle timeout")
	ErrAssertZeroValue = errors.New("assert: zero value")
)

type TaskMap map[int]*task

type task struct {
	index    int
	typ      TaskType
	state    TaskState
	starttime	int64
	idletime	int64
	idleTimeout int64
	resultMu sync.RWMutex
	result   interface{}
	tickChan chan struct{}
	doneChan chan struct{}
	idleChan chan struct{}
	job      *job
	body     func()
	init Init
	finalize Finalize
}

func newTask(job *job, typ TaskType, index int) *task {
	t := &task{
		job:      job,
		state: PendingTask,
		typ:      typ,
		index:    index,
		tickChan: make(chan struct{}, 1),
		doneChan: make(chan struct{}, 1),
		idleChan: make(chan struct{}, 1),
	}
	return t
}

func (t *task) GetIndex() int {
	return t.index
}

func (t *task) GetJob() Job {
	return t.job
}

func (t *task) GetState() TaskState {
	return t.state
}

func (t *task) GetResult() interface{} {
	t.resultMu.RLock()
	defer t.resultMu.RUnlock()
	return t.result
}

func (t *task) SetResult(result interface{}) {
	t.resultMu.Lock()
	defer t.resultMu.Unlock()
	t.result = result
}

func (t *task) Tick() {
	t.tickChan <- struct{}{}
}

func (t *task) Done() {
	t.state = FinishedTask
	t.doneChan <- struct{}{}
}

func (t *task) Idle() {
	t.idleChan <- struct{}{}
}

func (t *task) stopexec(err interface{}) {
	t.job.stoponce.Do(func() {
		t.job.interrby = t
		t.job.interrerr = err
		t.job.Cancel()
	})
}

func (t *task) Assert(err interface{}) {
	if err != nil {
		t.stopexec(err)
		// Now time to panic to stop normal goroutine execution from which Assert method was called.
		panic(err)
	}
}

func (t *task) AssertTrue(cond bool, err string) {
	if cond {
		err := errors.New(err)
		t.stopexec(err)
		panic(err)
	}
}

func (t *task) AssertNotNil(value interface{}) {
	if value == nil {
		err := ErrAssertZeroValue
		t.stopexec(err)
		panic(err)
	}
}

func (j *job) hasOneshotTask() bool {
	_, ok := j.taskMap[0]
	return ok
}

func (t *task) thread(f func(), finish bool) {
	defer func() {
		job := t.job
		if finish {
			atomic.AddUint32(&job.finishedcount, 1)
		}
		if r := recover(); r != nil {
			atomic.AddUint32(&job.failcount, 1)
			t.state = FailedTask
			t.stopexec(r)
		}
		job.observerchan <- DoneSig
	}()
	f()
}

func (t *task) wasStoppped() bool {
	switch {
	case t.job.state == Cancelled, t.job.state == Done, atomic.LoadUint32(&t.job.failcount) > 0:
		return true
	case t.typ == Recurrent && t.state == FinishedTask:
		return true
	default:
		return false
	}
}

func (task *task) taskLoop(run Run) {
	task.state = RunningTask
	task.tickChan <- struct{}{}
	// Assume the init routine will be finished (or must) almost instantly
	task.starttime = time.Now().UnixNano()

	for {
		select {
		case <- task.tickChan:
			task.idletime = 0
			if task.wasStoppped() { return }
			run(task)
		case <- task.doneChan:
			task.idletime = 0
			return
		case <- task.idleChan:
			task.idletime = time.Now().UnixNano()
			switch {
			case task.wasStoppped():
				return
			case task.idleTimeout > 0 && task.idletime - task.starttime >= task.idleTimeout:
				task.stopexec(ErrTaskIdleTimeout)
				return
			default:
				run(task)
			}
		default:
			if task.wasStoppped() { return }
		}
	}
}
