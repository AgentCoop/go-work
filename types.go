package job

import (
	"sync"
	"time"
)

type JobState int
type TaskType int

const (
	New JobState = iota
	WaitingForPrereq
	OneshotRunning
	RecurrentRunning
	Cancelled
	Done
)

const (
	Oneshot TaskType = iota
	Recurrent
)

func (s JobState) String() string {
	return [...]string{"New", "WaitingForPrereq", "Oneshot", "RecurrentRunning", "Cancelled", "Done"}[s]
}

func (t TaskType) String() string {
	return [...]string{"Oneshot", "Recurrent",}[t]
}

type JobTask func(j JobInterface) (func(), func() interface{}, func())
type OneshotTask func(j JobInterface) (func(), func() bool, func())

type JobInterface interface {
	AddTask(job JobTask) *TaskInfo
	AddOneshotTask (job JobTask) *TaskInfo
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
	GetFailedTasksNum() uint32
	GetValue() interface{}
	SetValue(v interface{})
	GetState() JobState
	// Helper methods to GetState
	IsRunning() bool
	IsDone() bool
	IsCancelled() bool
}

type TaskInfo struct {
	index 	int
	typ TaskType
	body func()
	cancel func()
	result 	chan interface{}
	job 	*Job
	err 	interface{}
}

type Job struct {
	taskMap TaskMap
	cancelTasks         []func()
	failedTasksCounter  uint32
	runningTasksCounter int32
	state               JobState
	timedoutFlag        bool
	withSyncCancel		bool
	timeout             time.Duration

	cancelMapMu sync.Mutex
	cancelMap CancelMap
	oneshotTask			JobTask
	errorChan			chan interface{}
	doneChan    		chan struct{}
	oneshotDone    		chan struct{}
	prereqWg    		sync.WaitGroup

	value      			interface{}
	stateMu 			sync.RWMutex
}
