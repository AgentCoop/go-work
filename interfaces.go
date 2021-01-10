package job

import "time"

type JobState int
type TaskType int
type TaskState int

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

const (
	New JobState = iota
	WaitingForPrereq
	OneshotRunning
	RecurrentRunning
	Cancelled
	Done
)

func (s JobState) String() string {
	return [...]string{"New", "WaitingForPrereq", "Oneshot", "Recurrent", "Cancelled", "Done"}[s]
}

const (
	PendingTask TaskState = iota
	RunningTask
	FailedTask
	FinishedTask
)

func (s TaskState) String() string {
	return [...]string{"Pending", "Running", "Failed", "Finished"}[s]
}

const (
	Oneshot TaskType = iota
	Recurrent
)

func (t TaskType) String() string {
	return [...]string{"Oneshot", "Recurrent"}[t]
}

var (
	DoneSig = struct{}{}
)

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
	WithPrerequisites(sigs ...<-chan struct{}) *job
	WithTimeout(duration time.Duration) *job
	WasTimedOut() bool
	Run() chan struct{}
	RunInBackground() <-chan struct{}
	Cancel()
	Finish()
	Log(level int) chan<- interface{}
	RegisterLogger(logger Logger)
	GetValue() interface{}
	SetValue(v interface{})
	GetInterruptedBy() (*task, interface{})
	GetState() JobState
	// Helper methods to GetState
	IsRunning() bool
	IsDone() bool
	IsCancelled() bool
}

type Task interface {
	GetIndex() int
	GetJob() Job
	GetState() TaskState
	GetResult() interface{}
	SetResult(result interface{})
	Tick()
	Done()
	Idle()
	Assert(err interface{})
	AssertTrue(cond bool, err string)
	AssertNotNil(value interface{})
}

func NewJob(value interface{}) *job {
	j := &job{}
	j.state = New
	j.value = value
	j.taskMap = make(TaskMap)
	j.doneChan = make(chan struct{}, 1)
	return j
}

func RegisterDefaultLogger(logger Logger) {
	m := logger()
	defaultLogger = m
	for level, item := range m {
		logchan := item.ch
		handler := item.rechandler
		l := level
		go func() {
			for {
				select {
				case entry := <- logchan:
					handler(entry, l)
				}
			}
		}()
	}
}