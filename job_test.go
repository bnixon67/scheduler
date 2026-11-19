package scheduler_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bnixon67/scheduler"
)

// TestScheduleInvalid verifies Schedule rejects invalid arguments.
func TestScheduleInvalid(t *testing.T) {
	tests := []struct {
		name     string
		jobName  string
		interval time.Duration
		run      scheduler.RunFunc
		wantErr  error
	}{
		{"empty name", "", time.Second, keepRunning, scheduler.ErrEmptyName},
		{"zero interval", "job", 0, keepRunning, scheduler.ErrInvalidInterval},
		{"negative interval", "job", -time.Second, keepRunning, scheduler.ErrInvalidInterval},
		{"nil run", "job", time.Second, nil, scheduler.ErrNilRun},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := mustNew(t, 1)
			defer s.Stop()

			job, err := s.Schedule(tc.jobName, tc.interval, tc.run)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Schedule() error = %v, want %v", err, tc.wantErr)
			}
			if job != nil {
				t.Errorf("Schedule() job = %v, want nil", job)
			}
			if names := s.JobNames(); len(names) != 0 {
				t.Errorf("JobNames() = %v, want none", names)
			}
		})
	}
}

// TestJobString verifies the String method, including runtime state.
func TestJobString(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		job := mustSchedule(t, s, "test", time.Hour, keepRunning)
		synctest.Wait() // the first run has finished
		job.Stop()

		got := job.String()
		want := `Job{name: "test", interval: 1h0m0s, stopOnError: false, maxRuns: 0, runTimeout: 0s, runs: 1, skipped: 0, stopped: true}`
		if got != want {
			t.Errorf("\ngot  %v,\nwant %v\nfor job.String()", got, want)
		}

		timed := mustSchedule(t, s, "timed", time.Hour, keepRunning,
			scheduler.WithRunTimeout(90*time.Second))
		if got := timed.String(); !strings.Contains(got, "runTimeout: 1m30s,") {
			t.Errorf("String() = %s, want it to contain runTimeout: 1m30s", got)
		}
	})
}

// TestJobNameInterval verifies Name and Interval return what was
// scheduled.
func TestJobNameInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		job := mustSchedule(t, s, "test", 42*time.Second, keepRunning)

		if got := job.Name(); got != "test" {
			t.Errorf("Name() = %q, want %q", got, "test")
		}
		if got := job.Interval(); got != 42*time.Second {
			t.Errorf("Interval() = %v, want %v", got, 42*time.Second)
		}
	})
}

// TestJobStop verifies Stop prevents further runs and frees the name at
// once, even while a run is still in progress.
func TestJobStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 2)
		t.Cleanup(s.Stop)

		release := make(chan struct{})
		var runs atomic.Int32
		job := mustSchedule(t, s, "job", time.Second,
			func(context.Context, *scheduler.Job) error {
				runs.Add(1)
				<-release
				return nil
			})
		synctest.Wait() // the first run is blocked

		job.Stop()

		if _, ok := s.Job("job"); ok {
			t.Error("Job() still finds the job after Stop")
		}
		if _, err := s.Schedule("job", time.Hour, keepRunning); err != nil {
			t.Errorf("rescheduling the name during the old run = %v, want nil", err)
		}

		close(release)
		time.Sleep(5 * time.Second)
		if got := runs.Load(); got != 1 {
			t.Errorf("runs = %d, want 1", got)
		}
	})
}

// TestJobStopInRun verifies a job can stop itself from its RunFunc.
func TestJobStopInRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		job := mustSchedule(t, s, "job", time.Second,
			func(_ context.Context, job *scheduler.Job) error {
				job.Stop()
				return nil
			})
		time.Sleep(5 * time.Second)

		if got := job.Runs(); got != 1 {
			t.Errorf("Runs() = %d, want 1", got)
		}
	})
}

// TestRunErrStop verifies returning ErrStop stops the job without calling
// the error handler.
func TestRunErrStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		var called atomic.Bool
		job := mustSchedule(t, s, "job", time.Second,
			func(context.Context, *scheduler.Job) error { return scheduler.ErrStop },
			scheduler.WithErrorHandler(func(*scheduler.Job, error) { called.Store(true) }))
		time.Sleep(5 * time.Second)

		if !job.Stopped() || job.Runs() != 1 {
			t.Errorf("Stopped(), Runs() = %t, %d; want true, 1",
				job.Stopped(), job.Runs())
		}
		if called.Load() {
			t.Error("error handler called for ErrStop")
		}
	})
}

// TestRunError verifies a failing run is reported, and that the job keeps
// running by default or stops WithStopOnError(true).
func TestRunError(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "keep running", true: "stop on error"}[stop],
			func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					s := mustNew(t, 1)
					t.Cleanup(s.Stop)

					failure := errors.New("failure")
					var reported atomic.Int32
					job := mustSchedule(t, s, "job", time.Second,
						func(context.Context, *scheduler.Job) error { return failure },
						scheduler.WithStopOnError(stop),
						scheduler.WithErrorHandler(func(_ *scheduler.Job, err error) {
							if errors.Is(err, failure) {
								reported.Add(1)
							}
						}))
					time.Sleep(2500 * time.Millisecond) // runs at 0s, 1s, 2s

					want := 3
					if stop {
						want = 1
					}
					if job.Runs() != want || int(reported.Load()) != want {
						t.Errorf("Runs(), reported = %d, %d; want %d each",
							job.Runs(), reported.Load(), want)
					}
				})
			})
	}
}
