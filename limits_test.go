package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bnixon67/scheduler"
)

// TestWithMinInterval verifies Schedule rejects intervals below the
// minimum and accepts the minimum itself.
func TestWithMinInterval(t *testing.T) {
	tests := []struct {
		name     string
		min      time.Duration
		interval time.Duration
		want     error
	}{
		{"below minimum", time.Second, 999 * time.Millisecond, scheduler.ErrIntervalTooShort},
		{"at minimum", time.Second, time.Second, nil},
		{"above minimum", time.Second, time.Minute, nil},
		{"zero minimum", 0, time.Nanosecond, nil},
		{"negative minimum", -time.Second, time.Nanosecond, nil},
		{"invalid interval wins", time.Second, 0, scheduler.ErrInvalidInterval},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := mustNew(t, 1, scheduler.WithMinInterval(tc.min))
				t.Cleanup(s.Stop)

				_, err := s.Schedule("job", tc.interval, keepRunning)
				if !errors.Is(err, tc.want) {
					t.Errorf("Schedule(%v) = %v, want %v", tc.interval, err, tc.want)
				}
				if _, ok := s.Job("job"); ok != (tc.want == nil) {
					t.Errorf("Job registered = %t, want %t", ok, tc.want == nil)
				}
			})
		})
	}
}

// TestWithMaxJobs verifies Schedule rejects jobs beyond the limit and
// accepts new ones once a job stops.
func TestWithMaxJobs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1, scheduler.WithMaxJobs(2))
		t.Cleanup(s.Stop)

		a := mustSchedule(t, s, "a", time.Second, keepRunning)
		mustSchedule(t, s, "b", time.Second, keepRunning)

		if _, err := s.Schedule("c", time.Second, keepRunning); !errors.Is(err, scheduler.ErrTooManyJobs) {
			t.Fatalf("Schedule over limit = %v, want %v", err, scheduler.ErrTooManyJobs)
		}
		if _, ok := s.Job("c"); ok {
			t.Error("rejected job is registered")
		}

		// A duplicate name is reported as such even at the limit.
		if _, err := s.Schedule("a", time.Second, keepRunning); !errors.Is(err, scheduler.ErrJobNameExists) {
			t.Errorf("Schedule duplicate = %v, want %v", err, scheduler.ErrJobNameExists)
		}

		a.Stop()
		mustSchedule(t, s, "c", time.Second, keepRunning)
	})
}

// TestWithMaxJobsFinishedJob verifies a job that finishes on its own
// frees its place.
func TestWithMaxJobsFinishedJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1, scheduler.WithMaxJobs(1))
		t.Cleanup(s.Stop)

		job := mustSchedule(t, s, "once", time.Second,
			func(context.Context, *scheduler.Job) error { return scheduler.ErrStop })
		<-job.Context().Done()
		synctest.Wait()

		mustSchedule(t, s, "next", time.Second, keepRunning)
	})
}

// TestWithMaxJobsUnlimited verifies 0 and negative values mean no limit.
func TestWithMaxJobsUnlimited(t *testing.T) {
	for _, n := range []int{0, -1} {
		synctest.Test(t, func(t *testing.T) {
			s := mustNew(t, 1, scheduler.WithMaxJobs(n))
			t.Cleanup(s.Stop)

			for _, name := range []string{"a", "b", "c"} {
				mustSchedule(t, s, name, time.Second, keepRunning)
			}
		})
	}
}
