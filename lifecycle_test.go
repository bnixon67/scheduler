package scheduler_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bnixon67/scheduler"
)

// TestSchedulerNoOverlap verifies a job never runs concurrently with itself,
// even when its runs take longer than its interval and slots are free.
func TestSchedulerNoOverlap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 4)
		t.Cleanup(s.Stop)

		var cur, max, runs atomic.Int32
		mustSchedule(t, s, "job", 10*time.Millisecond,
			func(context.Context, *scheduler.Job) error {
				trackMax(&cur, &max)
				runs.Add(1)
				time.Sleep(35 * time.Millisecond)
				cur.Add(-1)
				return nil
			})

		synctest.Sleep(300 * time.Millisecond)

		if got := max.Load(); got != 1 {
			t.Errorf("max concurrent runs of one job = %d, want 1", got)
		}
		if got := runs.Load(); got < 3 {
			t.Errorf("runs = %d, want at least 3", got)
		}
	})
}

// TestSchedulerSlowJobKeepsRunning verifies that a job slower than its
// interval keeps running instead of silently dying.
func TestSchedulerSlowJobKeepsRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		var runs atomic.Int32
		job := mustSchedule(t, s, "job", 10*time.Millisecond,
			func(context.Context, *scheduler.Job) error {
				runs.Add(1)
				time.Sleep(50 * time.Millisecond)
				return nil
			})

		synctest.Sleep(300 * time.Millisecond)
		first := runs.Load()
		synctest.Sleep(300 * time.Millisecond)
		second := runs.Load()

		if second <= first {
			t.Errorf("runs stalled at %d, want job to keep running", second)
		}
		if job.Stopped() {
			t.Error("Stopped() = true, want false")
		}
		if _, ok := s.Job("job"); !ok {
			t.Error("Job() does not find the running job")
		}
	})
}

// TestSchedulerMaxConcurrent verifies at most maxConcurrent jobs run at once
// and that jobs waiting for a slot still run.
func TestSchedulerMaxConcurrent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const slots = 2
		s := mustNew(t, slots)
		t.Cleanup(s.Stop)

		var cur, max atomic.Int32
		ran := make([]atomic.Bool, 5)
		for i := range ran {
			mustSchedule(t, s, fmt.Sprint("job", i), 10*time.Millisecond,
				func(context.Context, *scheduler.Job) error {
					trackMax(&cur, &max)
					ran[i].Store(true)
					time.Sleep(20 * time.Millisecond)
					cur.Add(-1)
					return nil
				})
		}

		synctest.Sleep(time.Second)

		for i := range ran {
			if !ran[i].Load() {
				t.Errorf("job%d never ran", i)
			}
		}
		if got := max.Load(); got > slots {
			t.Errorf("max concurrent runs = %d, want at most %d", got, slots)
		}
	})
}

// TestSchedulerFinishedJobRemoved verifies that jobs which end on their own
// are removed, so JobNames is accurate and the name can be reused.
func TestSchedulerFinishedJobRemoved(t *testing.T) {
	tests := map[string]struct {
		run  scheduler.RunFunc
		opts []scheduler.JobOption
	}{
		"run returns ErrStop": {
			run: func(context.Context, *scheduler.Job) error { return scheduler.ErrStop },
		},
		"max runs": {
			run:  keepRunning,
			opts: []scheduler.JobOption{scheduler.WithMaxRuns(2)},
		},
		"stop on error": {
			run: func(context.Context, *scheduler.Job) error { panic("boom") },
			opts: []scheduler.JobOption{
				scheduler.WithStopOnError(true),
				scheduler.WithErrorHandler(ignoreErrors),
			},
		},
		"job.Stop in run": {
			run: func(_ context.Context, j *scheduler.Job) error { j.Stop(); return nil },
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := mustNew(t, 1)
				t.Cleanup(s.Stop)

				job := mustSchedule(t, s, "job", time.Millisecond, tc.run, tc.opts...)
				time.Sleep(100 * time.Millisecond)

				if _, ok := s.Job("job"); ok {
					t.Fatal("Job() still finds the finished job")
				}
				if !job.Stopped() {
					t.Error("Stopped() = false, want true")
				}

				again, err := s.Schedule("job", time.Second, keepRunning)
				if err != nil {
					t.Fatalf("rescheduling the name = %v, want nil", err)
				}
				if again == job {
					t.Error("rescheduling returned the finished job")
				}
			})
		})
	}
}

