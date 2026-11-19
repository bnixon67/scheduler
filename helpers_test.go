package scheduler_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bnixon67/scheduler"
)

// mustNew calls scheduler.New and fails the test if it returns an error.
func mustNew(t testing.TB, maxConcurrent int,
	opts ...scheduler.Option,
) *scheduler.Scheduler {
	t.Helper()
	s, err := scheduler.New(maxConcurrent, opts...)
	if err != nil {
		t.Fatalf("New(%d) = %v", maxConcurrent, err)
	}
	return s
}

// mustSchedule calls s.Schedule and fails the test if it returns an error.
func mustSchedule(t testing.TB, s *scheduler.Scheduler, name string,
	interval time.Duration, run scheduler.RunFunc, opts ...scheduler.JobOption,
) *scheduler.Job {
	t.Helper()
	job, err := s.Schedule(name, interval, run, opts...)
	if err != nil {
		t.Fatalf("Schedule(%q, %v) = %v", name, interval, err)
	}
	return job
}

// keepRunning is a RunFunc that always asks to run again.
func keepRunning(context.Context, *scheduler.Job) error { return nil }

// ignoreErrors is an error handler that does nothing, for tests whose jobs
// fail on purpose.
func ignoreErrors(*scheduler.Job, error) {}

// trackMax records the highest value seen of a concurrently updated count.
func trackMax(cur, max *atomic.Int32) {
	c := cur.Add(1)
	for {
		m := max.Load()
		if c <= m || max.CompareAndSwap(m, c) {
			return
		}
	}
}
