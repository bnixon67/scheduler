package scheduler

import (
	"reflect"
	"testing"
	"time"
)

// TestJobNewJob verifies NewJob correctly creates a new job.
func TestJobNewJob(t *testing.T) {
	id := "test_job"
	interval := 1 * time.Second
	runFunc := func(jobID string) {}

	// Call NewJob with valid parameters
	job := NewJob(id, interval, runFunc)

	// Check that the job has been initialized with the correct values
	if job.ID() != id {
		t.Errorf("Expected job ID to be %s, got %s", id, job.ID())
	}
	if job.Interval() != interval {
		t.Errorf("Expected job interval to be %v, got %v", interval, job.Interval())
	}
	if reflect.ValueOf(job.run).Pointer() != reflect.ValueOf(runFunc).Pointer() {
		t.Errorf("Expected job run function to match, but it did not")
	}
	if job.stopCh == nil {
		t.Error("Expected job stopCh to be initialized, but got nil")
	}
}

// TestStop verifies if Stop correctly sets the job to be stopped.
func TestJobStop(t *testing.T) {
	job := NewJob("test", time.Second, func(string) {})
	job.Stop()

	got := isChannelClosed(job.stopCh)
	want := true
	if got != want {
		t.Errorf("got %t, want %t for isChannelClosed(job.stopCh)",
			got, want)
	}
}
