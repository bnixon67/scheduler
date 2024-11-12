package scheduler_test

import (
	"testing"
	"time"

	"github.com/bnixon67/scheduler"
)

// TestNewJobIntervalZero verifies that NewJob panics on zero interval.
func TestNewJobIntervalZero(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewJob did not panic on invalid interval")
		}
	}()

	// This should panic due to non-positive interval
	scheduler.NewJob("test", 0, func(*scheduler.Job) bool { return true })
}

// TestNewJobIntervalNegative verifies that NewJob panics on negative interval.
func TestNewJobIntervalNegative(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewJob did not panic on negative interval")
		}
	}()

	// This should panic due to non-positive interval
	scheduler.NewJob("test", -1*time.Second, func(*scheduler.Job) bool { return true })
}

// TestNewJobRunNil verifies that NewJob panics on nil run function.
func TestNewJobRunNil(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewJob did not panic on nil run function")
		}
	}()

	// This should panic due to non-positive interval
	scheduler.NewJob("test", time.Second, nil)
}

// TestJobString verifies the String method.
func TestJobString(t *testing.T) {
	job := scheduler.NewJob(
		"test",
		time.Second,
		func(*scheduler.Job) bool { return true },
	)

	got := job.String()
	want := "Job{id: test, interval: 1s, stopOnPanic: false, maxExecutions: 0, executions: 0, isStopped: false}"
	if got != want {
		t.Errorf("\ngot  %v,\nwant %v\nfor job.String()",
			got, want)
	}

	job.Stop()

	got = job.String()
	want = "Job{id: test, interval: 1s, stopOnPanic: false, maxExecutions: 0, executions: 0, isStopped: true}"
	if got != want {
		t.Errorf("\ngot  %v,\nwant %v\nfor job.String()",
			got, want)
	}
}

// TestJobID verifies that ID() correctly returns the Job ID.
func TestJobID(t *testing.T) {
	jobID := "test"

	job := scheduler.NewJob(
		jobID,
		time.Second,
		func(*scheduler.Job) bool { return true },
	)

	got := job.ID()
	want := jobID
	if got != want {
		t.Errorf("ID() = %q, want %q", got, want)
	}
}

// TestJobInterval verifies that Interval() correctly returns the Job Interval.
func TestJobInterval(t *testing.T) {
	jobInterval := 42 * time.Second

	job := scheduler.NewJob(
		"test",
		jobInterval,
		func(*scheduler.Job) bool { return true },
	)

	got := job.Interval()
	want := jobInterval
	if got != want {
		t.Errorf("Interval() = %v, want %v", got, want)
	}
}
