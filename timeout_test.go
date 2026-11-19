package scheduler_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bnixon67/scheduler"
)

// waitForCtx is a RunFunc that blocks until its context is done and
// returns ctx.Err(), as a well-behaved long run does.
func waitForCtx(ctx context.Context, _ *scheduler.Job) error {
	<-ctx.Done()
	return ctx.Err()
}

// errorRecorder is an error handler that records the errors it is passed.
type errorRecorder struct {
	mu   sync.Mutex
	errs []error
}

func (r *errorRecorder) handle(_ *scheduler.Job, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, err)
}

func (r *errorRecorder) get() []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]error(nil), r.errs...)
}

// TestRunTimeout verifies a run that exceeds its timeout has its context
// canceled, is reported as context.DeadlineExceeded, and that the job
// keeps running afterward.
func TestRunTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		var rec errorRecorder
		start := time.Now()
		var took atomic.Int64
		job := mustSchedule(t, s, "job", time.Hour,
			func(ctx context.Context, job *scheduler.Job) error {
				err := waitForCtx(ctx, job)
				took.Store(int64(time.Since(start)))
				return err
			},
			scheduler.WithRunTimeout(time.Second),
			scheduler.WithErrorHandler(rec.handle))

		synctest.Sleep(2 * time.Second)

		if got := time.Duration(took.Load()); got != time.Second {
			t.Errorf("run returned after %v, want %v", got, time.Second)
		}
		errs := rec.get()
		if len(errs) != 1 || !errors.Is(errs[0], context.DeadlineExceeded) {
			t.Errorf("errors = %v, want one %v", errs, context.DeadlineExceeded)
		}
		if job.Stopped() {
			t.Error("job stopped after a timeout, want it to keep running")
		}

		// The next run gets a fresh timeout.
		synctest.Sleep(time.Hour)
		if got := len(rec.get()); got != 2 {
			t.Errorf("errors after second run = %d, want 2", got)
		}
	})
}

// TestRunTimeoutStopOnError verifies WithStopOnError stops a job whose
// run times out.
func TestRunTimeoutStopOnError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		job := mustSchedule(t, s, "job", time.Hour, waitForCtx,
			scheduler.WithRunTimeout(time.Second),
			scheduler.WithStopOnError(true),
			scheduler.WithErrorHandler(ignoreErrors))

		synctest.Sleep(2 * time.Second)

		if !job.Stopped() || job.Runs() != 1 {
			t.Errorf("Stopped(), Runs() = %t, %d; want true, 1",
				job.Stopped(), job.Runs())
		}
	})
}

// TestRunTimeoutNotReached verifies a run that finishes in time reports
// no error.
func TestRunTimeoutNotReached(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		var rec errorRecorder
		mustSchedule(t, s, "job", time.Hour,
			func(ctx context.Context, _ *scheduler.Job) error {
				select {
				case <-time.After(500 * time.Millisecond):
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
			scheduler.WithRunTimeout(time.Second),
			scheduler.WithErrorHandler(rec.handle))

		synctest.Sleep(2 * time.Second)

		if errs := rec.get(); len(errs) != 0 {
			t.Errorf("errors = %v, want none", errs)
		}
	})
}

// TestRunTimeoutJobStopped verifies stopping a job cancels a timed run
// early, and that the cancellation is not reported as an error.
func TestRunTimeoutJobStopped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		var rec errorRecorder
		result := make(chan error, 1)
		job := mustSchedule(t, s, "job", time.Hour,
			func(ctx context.Context, job *scheduler.Job) error {
				err := waitForCtx(ctx, job)
				result <- err
				return err
			},
			scheduler.WithRunTimeout(time.Minute),
			scheduler.WithErrorHandler(rec.handle))

		synctest.Sleep(time.Second)
		job.Stop()

		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Errorf("run saw ctx.Err() = %v, want %v", err, context.Canceled)
		}
		s.Stop() // wait for the run to return
		if errs := rec.get(); len(errs) != 0 {
			t.Errorf("errors = %v, want none", errs)
		}
	})
}

// TestRunTimeoutDisabled verifies a zero or negative timeout means runs
// have no time limit.
func TestRunTimeoutDisabled(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		synctest.Test(t, func(t *testing.T) {
			s := mustNew(t, 1)

			var deadline atomic.Bool
			mustSchedule(t, s, "job", time.Hour,
				func(ctx context.Context, _ *scheduler.Job) error {
					_, ok := ctx.Deadline()
					deadline.Store(ok)
					return nil
				},
				scheduler.WithRunTimeout(d))

			synctest.Wait()
			s.Stop()

			if deadline.Load() {
				t.Errorf("WithRunTimeout(%v): run ctx has a deadline, want none", d)
			}
		})
	}
}
