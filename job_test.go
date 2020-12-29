package job_test

import (
	"github.com/AgentCoop/go-work"
	"os"
	"sync"
	"testing"
	"time"
)

var counter int
var mu sync.Mutex

func incCounterJob(j job.JobInterface) (job.Init, job.Run, job.Cancel) {
	return func() {  }, func(t *job.TaskInfo) interface{} {
		mu.Lock()
		defer mu.Unlock()
		counter++
		return counter
	}, func() { }
}

func squareJob(num int) job.JobTask {
	return func(j job.JobInterface) (job.Init, job.Run, job.Cancel) {
		return func() { }, func(t *job.TaskInfo) interface{} {
			return num * num
		}, func() { }
	}
}

func divideJob(num int, divider int, sleep time.Duration) job.JobTask {
	return func(j job.JobInterface) (job.Init, job.Run, job.Cancel) {
		return func() {  }, func(t *job.TaskInfo) interface{} {
			if sleep > 0 {
				time.Sleep(sleep)
			}
			if divider == 0 {
				j.Assert("division by zero")
			}
			return num / divider
		}, func() { }
	}
}

func failedIOJob() job.JobTask {
	return func(j job.JobInterface) (job.Init, job.Run, job.Cancel) {
		return func() { }, func(t *job.TaskInfo) interface{} {
			_, err := os.Open("foobar")
			j.Assert(err)
			return true
		}, func() { }
	}
}

func sleepIncCounterJob(sleep time.Duration) job.JobTask {
	return func(j job.JobInterface) (job.Init, job.Run, job.Cancel) {
		return func() {  }, func(t *job.TaskInfo) interface{} {
			if sleep > 0 {
				time.Sleep(sleep)
			}
			mu.Lock()
			defer mu.Unlock()
			counter++
			return counter
		}, func() { }
	}
}

func signalAfter(t time.Duration, fn func()) chan struct{} {
	ch := make(chan struct{})
	go func() {
		time.Sleep(t)
		if fn != nil {
			fn()
		}
		ch <- struct{}{}
	}()
	return ch
}

func TestPrereq(T *testing.T) {
	var counter int
	p1 := signalAfter(10 * time.Millisecond, func() { counter++ })
	p2 := signalAfter(20 * time.Millisecond, func() { counter++ })
	j := job.NewJob(nil)
	j.WithPrerequisites(p1, p2)
	j.AddTask(func(j job.JobInterface) (job.Init, job.Run, job.Cancel) {
		return func() {}, func(t *job.TaskInfo) interface{} {
				if counter != 2 {
					T.Fatalf("got %d, expected %d\n", counter, 2)
				}
				j.Cancel()
				return false
			}, func() {

			}
	})
	<-j.Run()
}

func TestDone(T *testing.T) {
	counter = 0
	nTasks := 10
	j := job.NewJob(nil)
	for i := 0; i < nTasks; i++ {
		j.AddTask(sleepIncCounterJob(time.Microsecond * time.Duration(i)))
	}
	<-j.Run()
	if ! j.IsDone() || j.GetFailedTasksNum() != 0 {
		T.Fatalf("expected: state %s, failed tasks count %d; got: %s, %d\n",
			job.Done, 0, j.GetState(), j.GetFailedTasksNum())
	}
}

func TestCancel(T *testing.T) {
	counter = 0
	nTasks := 100
	j := job.NewJob(nil)
	for i := 0; i < nTasks; i++ {
		j.AddTask(divideJob(9, 3, time.Microsecond * time.Duration(10 * i)))
	}
	for i := 0; i < nTasks; i++ {
		j.AddTask(divideJob(9, 0, time.Microsecond * time.Duration(i)))
	}
	<-j.Run()
	time.Sleep(5 * time.Millisecond)
	if ! j.IsCancelled() || j.GetFailedTasksNum() != uint32(nTasks) {
		T.Fatalf("expected: state %s, failed tasks count %d; got: %s, %d\n",
			job.Cancelled, 100, j.GetState(), j.GetFailedTasksNum())
	}
}

func TestTimeout(T *testing.T) {
	// Must succeed
	counter = 0
	j := job.NewJob(nil).WithTimeout(120 * time.Millisecond)
	for i := 0; i < 100; i++ {
		j.AddTask(sleepIncCounterJob(time.Duration(i + 1) * time.Millisecond))
	}
	<-j.Run()
	if ! j.IsDone() || counter != 100 {
		T.Fatalf("expected counter 100, got %d\n", counter)
	}
	// Must be cancelled
	counter = 0
	j = job.NewJob(nil)
	j.WithTimeout(15 * time.Millisecond)
	j.AddTask(sleepIncCounterJob(10 * time.Millisecond))
	j.AddTask(sleepIncCounterJob(99999 * time.Second)) // Must not block run method
	<-j.Run()
	if ! j.IsCancelled() || counter != 1 {
		T.Fail()
	}
}

func TestTaskResult(T *testing.T) {
	// Must succeed
	counter = 0
	j := job.NewJob(nil).WithTimeout(10 * time.Millisecond)
	task1 := j.AddTask(squareJob(3))
	task2 := j.AddTask(sleepIncCounterJob(20 * time.Millisecond))
	<-j.Run()
	if ! j.IsCancelled() || counter != 0 {
		T.Fatalf("expected: counter 0, state Done; got: %d %s\n", counter, j.GetState())
	}
	select {
	case num := <- task1.GetResult():
		if num != 9 { T.Fatalf("expected: 0; got: %d\n", num) }
	case <- task2.GetResult():
		T.Fatal()
	}
}

func TestAssert(T *testing.T) {
	// Must succeed
	counter = 0
	nTasks := 100
	j := job.NewJob(nil)
	for i := 0; i < nTasks; i++ {
		j.AddTask(divideJob(9, 3, 0))
		j.AddTask(divideJob(9, 0, 0))
		j.AddTask(divideJob(9, 9, 0))
	}
	<-j.Run()
	if ! j.IsCancelled() {
		T.Fatalf("expected: state %s; got: state %s", job.Cancelled, j.GetState())
	}
	time.Sleep(50 * time.Millisecond)
	if j.GetFailedTasksNum() != uint32(nTasks) {
		T.Fatalf("expected: %d; got: %d\n", nTasks, j.GetFailedTasksNum())
	}
	select {
	case err := <- j.GetError():
		if err != "division by zero" {
			T.Fatal()
		}
	}
	j = job.NewJob(nil)
	j.AddTask(failedIOJob())
	j.AddTask(failedIOJob())
	<-j.Run()
	if ! j.IsCancelled() {
		T.Fatalf("expected: state %s; got: state %s", job.Cancelled, j.GetState())
	}
}