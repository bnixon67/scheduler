package scheduler_test

import (
	"testing"
	"time"

	"github.com/bnixon67/scheduler"
)

// TestJobString verifies the String method correctly reflects job status.
func TestJobString(t *testing.T) {
	job := scheduler.NewJob("test", 1*time.Second, func(id string) {})
	expected := "Job{id: test, interval: 1s, isStopped: false}"

	if job.String() != expected {
		t.Errorf("Expected %v, but got %v", expected, job.String())
	}

	job.Stop()
	expected = "Job{id: test, interval: 1s, isStopped: true}"
	if job.String() != expected {
		t.Errorf("Expected %v after stop, but got %v", expected, job.String())
	}
}
