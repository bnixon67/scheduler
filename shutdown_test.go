package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bnixon67/scheduler"
)

// scheduleSlow schedules a job named "slow" whose run takes d, or returns
// early if its context is canceled. It sends ctx.Err() on result when each
// run returns.
func scheduleSlow(t *testing.T, s *scheduler.Scheduler, d time.Duration) (
	job *scheduler.Job, result chan error,
) {
	result = make(chan error, 100)
	job = mustSchedule(t, s, "slow", time.Second,
		func(ctx context.Context, _ *scheduler.Job) error {
			select {
			case <-time.After(d):
			case <-ctx.Done():
			}
			result <- ctx.Err()
			return nil
		})
	synctest.Wait() // the first run has started
	return job, result
}

// TestShutdownWaitsForRun verifies Shutdown lets a run in progress finish
// with its context intact, and starts no further runs.
func TestShutdownWaitsForRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		job, result := scheduleSlow(t, s, 5*time.Second)

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()

		start := time.Now()
		if err := s.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown = %v, want nil", err)
		}
		if got := time.Since(start); got != 5*time.Second {
			t.Errorf("Shutdown took %v, want 5s", got)
		}
		if err := <-result; err != nil {
			t.Errorf("run saw ctx.Err() = %v, want nil", err)
		}
		if got := job.Runs(); got != 1 {
			t.Errorf("Runs = %d, want 1", got)
		}
		if !job.Stopped() {
			t.Error("job not stopped after Shutdown")
		}
		if names := s.JobNames(); len(names) != 0 {
			t.Errorf("JobNames = %v, want none", names)
		}
	})
}

// TestShutdownDeadline verifies that when the deadline passes, Shutdown
// cancels runs in progress before returning the context's error without
// waiting, and that Stop then waits for those runs.
func TestShutdownDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)

		// This run ignores its context, so only Stop's wait ends it.
		release := make(chan struct{})
		var sawCancel bool
		mustSchedule(t, s, "job", time.Second,
			func(ctx context.Context, _ *scheduler.Job) error {
				<-release
				sawCancel = ctx.Err() != nil
				return nil
			})
		synctest.Wait()

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		start := time.Now()
		err := s.Shutdown(ctx)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Shutdown = %v, want %v", err, context.DeadlineExceeded)
		}
		if got := time.Since(start); got != time.Second {
			t.Errorf("Shutdown took %v, want 1s", got)
		}

		close(release)
		s.Stop() // waits for the run to return
		if !sawCancel {
			t.Error("run's context was not canceled after the deadline")
		}
	})
}

// TestShutdownDeadlineCancelsRun verifies a run that watches its context
// returns as soon as the Shutdown deadline passes.
func TestShutdownDeadlineCancelsRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		_, result := scheduleSlow(t, s, time.Hour)

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Shutdown = %v, want %v", err, context.DeadlineExceeded)
		}

		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Errorf("run saw ctx.Err() = %v, want %v", err, context.Canceled)
		}
		s.Stop()
	})
}

// TestShutdownWaitingJobsDontStart verifies that a job waiting for a slot
// when Shutdown begins never runs, even once the slot frees up.
func TestShutdownWaitingJobsDontStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		scheduleSlow(t, s, 5*time.Second)

		waiting := mustSchedule(t, s, "waiting", time.Second, keepRunning)
		synctest.Wait() // the second job is now blocked on the slot

		if err := s.Shutdown(t.Context()); err != nil {
			t.Errorf("Shutdown = %v, want nil", err)
		}
		if got := waiting.Runs(); got != 0 {
			t.Errorf("waiting job Runs = %d, want 0", got)
		}
	})
}

// TestShutdownIdle verifies Shutdown returns at once when no run is in
// progress, and that later calls to Shutdown, Stop and Schedule behave.
func TestShutdownIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		job := mustSchedule(t, s, "job", time.Minute, keepRunning)
		synctest.Wait() // first run done; waiting for the next one

		start := time.Now()
		if err := s.Shutdown(t.Context()); err != nil {
			t.Errorf("Shutdown = %v, want nil", err)
		}
		if got := time.Since(start); got != 0 {
			t.Errorf("Shutdown took %v, want 0", got)
		}
		if !job.Stopped() {
			t.Error("job not stopped after Shutdown")
		}

		if err := s.Shutdown(t.Context()); err != nil {
			t.Errorf("second Shutdown = %v, want nil", err)
		}
		s.Stop()

		_, err := s.Schedule("job", time.Second, keepRunning)
		if !errors.Is(err, scheduler.ErrSchedulerStopped) {
			t.Errorf("Schedule after Shutdown = %v, want %v",
				err, scheduler.ErrSchedulerStopped)
		}
	})
}
