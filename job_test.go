package job_test

import (
	j "github.com/AgentCoop/go-work"
	"os"
	"sync"
	"testing"
	"time"
)

var counter int
var mu sync.Mutex

func t(info *j.TaskInfo) {
	info.GetResult()
}

func incCounterJob(j j.Job) (func(), func() interface{}, func()) {
	return func() {  }, func() interface{} {
		mu.Lock()
		defer mu.Unlock()
		counter++
		return counter
	}, func() { }
}

func squareJob(num int) j.JobTask {
	return func(j j.Job) (func(), func() interface{}, func()) {
		return func() { }, func() interface{} {
			return num * num
		}, func() { }
	}
}

func divideJob(num int, divider int, sleep time.Duration) j.JobTask {
	return func(j j.Job) (func(), func() interface{}, func()) {
		return func() {  }, func() interface{} {
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

func failedIOJob() j.JobTask {
	return func(j j.Job) (func(), func() interface{}, func()) {
		return func() { }, func() interface{} {
			_, err := os.Open("foobar")
			j.Assert(err)
			return true
		}, func() { }
	}
}

func sleepIncCounterJob(sleep time.Duration) j.JobTask {
	return func(j j.Job) (func(), func() interface{}, func()) {
		return func() {  }, func() interface{} {
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
	job := j.NewJob(nil)
	job.WithPrerequisites(p1, p2)
	job.AddTask(func(j j.Job) (func(), func() interface{}, func()) {
		return func() {}, func() interface{} {
				if counter != 2 {
					T.Fatalf("got %d, expected %d\n", counter, 2)
				}
				j.Cancel()
				return false
			}, func() {

			}
	})
	<-job.Run()
}

func TestDone(T *testing.T) {
	counter = 0
	nTasks := 10
	job := j.NewJob(nil)
	for i := 0; i < nTasks; i++ {
		job.AddTask(sleepIncCounterJob(time.Microsecond * time.Duration(i)))
	}
	<-job.Run()
	if ! job.IsDone() || job.GetFailedTasksNum() != 0 {
		T.Fatalf("expected: state %s, failed tasks count %d; got: %s, %d\n",
			j.Done, 0, job.GetState(), job.GetFailedTasksNum())
	}
}

func TestCancel(T *testing.T) {
	counter = 0
	nTasks := 100
	job := j.NewJob(nil)
	for i := 0; i < nTasks; i++ {
		job.AddTask(divideJob(9, 3, time.Microsecond * time.Duration(100 * i)))
	}
	for i := 0; i < nTasks; i++ {
		job.AddTask(divideJob(9, 0, time.Microsecond * time.Duration(i)))
	}
	<-job.Run()
	time.Sleep(2000 * time.Millisecond)
	if ! job.IsCancelled() || job.GetFailedTasksNum() != uint32(nTasks) {
		T.Fatalf("expected: state %s, failed tasks count %d; got: %s, %d\n",
			j.Cancelled, 100, job.GetState(), job.GetFailedTasksNum())
	}
}

func TestTimeout(T *testing.T) {
	// Must succeed
	counter = 0
	job := j.NewJob(nil).WithTimeout(120 * time.Millisecond)
	for i := 0; i < 100; i++ {
		job.AddTask(sleepIncCounterJob(time.Duration(i + 1) * time.Millisecond))
	}
	<-job.Run()
	if ! job.IsDone() || counter != 100 {
		T.Fatalf("expected counter 100, got %d\n", counter)
	}
	// Must be cancelled
	counter = 0
	job = j.NewJob(nil)
	job.WithTimeout(15 * time.Millisecond)
	job.AddTask(sleepIncCounterJob(10 * time.Millisecond))
	job.AddTask(sleepIncCounterJob(99999 * time.Second)) // Must not block run method
	<-job.Run()
	if ! job.IsCancelled() || counter != 1 {
		T.Fail()
	}
}

func TestTaskResult(T *testing.T) {
	// Must succeed
	counter = 0
	job := j.NewJob(nil).WithTimeout(10 * time.Millisecond)
	task1 := job.AddTask(squareJob(3))
	task2 := job.AddTask(sleepIncCounterJob(20 * time.Millisecond))
	<-job.Run()
	if ! job.IsCancelled() || counter != 0 {
		T.Fatalf("expected: counter 0, state Done; got: %d %s\n", counter, job.GetState())
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
	job := j.NewJob(nil)
	for i := 0; i < nTasks; i++ {
		job.AddTask(divideJob(9, 3, 0))
		job.AddTask(divideJob(9, 0, 0))
		job.AddTask(divideJob(9, 9, 0))
	}
	<-job.Run()
	if ! job.IsCancelled() {
		T.Fatalf("expected: state %s; got: state %s", j.Cancelled, job.GetState())
	}
	time.Sleep(50 * time.Millisecond)
	if job.GetFailedTasksNum() != uint32(nTasks) {
		T.Fatalf("expected: %d; got: %d\n", nTasks, job.GetFailedTasksNum())
	}
	select {
	case err := <- job.GetError():
		if err != "division by zero" {
			T.Fatal()
		}
	}
	job = j.NewJob(nil)
	job.AddTask(failedIOJob())
	job.AddTask(failedIOJob())
	<-job.Run()
	if ! job.IsCancelled() {
		T.Fatalf("expected: state %s; got: state %s", j.Cancelled, job.GetState())
	}
}