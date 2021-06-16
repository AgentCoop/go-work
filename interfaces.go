package job

import (
	"errors"
	"time"
)

type LogLevelMapItem struct {
	ch         chan interface{}
	rechandler LogRecordHandler
}
type LogRecordHandler func(entry interface{}, level int)
type LogLevelMap map[int]*LogLevelMapItem
type Logger func() LogLevelMap

func NewLogLevelMapItem(ch chan interface{}, handler LogRecordHandler) *LogLevelMapItem {
	return &LogLevelMapItem{ch, handler}
}

var (
	DefaultLogLevel    int
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

// Task main routines
type Init func(Task)
type Run func(Task)
type Finalize func(Task)

type JobTask func(j Job) (Init, Run, Finalize)
type OneshotTask JobTask

type Job interface {
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
	Log(level int) chan<- interface{}
	RegisterLogger(logger Logger)
	GetValue() interface{}
	SetValue(v interface{})
	GetState() jobState
	GetInterruptedBy() (*task, interface{})
	TaskDoneNotify() <-chan *task
	JobDoneNotify() chan struct{}
}

type Task interface {
	GetIndex() int
	GetJob() Job
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
