package job

import "time"

type JobState int

const (
	New JobState = iota
	WaitingForPrereq
	Running
	Cancelling
	Cancelled
	Done
)

func (s JobState) String() string {
	return [...]string{"New", "WaitingForPrereq", "Running", "Cancelling", "Cancelled", "Done"}[s]
}

type JobTask func(j Job) (func(), func() interface{}, func())

type Job interface {
	AddTask(job JobTask) *TaskInfo
	WithPrerequisites(sigs ...<-chan struct{}) *job
	WithTimeout(duration time.Duration) *job
	WasTimedOut() bool
	Run() chan struct{}
	Cancel()
	CancelWithError(err interface{})
	Assert(err interface{})
	GetError() chan interface{}
	GetFailedTasksNum() uint32
	GetValue() interface{}
	GetState() JobState
	// Helper methods to GetState
	IsRunning() bool
	IsDone() bool
	IsCancelled() bool
}

type TaskInfo struct {
	index 	int
	result 	chan interface{}
	job 	*job
	err 	interface{}
}
