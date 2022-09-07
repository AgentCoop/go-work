package job

import (
	"errors"
	"time"
)

var (
	NotifySig          = struct{}{}
	ErrTaskIdleTimeout = errors.New("go-work: task idling timed out")
	ErrAssertZeroValue = errors.New("go-work.Assert: zero value")
	ErrJobExecTimeout  = errors.New("go-work: job execution timed out")
)

const (
	New jobState = iota
	OneshotRunning
	RecurrentRunning
	Finalizing
	Cancelled
	Done
)

func (s jobState) String() string {
	return [...]string{"New", "Oneshot", "Recurrent", "Finalizing", "Cancelled", "Done"}[s]
}

const (
	PendingTask taskState = iota
	RunningTask
	FailedTask
	FinishedTask
)

func (s taskState) String() string {
	return [...]string{"Pending", "Running", "Failed", "Finished"}[s]
}

const (
	Oneshot taskType = iota
	Recurrent
)

func (t taskType) String() string {
	return [...]string{"Oneshot", "Recurrent"}[t]
}

// TaskInterface main routines
type Init func(TaskInterface)
type Run func(TaskInterface)
type Finalize func(TaskInterface)

type JobTask func(j JobInterface) (Init, Run, Finalize)
type OneshotTask JobTask

type JobInterface interface {
	AddTask(job JobTask) *task
	GetTaskByIndex(index int) *task
	AddOneshotTask(job JobTask)
	AddTaskWithIdleTimeout(job JobTask, timeout time.Duration) *task
	WithTimeout(duration time.Duration)
	WithShutdown(func(interface{}))
	WithErrorWrapper(wrapper ErrWrapper)
	Run() chan struct{}
	RunInBackground() <-chan struct{}
	Cancel(err interface{})
	Finish()
	GetValue() interface{}
	SetValue(v interface{})
	GetState() jobState
	GetInterruptedBy() (*task, interface{})
	TaskDoneNotify() <-chan *task
	JobDoneNotify() chan struct{}
}

type TaskInterface interface {
	Index() int
	GetJob() JobInterface
	GetState() taskState
	GetResult() interface{}
	SetResult(result interface{})
	Tick()
	Done()
	Idle()
	FinishJob()
	Assert(err interface{})
	AssertTrue(cond bool, err interface{})
	AssertNotNil(value interface{})
}

func NewJob(value interface{}) *job {
	j := &job{}
	j.state = New
	j.value = value
	j.taskMap = make(taskMap)
	j.donechan = make(chan struct{}, 1)
	return j
}
