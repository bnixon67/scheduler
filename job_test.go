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
	scheduler.NewJob("test", 0, func(id string) {})
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
	want := "Job{id: test, interval: 1s, isStopped: false}"

	job := scheduler.NewJob("test", 1*time.Second, func(id string) {})

	if job.String() != want {
		t.Errorf("\ngot  %v,\nwant %v\nfor job.String()",
			want, job.String())
	}

	job.Stop()
	want = "Job{id: test, interval: 1s, isStopped: true}"
	if job.String() != want {
		t.Errorf("\ngot  %v,\nwant %v\nfor job.String()",
			want, job.String())
	}
}

// TestJobID verifies that ID() correctly returns the Job ID.
func TestJobID(t *testing.T) {
	jobID := "test"

	job := scheduler.NewJob(jobID, time.Second, func(id string) {})

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

	job := scheduler.NewJob(jobID, jobInterval, func(id string) {})

	got := job.Interval()
	want := jobInterval
	if got != want {
		t.Errorf("got %v, want %v for job.Interval()", got, want)
	}
}
