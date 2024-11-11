package scheduler

import (
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// TestJobNewJob verifies NewJob correctly creates a new job.
func TestJobNewJob(t *testing.T) {
	id := "test_job"
	interval := 1 * time.Second
	runFunc := func(*Job) bool { return true }

	// Call NewJob with valid parameters
	job := NewJob(id, interval, runFunc)

	// Check that the job has been initialized with the correct values
	if job.ID() != id {
		t.Errorf("Expected job ID to be %s, got %s", id, job.ID())
	}
	if job.Interval() != interval {
		t.Errorf("Expected job interval to be %v, got %v", interval, job.Interval())
	}
	if reflect.ValueOf(job.runFunc).Pointer() != reflect.ValueOf(runFunc).Pointer() {
		t.Errorf("Expected job run function to match, but it did not")
	}
	if job.logger == nil {
		t.Error("Expected logger to be initialized, but got nil")
	}
	if job.cancelCtx == nil {
		t.Error("Expected ctx to be initialized, but got nil")
	}
	if job.cancelFunc == nil {
		t.Error("Expected cancel to be initialized, but got nil")
	}
}

// TestStop verifies if Stop correctly sets the job to be stopped.
func TestJobStop(t *testing.T) {
	job := NewJob("test", time.Second, func(*Job) bool { return true })
	job.Stop()

	got := job.IsStopped()
	want := true
	if got != want {
		t.Errorf("got %t, want %t for job.isClosed()",
			got, want)
	}
}

// TestJobExecuteWithPanicContinue verifies if the job returns true upon
// panic with default value of false for stopOnPanic.
func TestJobExecuteWithPanicContinue(t *testing.T) {
	var executions atomic.Int32

	job := NewJob(
		"test",
		time.Second,
		func(j *Job) bool {
			executions.Add(1)
			panic("simulated panic")
			return true
		},
		WithRecoveryHandler(nil),
	)

	shouldContinue := job.execute()

	if !shouldContinue {
		t.Errorf("wanted job to continue after panic, but it did not")
	}

	if executions.Load() != 1 {
		t.Errorf("got %d executions, wanted 1", executions.Load())
	}
}

// TestJobExecuteWithPanicContinue verifies if the job returns false upon
// panic with stopOnPanic set to true.
func TestJobExecuteWithPanicDontContinue(t *testing.T) {
	var executions atomic.Int32

	job := NewJob(
		"test",
		time.Second,
		func(j *Job) bool {
			executions.Add(1)
			panic("simulated panic")
			return true
		},
		WithRecoveryHandler(nil),
		WithStopOnPanic(true),
	)

	shouldContinue := job.execute()

	if shouldContinue {
		t.Errorf("wanted job to stop after panic, but it did not")
	}

	if executions.Load() != 1 {
		t.Errorf("got %d executions, wanted 1", executions.Load())
	}
}
