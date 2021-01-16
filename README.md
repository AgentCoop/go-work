## Introduction
Go has lots of synchronization mechanisms ranging from language primitives such as mutexes, channels, atomic functions,
and to a more complicated components such as [*context.Context*](https://golang.org/pkg/context/). Unfortunately,
using them to solve problems arising in day-to-day programming might be quite challenging, especially for novices.

The goal of the _Job design pattern_ and this implementation is to provide an easy-to-use and solid foundation
for solving problems involving concurrent executions and their control. The Job pattern in many cases can be viewed as
an alternative to [*context.Context*](https://golang.org/pkg/context/), though it's not meant to completely replace it.   

## Documentation
### Why?
I know, this is probably the most frequent question a skeptical programmer might ask. "Why should I learn how to use it,
when we already have [*context.Context*](https://golang.org/pkg/context/), your solution looks like an over-engineered one".
Well, because I have a weakness for Gimp, allow me to retort in a sarcastic way to such criticism:
<img align="center" style="margin: 6px" src="https://raw.githubusercontent.com/AgentCoop/go-work/master/docs/funny-software-eng.png" alt='funny Software Engineering' aria-label='' />
<br>
But seriously, simplicity in software engineering is often a reciprocal for productivity. Never underestimate the benefits
of going one abstraction level up.

#### What is a Job?
A job is a set of concurrently running tasks, execution of which depends on each other. If one task fails, the whole job
execution fails too.
```go
    myjob := job.NewJob(nil)
    myjob.AddTask(task1)
    myjob.AddTask(task2)
    <-myjob.Run()
    // Let's process the result
```
Here is a simple example: https://play.golang.org/p/vec2ybo3ti9

#### What is a Task?
A single task consists of the three routines: an initialization routine, a recurrent routine, and a finalization routine:
```go
func (stream *stream) ReadOnStreamTask(j job.Job) (job.Init, job.Run, job.Finalize) {
    init := func(task job.Task){
        // Do some initialization
    }
    run := func(task job.Task) {
        read(stream, task)
        task.Tick()
    }
    fin := func(task job.Task) {
        readCancel(stream, task)
    }
    return init, run, fin
}
```
The recurrent routine is running in an indefinite loop. It represents well-known `for { select {  } }` statements in
Go. The recurrent routine calls three special methods:
 * **.Tick()** - to proceed task execution.
 * **.Done()** - to finish task execution (or **.FinishJob()** to finish job execution as well).
 * **.Idle()** - to tell that a task has nothing to do, and as a result it might be finished by reaching the idle timeout.

Tasks can assert some conditions, replacing `if err != nil { panic(err) }` by a more terse way:
```go
func (m *MyTask) MyTask(j job.Job) (job.Init, job.Run, job.Finalize) {
    run := func(task *job.TaskInfo) {
        _, err := os.Open("badfile")
        task.Assert(err)
    }
}
```
Every failed assertion will result in the cancellation of job execution, and invocation of the finalization routines of all
tasks of the job being cancelled.

There are two types of tasks: an ordinary task (or recurrent), and an oneshot task. The main purpose of a oneshot task
is to start off execution of other recurrent tasks waiting for the oneshot to be finished. You probably heard of such
an approach in software design as [Design by contract](https://en.wikipedia.org/wiki/Design_by_contract). Here we have
similar approach: in the example below recurrent tasks make an assumption that they will be running when a network
connection is established and the goal of the oneshot task (_mngr.ConnectTask_) is to fulfill that precondition, providing
reference to the connection in Job [value](#public-functions).
```go
    mainJob := j.NewJob(nil)
    mainJob.AddOneshotTask(mngr.ConnectTask)
    mainJob.AddTask(netmanager.ReadTask)
    mainJob.AddTask(netmanager.WriteTask)
    mainJob.AddTask(imgResizer.ScanForImagesTask)
    mainJob.AddTask(imgResizer.SaveResizedImageTask)
    <-mainJob.Run()
```
A job can have only one oneshot task.

For data sharing tasks should employ (although it's not an obligation) a ping/pong synchronization using two channels,
where the first one is being used to receive data and the second one - to notify the sender that data processing is completed.
```go
    run := func(task job.Task) {
        select {
        case data := <- p.conn.Upstream().RecvRaw(): // Receive data from upstream server
            p.conn.Downstream().Write() <- data // Write data to downstream server
            p.conn.Downstream().WriteSync() // sync with downstream data receiver
            p.conn.Upstream().RecvRawSync() // sync with upstream data sender
        }
        task.Tick()
    }
```

To prevent a task from being blocked for good, be sure to close all channels it's using in its finalization routine.

### Real life example
Now, when you have a basic understanding, let's put the given pattern to use and take a look at a
[real life example](https://github.com/AgentCoop/go-work-tcpbalancer):
the implementation of a reverse proxy working as layer 4 load balancer, a backend server resizing images, and a simple
client that would scan a specified directory for images and send them through the proxy server for resizing.
The code will speak for itself, so that you will quickly get the idea of how to use the given pattern.

### API reference
##### Public functions and variables
  * [_NewJob(value **interface{}**) ***job**_](#job-value) - creates a new job with the given job value.
  * RegisterDefaultLogger(logger **Logger**) - registers a fallback logger for all jobs.
  * DefaultLogLevel **int**
##### Job
  * [_AddTask(job **JobTask**) ***task**_](docs/job.md) - adds a new task to the job
  * [_GetTaskByIndex(index **int**) ***task**_](docs/job.md) - returns a task by the given index.
  * [_AddOneshotTask(job **JobTask**)_](docs/job.md) - adds an oneshot tasks to the job
  * [_AddTaskWithIdleTimeout(job **JobTask**, timeout **time.Duration**) ***task**_](docs/job.md) - adds a task with an idle timeout
  * [_WithPrerequisites(sigs ...**<-chan struct{}**)_](docs/job.md) - waits for the prerequisites to be met before executing the job.
  * [_WithTimeout(duration **time.Duration**)_](docs/job.md) - sets run timeout for the job. 
  * [_Run() **chan struct{}**_](docs/job.md) - starts the execution of the tasks.
  * [_RunInBackground() **<-chan struct{}**_](docs/job.md) - runs job's tasks in background. An oneshot task is required.
  Receives a signal from the returned channel once the oneshot task is finished.
  * [_Cancel(**err interface{}**)_](docs/job.md) - cancels the job with the given error.
  * [_Finish()_](docs/job.md) - finishes the job.
  * [_Log(level **int**) **chan<- interface{}**_](docs/job.md) - writes a log record using fallback or job's logger.
  * [_RegisterLogger(logger **Logger**)_](docs/job.md) - registers a job logger.
  * [_GetValue() **interface{}**_](docs/job.md) - returns a job value.
  * [_SetValue(v **interface{}**)_](docs/job.md) - sets a job value.
  * [_GetInterruptedBy() (***task, interface{}**)_](docs/job.md) - returns a task and an error that interrupted the job execution.
  * [_GetState() **JobState**_](docs/job.md) - returns the job state.
  *	[_TaskDoneNotify() **<-chan \*task**_](docs/job.md) -  receives **\*task** from the returned channel when a task is finished.
  *	[_JobDoneNotify() **chan struct{}**_](docs/job.md)- receives a signal from the returned channel when the job is finished
##### Task
  * [_GetIndex() **int**_](docs/task.md) - returns task index. Oneshot tasks have predefined 0-index.
  * [_GetJob() **Job**_](docs/task.md) - returns a reference to the task's job.
  * [_GetState() **TaskState**_](docs/task.md) - returns task state.
  * [_GetResult() **interface{}**_](docs/task.md) - returns task result.
  * [_SetResult(result **interface{}**)_](docs/task.md) - sets task result.
  * [_Tick()_](docs/task.md)<sup>1</sup> - proceeds task execution. 
  * [_Done()_](docs/task.md)<sup>1</sup> - marks task as finished and stops its execution.
  * [_Idle()_](docs/task.md)<sup>1</sup> - do nothing. Being used for tasks having idle timeouts.
  * [_FinishJob()_](docs/task.md) 
  * [_Assert(err **interface{}**)_](docs/task.md) - asserts that error has zero value.
  * [_AssertTrue(cond **bool**, err **string**)_](docs/task.md) - asserts that condition is evaluated to _true_, otherwise stops
  * [_AssertNotNil(value **interface{}**)_](docs/task.md)
 
###### Footnotes:
 1. Being called by the recurrent routine.