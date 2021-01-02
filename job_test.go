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
	return func(t *job.TaskInfo) {  }, func(t *job.TaskInfo) {
		mu.Lock()
		defer mu.Unlock()
		counter++
	}, func() { }
}

func squareJob(num int, sleep time.Duration) job.JobTask {
	return func(j job.JobInterface) (job.Init, job.Run, job.Cancel) {
		return func(t *job.TaskInfo) { }, func(t *job.TaskInfo) {
			if sleep > 0 { time.Sleep(sleep) }
			squaredNum := num * num
			t.SetResult(squaredNum)
			t.Done()
		}, func() { }
	}
}

func divideJob(num int, divider int, sleep time.Duration) job.JobTask {
	return func(j job.JobInterface) (job.Init, job.Run, job.Cancel) {
		return func(t *job.TaskInfo) {  }, func(t *job.TaskInfo) {
			if sleep > 0 {
				time.Sleep(sleep)
			}
			if divider == 0 {
				j.Assert("division by zero")
			}
			t.SetResult(num / divider)
			t.Done()
		}, func() { }
	}
}

func failedIOJob() job.JobTask {
	return func(j job.JobInterface) (job.Init, job.Run, job.Cancel) {
		return func(t *job.TaskInfo) { }, func(t *job.TaskInfo) {
			_, err := os.Open("foobar")
			j.Assert(err)
			t.Done()
		}, func() { }
	}
}

func sleepIncCounterJob(sleep time.Duration) job.JobTask {
	return func(j job.JobInterface) (job.Init, job.Run, job.Cancel) {
		return func(t *job.TaskInfo) {  }, func(t *job.TaskInfo) {
			if sleep > 0 {
				time.Sleep(sleep)
			}
			mu.Lock()
			counter++
			mu.Unlock()
			t.Done()
		}, nil
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
		return func(t *job.TaskInfo) {}, func(t *job.TaskInfo) {
				if counter != 2 {
					T.Fatalf("got %d, expected %d\n", counter, 2)
				}
				j.Cancel()
			}, func() {

			}
	})
	<-j.Run()
}

func TestDone(T *testing.T) {
	counter = 0
	nTasks := 50
	j := job.NewJob(nil)
	for i := 0; i < nTasks; i++ {
		j.AddTask(sleepIncCounterJob(time.Microsecond * time.Duration(i)))
	}
	<-j.Run()
	if ! j.IsDone() || counter != nTasks {
		T.Fatalf("expected: state %s, counter %d; got: %s, %d\n",
			job.Done, nTasks, j.GetState(), counter)
	}
}

func TestCancel(T *testing.T) {
	counter = 0
	nTasks := 10
	j := job.NewJob(nil)
	for i := 0; i < nTasks; i++ {
		j.AddTask(divideJob(9, 3, time.Microsecond * time.Duration(i)))
		j.AddTask(divideJob(9, 0, time.Microsecond * time.Duration(2 * i)))
	}
	<-j.Run()
	if ! j.IsCancelled() {
		T.Fatalf("expected: state %s; got: %s\n", job.Cancelled, j.GetState())
	}

	counter = 0
	nTasks = 10
	j = job.NewJob(nil)
	for i := 0; i < nTasks; i++ {
		j.AddTask(func(j job.JobInterface) (job.Init, job.Run, job.Cancel) {
			run := func(t *job.TaskInfo) {
				//time.Sleep(time.Millisecond)
				t.Done()
			}
			cancel := func() {
				mu.Lock()
				defer mu.Unlock()
				counter++
			}
			return nil, run, cancel
		})
	}
	j.AddTask(divideJob(9, 0, time.Microsecond * 10))
	<-j.Run()
	if ! j.IsCancelled() {
		T.Fatalf("expected: state %s; got: %s\n", job.Cancelled, j.GetState())
	}
	// Allocate enough time for finalize routines to finish
	time.Sleep(time.Millisecond * 50)
	if counter != nTasks {
		T.Fatalf("expected: counter %d; got: %d\n", nTasks, counter)
	}
}

func TestTimeout(T *testing.T) {
	// Must succeed
	counter = 0
	j := job.NewJob(nil).WithTimeout(200 * time.Millisecond)
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
	j.WithTimeout(20 * time.Millisecond)
	task1 := j.AddTask(sleepIncCounterJob(10 * time.Millisecond))
	task2 := j.AddTask(sleepIncCounterJob(99999 * time.Second)) // Must not block run method
	<-j.Run()

	if ! j.IsCancelled() || counter != 1 {
		T.Fatalf("expected: state %s, counter 1; got %s %d\n", job.Cancelled, j.GetState(), counter)
	}
	if task1.GetState() != job.FinishedTask {
		T.Fatalf("expected: task state %s; got %s\n", job.FinishedTask, task1.GetState())
	}
	if task2.GetState() != job.RunningTask {
		T.Fatalf("expected: task state %s; got %s\n", job.RunningTask, task2.GetState())
	}
}


func TestOneshot(T *testing.T) {
	j := job.NewJob(nil)
	j.AddOneshotTask(divideJob(9,3, time.Microsecond * 50))
	task1 := j.AddTask(squareJob(4, 0))
	<-j.Run()
	res := task1.GetResult()
	if ! j.IsCancelled() && res != 16  {
		T.Fatalf("expected: state %s, task result %d; got: %s %v\n", job.Done, 16, j.GetState(), res)
	}
	// Failed oneshot task
	j = job.NewJob(nil)
	j.AddOneshotTask(divideJob(3,0, time.Microsecond * 50))
	task1 = j.AddTask(squareJob(3, 0))
	<-j.Run()
	res = task1.GetResult()
	if ! j.IsCancelled() && res != nil  {
		T.Fatalf("expected: state %s, task result %v; got: %s %v\n", job.Cancelled, 0, j.GetState(), res)
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