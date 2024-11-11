package scheduler_test

import (
	"testing"
	"time"

	"github.com/bnixon67/scheduler"
)

// TestJobNewJobInterval verifies that NewJob panics on invalid interval.
func TestJobNewJobInterval(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("want NewJob to panic on invalid interval, but it did not")
		}
	}()

	// This should panic due to non-positive interval
	scheduler.NewJob("test", 0, func(*scheduler.Job) bool { return true })
}

// TestJobNewJobRunFunc verifies that NewJob panics on invalid run function.
func TestJobNewJobRunFunc(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("want NewJob to panic on nil run function, but it did not")
		}
	}()

	// This should panic due to nil run function
	scheduler.NewJob("test", time.Second, nil)
}

// TestJobString verifies the String method.
func TestJobString(t *testing.T) {
	job := scheduler.NewJob(
		"test",
		1*time.Second,
		func(*scheduler.Job) bool { return true },
	)

	got := job.String()
	want := "Job{id: test, interval: 1s, maxExecutions: 0, executions: 0, stopOnPanic: false, isStopped: false}"
	if got != want {
		t.Errorf("\ngot  %v,\nwant %v\nfor job.String()",
			got, want)
	}

	job.Stop()

	got = job.String()
	want = "Job{id: test, interval: 1s, maxExecutions: 0, executions: 0, stopOnPanic: false, isStopped: true}"
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
		t.Errorf("got %v, want %v for job.ID()", got, want)
	}
}

// TestJobInterval verifies that Interval() correctly returns the Job Interval.
func TestJobInterval(t *testing.T) {
	jobID := "test"
	jobInterval := 42 * time.Second

	job := scheduler.NewJob(
		jobID,
		jobInterval,
		func(*scheduler.Job) bool { return true },
	)

	got := job.Interval()
	want := jobInterval
	if got != want {
		t.Errorf("got %v, want %v for job.Interval()", got, want)
	}
}
