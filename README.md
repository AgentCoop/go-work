## Introduction
Go has lots of synchronization mechanisms ranging from language primitives such as mutexes, channels, atomic functions,
and to a more complicated components such as [*content.Context*](https://golang.org/pkg/context/). Despite the fact that Go already has aforementioned
synchronization mechanisms using them to solve problems arising in day-to-day programming might be quite challenging,
especially for novices.

The goal of the _Job design pattern_ and this implementation is to provide a clear and steady foundation for solving problems
involving concurrent execution and its control. The Job pattern can be viewed as an alternative to
[*content.Context*](https://golang.org/pkg/context/) in many cases, though it's not meant to completely replace it.   

#### What is a Job?
A job is a set of concurrently running tasks execution of which depends on each other. If one task fails,
the whole job execution fails too.
```go
    myjob := job.NewJob(nil)
    myjob.AddTask(task1)
    myjob.AddTask(task2)
    <-myjob.Run()
    // Let's process the result
```

#### What is a Task?
A single task consists of three routines: an initialization routine, a recurrent routine, and a finalization routine:
```go
func (t *taskData) MyTask(j job.JobInterface) (job.Init, job.Run, job.Finalize) {
	init := func(task *job.TaskInfo) {
		// Do some initialization here
	}
	run := func(task *job.TaskInfo) {
		// Run some recurrent logic here.
		// Use a Ping/Pong synchronization approach to share data among other tasks
		select {
		case data := <- t.task1Chan: // ping
			// handle data
			// ...
			// Tell task1 we are done, pong
			t.task1ChanSync <- struct{}{}
		default:
			task.Tick()
		}
	}
	fin := func(task *job.TaskInfo) {
		// Clean up
		close(t.task1Chan)
		close(t.task1ChanSync)
	}
}
```
The recurrent routine is running in an indefinite loop. It's a well-known concept of `for { select {  } }` statements in Go.
<br><br>
Tasks have three special methods:
 * **.Tick()** - to proceed a task execution.
 * **.Done()** - to finish a task execution.
 * **.Idle()** - to tell that a task has nothing to do, and as a result it might be finished by reaching the idle timeout.

Tasks can assert some conditions, replacing `if err != nil { panic(err) }` by a more terse way:
```go
func (m *MyTask) MyTask(j job.JobInterface) (job.Init, job.Run, job.Finalize) {
    run := func(task *job.TaskInfo) {
        _, err := os.Open("badfile")
        task.Assert(err)
    }
}
```
Every failed assertion will result in the cancellation of job execution, and invocation of
finalization routines of all tasks in the job being cancelled.

## Documentation

### API reference

##### Job
  * [_AddTask(job JobTask) ***TaskInfo**_](docs/job.md) - adds a new task to the job
  * [_AddOneshotTask(job JobTask)_](docs/job.md) - adds an oneshot tasks to the job
  * [_AddTaskWithIdleTimeout(job JobTask, timeout time.Duration) ***TaskInfo**_](docs/job.md) - adds a task with an idle timeout
  * [_WithPrerequisites(sigs ...<-chan struct{}) ***Job**_](docs/job.md) - waits for the prerequisites to be met before running the job.
  * [_WithTimeout(duration time.Duration) ***Job**_](docs/job.md) - sets run timeout for the job. 
  * [_WasTimedOut() bool_](docs/job.md) - returns TRUE if the job was timed out.
  * [_Run() chan struct{}_](docs/job.md) - Runs the job.
  * [_RunInBackground() **<-chan struct{}**_](docs/job.md) - Runs the job in background. An oneshot task required. 
  * [_Cancel()_](docs/job.md) - Cancels the job.
  * [_Finish()_](docs/job.md) - Finishes the job.
  * [_Log(level int) **chan<- interface{}**_](docs/job.md) - Writes a log record using default or job's logger.
  * [_RegisterLogger(logger Logger)_](docs/job.md) - Registers a job logger.
  * [_GetValue() interface{}_](docs/job.md) - Returns a job value.
  * [_SetValue(v interface{})_](docs/job.md) - Sets a job value.
  * [_GetInterruptedBy() (***TaskInfo, interface{}**)_](docs/job.md) - Returns a task and an error that interrupted the job execution.
  * [_GetState() **JobState**_](docs/job.md) - Returns the job state.
##### Task