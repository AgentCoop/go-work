package job

import (
	"sync"
	"time"
)

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
	StoppedTask
	FinishedTask
)

func (s TaskState) String() string {
	return [...]string{"Pending", "Running", "Stopped", "Finished"}[s]
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

type Init func(*TaskInfo)
type Run func(*TaskInfo)
type Finalize func(*TaskInfo)
type JobTask func(j JobInterface) (Init, Run, Finalize)
type OneshotTask JobTask

type JobInterface interface {
	AddTask(job JobTask) *TaskInfo
	AddOneshotTask(job JobTask)
	WithPrerequisites(sigs ...<-chan struct{}) *Job
	WithTimeout(duration time.Duration) *Job
	WasTimedOut() bool
	Run() chan struct{}
	RunInBackground() <-chan struct{}
	Cancel()
	Finish()
	Log(level int) chan<- interface{}
	RegisterLogger(logger Logger)
	GetValue() interface{}
	SetValue(v interface{})
	GetInterruptedBy() (*TaskInfo, interface{})
	GetState() JobState
	// Helper methods to GetState
	IsRunning() bool
	IsDone() bool
	IsCancelled() bool
}

type TaskMap map[int]*TaskInfo

type TaskInfo struct {
	index    int
	typ      TaskType
	state    TaskState
	resultMu sync.RWMutex
	result   interface{}
	tickChan chan struct{}
	doneChan chan struct{}
	job      *Job
	body     func()
	init Init
	finalize Finalize
}

type Job struct {
	taskMap              TaskMap
	logLevelMap          LogLevelMap
	logLevel             int
	failedTasksCounter   uint32
	finishedTasksCounter uint32
	stateMu              sync.RWMutex
	state                JobState
	finalizeOnce		sync.Once
	timedoutFlag         bool
	timeout              time.Duration
	doneChan             chan struct{}
	oneshotDone          chan struct{}
	prereqWg             sync.WaitGroup
	value                interface{}
	interronce           sync.Once
	interrby             *TaskInfo //// task interrupted the job execution
	interrerr            interface{}
}
