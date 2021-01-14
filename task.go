package job

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type taskType int
type taskState int
type taskMap map[int]*task

type task struct {
	index       int
	typ         taskType
	statemux    sync.RWMutex
	state       taskState
	lasttick    int64
	idletime    int64
	idleTimeout int64
	resultmux   sync.RWMutex
	result      interface{}
	tickchan    chan struct{}
	donechan    chan struct{}
	idlechan    chan struct{}
	job         *job
	body        func()
	init        Init
	finalize    Finalize
}

func newTask(job *job, typ taskType, index int) *task {
	t := &task{
		job:      job,
		state:    PendingTask,
		typ:      typ,
		index:    index,
		tickchan: make(chan struct{}, 1),
		donechan: make(chan struct{}, 1),
		idlechan: make(chan struct{}, 1),
	}
	return t
}

func (t *task) GetIndex() int {
	return t.index
}

func (t *task) GetJob() Job {
	return t.job
}

func (t *task) GetState() taskState {
	t.statemux.RLock()
	defer t.statemux.RUnlock()
	return t.state
}

func (t *task) GetResult() interface{} {
	t.resultmux.RLock()
	defer t.resultmux.RUnlock()
	return t.result
}

func (t *task) SetResult(result interface{}) {
	t.resultmux.Lock()
	defer t.resultmux.Unlock()
	t.result = result
}

func (t *task) Tick() {
	t.tickchan <- struct{}{}
}

func (t *task) Done() {
	t.statemux.Lock()
	defer t.statemux.Unlock()
	t.state = FinishedTask
	t.donechan <- struct{}{}
	t.job.taskdonechan <- t
}

func (t *task) Idle() {
	runtime.Gosched()
	t.idletime = time.Now().UnixNano()
	t.idlechan <- struct{}{}
}

func (t *task) FinishJob() {
	t.Done()
	t.job.Finish()
}

func (t *task) Assert(err interface{}) {
	if err != nil {
		t.job.cancel(t, err)
		// Now time to panic to stop normal goroutine execution from which Assert method was called.
		panic(err)
	}
}

func (t *task) AssertTrue(cond bool, err string) {
	if cond {
		err := errors.New(err)
		t.job.cancel(t, err)
		panic(err)
	}
}

func (t *task) AssertNotNil(value interface{}) {
	if value == nil {
		err := ErrAssertZeroValue
		t.job.cancel(t, err)
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
		if err := recover(); err != nil {
			atomic.AddUint32(&job.failcount, 1)
			t.state = FailedTask
			t.job.cancel(t, err)
		}
		job.observerchan <- NotifySig
	}()
	f()
}

func (t *task) wasStoppped() bool {
	t.job.statemux.RLock()
	defer t.job.statemux.RUnlock()
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
	task.tickchan <- struct{}{}
	for {
		select {
		case <- task.tickchan:
			task.lasttick = time.Now().UnixNano()
			if task.wasStoppped() { return }
			run(task)
		case <- task.donechan:
			task.lasttick = time.Now().UnixNano()
			return
		case <- task.idlechan:
			switch {
			case task.wasStoppped():
				return
			case task.idleTimeout > 0 && task.idletime - task.lasttick >= task.idleTimeout:
				task.job.cancel(task, ErrTaskIdleTimeout)
				return
			default:
				run(task)
			}
		}
	}
}
