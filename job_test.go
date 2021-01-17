package job_test

import (
	"errors"
	"github.com/AgentCoop/go-work"
	"os"
	"sync"
	"testing"
	"time"
)

var counter int
var mu sync.Mutex

func squareNumTask(num int, sleep time.Duration) job.JobTask {
	return func(j job.Job) (job.Init, job.Run, job.Finalize) {
		return func(t job.Task) { }, func(t job.Task) {
			if sleep > 0 { time.Sleep(sleep) }
			squaredNum := num * num
			t.SetResult(squaredNum)
			t.Done()
		}, nil
	}
}

func divideNumTask(num int, divider int, sleep time.Duration) job.JobTask {
	return func(j job.Job) (job.Init, job.Run, job.Finalize) {
		return func(t job.Task) {  }, func(t job.Task) {
			if sleep > 0 {
				time.Sleep(sleep)
			}
			if divider == 0 {
				t.Assert("division by zero")
			}
			t.SetResult(num / divider)
			t.Done()
		}, nil
	}
}

func failedIOTask() job.JobTask {
	return func(j job.Job) (job.Init, job.Run, job.Finalize) {
		return func(t job.Task) { }, func(t job.Task) {
			_, err := os.Open("foobar")
			t.Assert(err)
			t.Done()
		}, nil
	}
}

