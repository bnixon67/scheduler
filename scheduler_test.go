package scheduler_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bnixon67/scheduler"
)

// TestSchedulerJobNames verifies JobNames lists scheduled jobs in order.
func TestSchedulerJobNames(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		mustSchedule(t, s, "b", time.Second, keepRunning)
		mustSchedule(t, s, "a", time.Second, keepRunning)

		if got, want := s.JobNames(), []string{"a", "b"}; !slices.Equal(got, want) {
			t.Errorf("JobNames() = %v, want %v", got, want)
		}
	})
}

// TestSchedulerJob verifies looking up a scheduled job by name.
func TestSchedulerJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		want := mustSchedule(t, s, "job", time.Second, keepRunning)
		mustSchedule(t, s, "other", time.Second, keepRunning)

		if got, ok := s.Job("job"); !ok || got != want {
			t.Errorf("Job(%q) = %v, %t; want %v, true", "job", got, ok, want)
		}
		if got, ok := s.Job("missing"); ok || got != nil {
			t.Errorf("Job(%q) = %v, %t; want nil, false", "missing", got, ok)
		}
	})
}

// TestSchedulerRunsOnSchedule verifies a job runs immediately and then
// once per interval.
func TestSchedulerRunsOnSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		var runs atomic.Int32
		mustSchedule(t, s, "job", 500*time.Millisecond,
			func(context.Context, *scheduler.Job) error {
				runs.Add(1)
				return nil
			})

		synctest.Sleep(2250 * time.Millisecond) // runs at 0, 0.5, 1, 1.5 and 2s

		if got := runs.Load(); got != 5 {
			t.Errorf("runs = %d, want 5", got)
		}
	})
}

// TestSchedulerMultipleJobs verifies several jobs each run on their own
// interval.
func TestSchedulerMultipleJobs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 3)
		t.Cleanup(s.Stop)

		var mu sync.Mutex
		runs := map[string]int{}
		count := func(_ context.Context, job *scheduler.Job) error {
			mu.Lock()
			runs[job.Name()]++
			mu.Unlock()
			return nil
		}

		mustSchedule(t, s, "1s", time.Second, count)
		mustSchedule(t, s, "2s", 2*time.Second, count)
		mustSchedule(t, s, "3s", 3*time.Second, count)

		synctest.Sleep(5500 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()
		want := map[string]int{"1s": 6, "2s": 3, "3s": 2}
		for name, n := range want {
			if runs[name] != n {
				t.Errorf("job %q ran %d times, want %d", name, runs[name], n)
			}
		}
	})
}

// TestSchedulerWithMaxRuns verifies a job stops after its limit.
func TestSchedulerWithMaxRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		job := mustSchedule(t, s, "job", 50*time.Millisecond, keepRunning,
			scheduler.WithMaxRuns(3))

		synctest.Sleep(time.Second)

		if !job.Stopped() || job.Runs() != 3 {
			t.Errorf("Stopped(), Runs() = %t, %d; want true, 3",
				job.Stopped(), job.Runs())
		}
	})
}

// TestSchedulerScheduleDuplicateName verifies that scheduling a job with a
// name already in use returns an error.
func TestSchedulerScheduleDuplicateName(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 5)
		t.Cleanup(s.Stop)

		tests := []struct {
			name    string
			wantErr error
		}{
			{"a", nil},
			{"b", nil},
			{"a", scheduler.ErrJobNameExists},
			{"a", scheduler.ErrJobNameExists},
		}

		// One scheduler is shared, so the cases run in order without t.Run.
		for n, tc := range tests {
			job, err := s.Schedule(tc.name, time.Second, keepRunning)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("case %d: Schedule(%q) = %v, want %v",
					n, tc.name, err, tc.wantErr)
			}
			if (job == nil) != (tc.wantErr != nil) {
				t.Errorf("case %d: Schedule(%q) job = %v", n, tc.name, job)
			}
		}
	})
}

// TestSchedulerStopEndsRuns verifies no run starts after Stop returns.
func TestSchedulerStopEndsRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 2)

		var runs atomic.Int32
		mustSchedule(t, s, "job", 100*time.Millisecond,
			func(context.Context, *scheduler.Job) error {
				runs.Add(1)
				time.Sleep(50 * time.Millisecond)
				return nil
			})

		synctest.Sleep(225 * time.Millisecond) // runs at 0, 100 and 200ms
		s.Stop()
		final := runs.Load()

		synctest.Sleep(time.Second)
		if final != 3 || runs.Load() != final {
			t.Errorf("runs = %d at Stop and %d later, want 3 both times",
				final, runs.Load())
		}
	})
}

// TestHighFrequencyJob verifies a job with a very short interval keeps up.
func TestHighFrequencyJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 5)
		t.Cleanup(s.Stop)

		var runs atomic.Int32
		job := mustSchedule(t, s, "job", time.Millisecond,
			func(context.Context, *scheduler.Job) error {
				runs.Add(1)
				return nil
			})

		synctest.Sleep(100*time.Millisecond + time.Microsecond)

		if got := runs.Load(); got != 101 {
			t.Errorf("runs = %d, want 101", got)
		}
		if got := job.Skipped(); got != 0 {
			t.Errorf("Skipped() = %d, want 0", got)
		}
	})
}
