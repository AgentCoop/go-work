package job

import (
	"sync"
	"time"
)

type JobState int
type TaskType int
type TaskState int

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
	return [...]string{"Oneshot", "Recurrent",}[t]
}

var (
	DoneSig = struct{}{}
)

type Init func(*TaskInfo)
type Run func(*TaskInfo)
type Cancel func()
type JobTask func(j JobInterface) (Init, Run, Cancel)
type OneshotTask JobTask

type JobInterface interface {
	AddTask(job JobTask) *TaskInfo
	AddOneshotTask (job JobTask)
	WithPrerequisites(sigs ...<-chan struct{}) *Job
	WithTimeout(duration time.Duration) *Job
	WasTimedOut() bool
	Run() chan struct{}
	RunInBackground() <-chan struct{}
	Cancel()
	Finish()
	CancelWithError(err interface{})
	Assert(err interface{})
	AssertTrue(cond bool, err string)
	GetError() chan interface{}
	GetValue() interface{}
	SetValue(v interface{})
	GetState() JobState
	// Helper methods to GetState
	IsRunning() bool
	IsDone() bool
	IsCancelled() bool
}

type TaskInfo struct {
	index              int
	typ                TaskType
	state              TaskState
	resultMu           sync.RWMutex
	result             interface{}
	tickChan           chan struct{}
	doneChan           chan struct{}
	job      *Job
	body     func()
	finalize func()
	err      interface{}
}

type Job struct {
	taskMap 			TaskMap
	failedTasksCounter  uint32
	finishedTasksCounter 	uint32
	stateMu 			sync.RWMutex
	state               JobState
	timedoutFlag        bool
	timeout             time.Duration
	errorChan			chan interface{}
	doneChan    		chan struct{}
	oneshotDone    		chan struct{}
	prereqWg    		sync.WaitGroup
	value      			interface{}
}