func sleepIncCounterTask(sleep time.Duration) job.JobTask {
	return func(j job.Job) (job.Init, job.Run, job.Finalize) {
		return func(t job.Task) {  }, func(t job.Task) {
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

func TestDone(T *testing.T) {
	counter = 0
	nTasks := 50
	j := job.NewJob(nil)
	for i := 0; i < nTasks; i++ {
		j.AddTask(sleepIncCounterTask(time.Microsecond * time.Duration(i)))
	}
	<-j.Run()
	if j.GetState() != job.Done || counter != nTasks {
		T.Fatalf("expected: state %s, counter %d; got: %s, %d\n",
			job.Done, nTasks, j.GetState(), counter)
	}
}

func TestCancel(T *testing.T) {
	counter = 0
	nTasks := 10
	j := job.NewJob(nil)
	for i := 0; i < nTasks; i++ {
		j.AddTask(divideNumTask(9, 3, time.Microsecond * time.Duration(i)))
		j.AddTask(divideNumTask(9, 0, time.Microsecond * time.Duration(2 * i)))
	}
	<-j.Run()
	if j.GetState() != job.Cancelled {
		T.Fatalf("expected: state %s; got: %s\n", job.Cancelled, j.GetState())
	}

	counter = 0
	nTasks = 10
	j = job.NewJob(nil)
	for i := 0; i < nTasks; i++ {
		j.AddTask(func(j job.Job) (job.Init, job.Run, job.Finalize) {
			run := func(t job.Task) {
				//time.Sleep(time.Millisecond)
				t.Done()
			}
			cancel := func(task job.Task) {
				mu.Lock()
				defer mu.Unlock()
				counter++
			}
			return nil, run, cancel
		})
	}
	j.AddTask(divideNumTask(9, 0, time.Microsecond * 10))
	<-j.Run()
	if j.GetState() != job.Cancelled {
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
	j := job.NewJob(nil)
	j.WithTimeout(200 * time.Millisecond)
	for i := 0; i < 100; i++ {
		j.AddTask(sleepIncCounterTask(time.Duration(i + 1) * time.Millisecond))
	}
	<-j.Run()
	if j.GetState() != job.Done || counter != 100 {
		T.Fatalf("expected counter 100, got %d\n", counter)
	}
	// Must be cancelled
	counter = 0
	j = job.NewJob(nil)
	j.WithTimeout(20 * time.Millisecond)
	task1 := j.AddTask(sleepIncCounterTask(10 * time.Millisecond))
	task2 := j.AddTask(sleepIncCounterTask(99999 * time.Second)) // Must not block run method
	<-j.Run()

	if j.GetState() != job.Cancelled || counter != 1 {
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
	j.AddOneshotTask(divideNumTask(9,3, time.Microsecond * 50))
	task1 := j.AddTask(squareNumTask(4, 0))
	<-j.Run()
	res := task1.GetResult()
	if j.GetState() != job.Cancelled && res != 16  {
		T.Fatalf("expected: state %s, task result %d; got: %s %v\n", job.Done, 16, j.GetState(), res)
	}
	// Failed oneshot task
	j = job.NewJob(nil)
	j.AddOneshotTask(divideNumTask(3,0, time.Microsecond * 50))
	task1 = j.AddTask(squareNumTask(3, 0))
	<-j.Run()
	res = task1.GetResult()
	if j.GetState() != job.Cancelled && res != nil  {
		T.Fatalf("expected: state %s, task result %v; got: %s %v\n", job.Cancelled, 0, j.GetState(), res)
	}
}

func TestOneshotBg(T *testing.T) {
	j := job.NewJob(nil)
	j.AddOneshotTask(func(j job.Job) (job.Init, job.Run, job.Finalize) {
		run := func(t job.Task) {
			time.Sleep(time.Millisecond * 10)
			counter = 1
			t.Done()
		}
		return nil, run, nil
	})
	j.AddTask(func(j job.Job) (job.Init, job.Run, job.Finalize) {
		run := func(t job.Task) {
			time.Sleep(time.Millisecond * 5)
			counter = 2
			t.Done()
		}
		return nil, run, nil
	})
	<-j.RunInBackground()
	if j.GetState() != job.RecurrentRunning || counter != 1 {
		T.Fatalf("expected: state %s, counter %d; got: %s %d\n", job.OneshotRunning, 1, j.GetState(), counter)
	}
	select {
	case <- j.JobDoneNotify():
		if j.GetState() != job.Done || counter != 2 {
			T.Fatalf("expected: state %s, counter %d; got: %s %d\n", job.Done, 2, j.GetState(), counter)
		}
	}
}

func TestAssert(T *testing.T) {
	// Must succeed
	counter = 0
	nTasks := 100
	j := job.NewJob(nil)
	failedTasks := make([]job.Task, 100)
	for i := 0; i < nTasks; i++ {
		j.AddTask(divideNumTask(9, 3, 0))
		failedTasks[i] = j.AddTask(divideNumTask(9, 0, 0))
		j.AddTask(divideNumTask(9, 9, 0))
	}
	<-j.Run()
	if j.GetState() != job.Cancelled {
		T.Fatalf("expected: state %s; got: state %s", job.Cancelled, j.GetState())
	}
	_, err := j.GetInterruptedBy()
	if err != "division by zero" {
		T.Fatal()
	}
	j = job.NewJob(nil)
	j.AddTask(failedIOTask())
	j.AddTask(failedIOTask())
	<-j.Run()
	if j.GetState() != job.Cancelled {
		T.Fatalf("expected: state %s; got: state %s", job.Cancelled, j.GetState())
	}
}

func TestIdleTimedout(T *testing.T) {
	j := job.NewJob(nil)
	doneTimer := time.After(time.Millisecond * 31)
	j.AddTaskWithIdleTimeout(func(j job.Job) (job.Init, job.Run, job.Finalize) {
		run := func(task job.Task) {
			select {
			case <- doneTimer:
				task.Done()
			default:
				task.Idle()
			}
		}
		return nil, run, nil
	}, time.Millisecond * 29)
	<-j.Run()
	_, err := j.GetInterruptedBy()
	if j.GetState() != job.Cancelled && err != job.ErrTaskIdleTimeout {
		T.Fatalf("expected: state %s, error='%s'; got: %s, '%v'", job.Cancelled, job.ErrTaskIdleTimeout, j.GetState(), err)
	}
}

func TestIdle(T *testing.T) {
	j := job.NewJob(nil)
	doneTimer := time.After(time.Millisecond * 19)
	j.AddTaskWithIdleTimeout(func(j job.Job) (job.Init, job.Run, job.Finalize) {
		run := func(task job.Task) {
			select {
			case <- doneTimer:
				task.Done()
			default:
				task.Idle()
			}
		}
		return nil, run, nil
	}, time.Millisecond * 21)
	<-j.Run()
	_, err := j.GetInterruptedBy()
	if j.GetState() != job.Done && err != nil {
		T.Fatalf("expected: state %s, error='%v'; got: %s, '%v'", job.Done, nil, j.GetState(), err)
	}
}

func TestCatchPanic(T *testing.T) {
	var errUnexpected = errors.New("Unexpected error")
	j := job.NewJob(nil)
	j.AddTask(func(j job.Job) (job.Init, job.Run, job.Finalize) {
		run := func(task job.Task) {
			time.Sleep(time.Millisecond * 10)
			panic(errUnexpected)
		}
		return nil, run, nil
	})
	<-j.Run()
	_, err := j.GetInterruptedBy()
	if j.GetState() != job.Cancelled && err != errUnexpected {
		T.Fatalf("expected: state %s, err %s; got: %s, %s", job.Cancelled, j.GetState(), errUnexpected, err)
	}
	// Init panic
	var errInitUnexpected = errors.New("Init unexpected error")
	j = job.NewJob(nil)
	j.AddTask(func(j job.Job) (job.Init, job.Run, job.Finalize) {
		init := func(task job.Task) {
			panic(errInitUnexpected)
		}
		run := func(task job.Task) {
			time.Sleep(time.Millisecond * 10)
			panic(errUnexpected)
		}
		return init, run, nil
	})
	<-j.Run()
	_, err = j.GetInterruptedBy()
	if j.GetState() != job.Cancelled && err != errInitUnexpected {
		T.Fatalf("expected: state %s, err %s; got: %s, %s", job.Cancelled, j.GetState(), errUnexpected, err)
	}
}
