package scheduler_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bnixon67/scheduler"
)

// TestStatusBeforeFirstRun verifies the zero status of a job that has not
// run yet, because it is waiting for a slot.
func TestStatusBeforeFirstRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		mustSchedule(t, s, "busy", time.Hour, ignoreCtx(time.Minute))
		synctest.Wait() // busy holds the only slot
		job := mustSchedule(t, s, "waiting", time.Hour, keepRunning)
		synctest.Wait()

		if job.Running() || !job.LastRun().IsZero() || job.LastError() != nil {
			t.Errorf("Running, LastRun, LastError = %t, %v, %v; want false, zero, nil",
				job.Running(), job.LastRun(), job.LastError())
		}
	})
}

// TestStatusRunning verifies Running and LastRun during and after a run.
func TestStatusRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		release := make(chan struct{})
		var sawRunning atomic.Bool // written by every run
		start := time.Now()
		job := mustSchedule(t, s, "job", time.Hour,
			func(_ context.Context, job *scheduler.Job) error {
				sawRunning.Store(job.Running())
				<-release
				return nil
			})

		time.Sleep(time.Second)
		synctest.Wait()
		if !job.Running() {
			t.Error("Running() = false during a run, want true")
		}
		if got := job.LastRun(); !got.Equal(start) {
			t.Errorf("LastRun() = %v during the first run, want %v", got, start)
		}

		close(release)
		synctest.Wait()
		if job.Running() {
			t.Error("Running() = true after the run, want false")
		}
		if !sawRunning.Load() {
			t.Error("Running() = false inside the RunFunc, want true")
		}

		time.Sleep(time.Hour) // the second run starts and returns at once
		synctest.Wait()
		if got, want := job.LastRun(), start.Add(time.Hour); !got.Equal(want) {
			t.Errorf("LastRun() = %v after the second run, want %v", got, want)
		}
	})
}

// TestStatusLastError verifies LastError reports each finished run's
// error, is cleared by a successful run, and is up to date when the error
// handler is called.
func TestStatusLastError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		errBoom := errors.New("boom")
		results := []error{errBoom, nil}
		var inHandler error
		job := mustSchedule(t, s, "job", time.Hour,
			func(_ context.Context, job *scheduler.Job) error {
				return results[job.Runs()-1]
			},
			scheduler.WithMaxRuns(len(results)),
			scheduler.WithErrorHandler(func(job *scheduler.Job, _ error) {
				inHandler = job.LastError()
			}))

		synctest.Wait()
		if err := job.LastError(); !errors.Is(err, errBoom) {
			t.Errorf("LastError() after a failed run = %v, want %v", err, errBoom)
		}
		if !errors.Is(inHandler, errBoom) {
			t.Errorf("LastError() in the error handler = %v, want %v", inHandler, errBoom)
		}

		time.Sleep(time.Hour)
		synctest.Wait()
		if err := job.LastError(); err != nil {
			t.Errorf("LastError() after a successful run = %v, want nil", err)
		}
	})
}

// TestStatusLastErrorNotFailures verifies LastError is nil for results
// that are not failures, and a *PanicError for a panic.
func TestStatusLastErrorNotFailures(t *testing.T) {
	t.Run("ErrStop", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s := mustNew(t, 1)
			t.Cleanup(s.Stop)

			job := mustSchedule(t, s, "job", time.Hour,
				func(context.Context, *scheduler.Job) error {
					return scheduler.ErrStop
				})
			synctest.Wait()

			if err := job.LastError(); err != nil {
				t.Errorf("LastError() = %v, want nil", err)
			}
		})
	})

	t.Run("canceled after stop", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s := mustNew(t, 1)

			job := mustSchedule(t, s, "job", time.Hour, waitForCtx)
			synctest.Wait()
			job.Stop()
			s.Stop() // waits for the run to return

			if err := job.LastError(); err != nil {
				t.Errorf("LastError() = %v, want nil", err)
			}
		})
	})

	t.Run("panic", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s := mustNew(t, 1)
			t.Cleanup(s.Stop)

			job := mustSchedule(t, s, "job", time.Hour,
				func(context.Context, *scheduler.Job) error {
					panic("boom")
				},
				scheduler.WithErrorHandler(ignoreErrors))
			synctest.Wait()

			var pe *scheduler.PanicError
			if err := job.LastError(); !errors.As(err, &pe) || pe.Value != "boom" {
				t.Errorf("LastError() = %v, want a *PanicError with value boom", err)
			}
		})
	})
}