// TestSchedulerStopThenReuseName verifies that a stopped job's goroutine,
// still in its last run, does not remove a new job that reused its name.
func TestSchedulerStopThenReuseName(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		release := make(chan struct{})
		old := mustSchedule(t, s, "same", time.Millisecond,
			func(context.Context, *scheduler.Job) error { <-release; return nil })
		synctest.Wait() // the run is blocked

		old.Stop()
		want := mustSchedule(t, s, "same", time.Hour, keepRunning)

		close(release) // the old run returns and its goroutine exits
		synctest.Wait()

		if got, _ := s.Job("same"); got != want {
			t.Errorf("Job(%q) = %v, want the replacement job", "same", got)
		}
	})
}

// TestSchedulerScheduleAfterStop verifies Schedule fails once stopped.
func TestSchedulerScheduleAfterStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		s.Stop()
		s.Stop() // safe to call twice

		job, err := s.Schedule("job", time.Second, keepRunning)
		if job != nil {
			t.Errorf("Schedule() returned %v, want nil", job)
		}
		if !errors.Is(err, scheduler.ErrSchedulerStopped) {
			t.Errorf("Schedule() = %v, want %v", err, scheduler.ErrSchedulerStopped)
		}
	})
}

// TestSchedulerStopRemovesJobs verifies Stop stops and removes every job.
func TestSchedulerStopRemovesJobs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 2)
		job := mustSchedule(t, s, "job", time.Hour, keepRunning)

		s.Stop()

		if !job.Stopped() {
			t.Error("Stopped() = false after Stop, want true")
		}
		if names := s.JobNames(); len(names) != 0 {
			t.Errorf("JobNames() = %v after Stop, want none", names)
		}
	})
}

// TestSchedulerSameNameSeparateSchedulers verifies the same name can be
// scheduled in two Schedulers, each job keeping its own count, and
// scheduled again after it stops, starting with a fresh count.
func TestSchedulerSameNameSeparateSchedulers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int32
		run := func(context.Context, *scheduler.Job) error { runs.Add(1); return nil }
		limit := scheduler.WithMaxRuns(2)

		s1 := mustNew(t, 1)
		t.Cleanup(s1.Stop)
		s2 := mustNew(t, 1)
		t.Cleanup(s2.Stop)

		a := mustSchedule(t, s1, "shared", time.Millisecond, run, limit)
		b := mustSchedule(t, s2, "shared", time.Millisecond, run, limit)
		synctest.Sleep(100 * time.Millisecond)

		if !a.Stopped() || !b.Stopped() || a.Runs() != 2 || b.Runs() != 2 {
			t.Errorf("a, b = %v, %v; want both stopped after 2 runs", a, b)
		}

		c := mustSchedule(t, s1, "shared", time.Millisecond, run, limit)
		synctest.Sleep(100 * time.Millisecond)

		if !c.Stopped() || c.Runs() != 2 {
			t.Errorf("rescheduled job = %v, want stopped after 2 runs", c)
		}
		if got := runs.Load(); got != 6 {
			t.Errorf("total runs = %d, want 6", got)
		}
	})
}

// TestSchedulerStopCancelsJobContext verifies a run watching its context
// returns promptly when the scheduler stops.
func TestSchedulerStopCancelsJobContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)

		mustSchedule(t, s, "job", time.Second,
			func(ctx context.Context, _ *scheduler.Job) error {
				<-ctx.Done()
				return ctx.Err()
			})
		synctest.Wait() // the run is waiting on ctx

		start := time.Now()
		s.Stop()
		if got := time.Since(start); got != 0 {
			t.Errorf("Stop took %v, want it to return at once", got)
		}
	})
}

// TestWithLoggerNil verifies a nil logger falls back to the default instead
// of crashing when the scheduler logs.
func TestWithLoggerNil(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1, scheduler.WithLogger(nil))

		// The default error handler and Stop both use the logger.
		job := mustSchedule(t, s, "job", time.Millisecond,
			func(context.Context, *scheduler.Job) error { return errors.New("boom") },
			scheduler.WithStopOnError(true))
		synctest.Sleep(100 * time.Millisecond)

		if !job.Stopped() {
			t.Error("job did not stop after its error")
		}
		s.Stop()
	})
}

// TestNewInvalid verifies New rejects a bad limit.
func TestNewInvalid(t *testing.T) {
	for _, n := range []int{0, -1} {
		s, err := scheduler.New(n)
		if !errors.Is(err, scheduler.ErrInvalidMaxConcurrent) {
			t.Errorf("New(%d) error = %v, want %v",
				n, err, scheduler.ErrInvalidMaxConcurrent)
		}
		if s != nil {
			t.Errorf("New(%d) returned a scheduler, want nil", n)
		}
	}
}
