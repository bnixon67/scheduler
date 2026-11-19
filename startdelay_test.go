package scheduler_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bnixon67/scheduler"
)

// runTimes records when each run of a job starts, relative to start.
type runTimes struct {
	start time.Time
	mu    sync.Mutex
	times []time.Duration
}

func (r *runTimes) run(context.Context, *scheduler.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.times = append(r.times, time.Since(r.start))
	return nil
}

func (r *runTimes) get() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.times)
}

// TestStartDelay verifies the first run waits for the delay and later
// runs follow every interval after the first run, not after scheduling.
func TestStartDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		r := &runTimes{start: time.Now()}
		job := mustSchedule(t, s, "job", time.Minute, r.run,
			scheduler.WithStartDelay(30*time.Second))

		synctest.Wait()
		if got := job.Runs(); got != 0 {
			t.Errorf("runs before the delay = %d, want 0", got)
		}
		if !job.LastRun().IsZero() {
			t.Errorf("LastRun before the delay = %v, want zero", job.LastRun())
		}

		synctest.Sleep(2*time.Minute + 45*time.Second)

		want := []time.Duration{
			30 * time.Second,
			90 * time.Second,
			150 * time.Second,
		}
		if got := r.get(); !slices.Equal(got, want) {
			t.Errorf("runs started at %v, want %v", got, want)
		}
		if got := job.Skipped(); got != 0 {
			t.Errorf("Skipped() = %d, want 0", got)
		}
	})
}

// TestStartDelayZeroOrNegative verifies a delay of 0 or less runs the job
// as soon as it is scheduled.
func TestStartDelayZeroOrNegative(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		t.Run(d.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := mustNew(t, 1)
				t.Cleanup(s.Stop)

				r := &runTimes{start: time.Now()}
				job := mustSchedule(t, s, "job", time.Minute, r.run,
					scheduler.WithStartDelay(d))
				synctest.Wait()

				if got := r.get(); !slices.Equal(got, []time.Duration{0}) {
					t.Errorf("runs started at %v, want [0s]", got)
				}
				if got := job.String(); !strings.Contains(got, "startDelay: 0s,") {
					t.Errorf("String() = %s, want it to contain startDelay: 0s", got)
				}
			})
		})
	}
}

// TestStartDelayStop verifies a job stopped during its delay never runs
// and is removed from the scheduler at once.
func TestStartDelayStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		r := &runTimes{start: time.Now()}
		job := mustSchedule(t, s, "job", time.Minute, r.run,
			scheduler.WithStartDelay(time.Hour))

		synctest.Sleep(time.Minute)
		job.Stop()
		synctest.Wait()

		if !job.Stopped() {
			t.Error("Stopped() = false after Stop, want true")
		}
		if names := s.JobNames(); len(names) != 0 {
			t.Errorf("JobNames() = %v after Stop, want none", names)
		}

		synctest.Sleep(2 * time.Hour)
		if got := r.get(); len(got) != 0 {
			t.Errorf("runs started at %v after Stop, want none", got)
		}
	})
}

// TestStartDelayShutdown verifies Shutdown returns at once for jobs that
// are still waiting for their first run, which then never happens.
func TestStartDelayShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)

		r := &runTimes{start: time.Now()}
		job := mustSchedule(t, s, "job", time.Minute, r.run,
			scheduler.WithStartDelay(time.Hour))

		start := time.Now()
		if err := s.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown() = %v, want nil", err)
		}
		if took := time.Since(start); took != 0 {
			t.Errorf("Shutdown took %v, want it to return at once", took)
		}

		synctest.Sleep(2 * time.Hour)
		if got := r.get(); len(got) != 0 {
			t.Errorf("runs started at %v after Shutdown, want none", got)
		}
		if !job.Stopped() {
			t.Error("Stopped() = false after Shutdown, want true")
		}
	})
}

// TestStartDelayHoldsNoSlot verifies a job waiting for its first run does
// not take a concurrency slot from other jobs.
func TestStartDelayHoldsNoSlot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		mustSchedule(t, s, "delayed", time.Hour, keepRunning,
			scheduler.WithStartDelay(time.Hour))
		r := &runTimes{start: time.Now()}
		mustSchedule(t, s, "now", time.Minute, r.run)

		synctest.Sleep(90 * time.Second)

		want := []time.Duration{0, time.Minute}
		if got := r.get(); !slices.Equal(got, want) {
			t.Errorf("other job's runs started at %v, want %v", got, want)
		}
	})
}

// TestStartDelaySpreadsJobs verifies jobs with the same interval but
// different delays keep their offsets on every run.
func TestStartDelaySpreadsJobs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 3)
		t.Cleanup(s.Stop)

		start := time.Now()
		recs := make([]*runTimes, 3)
		for i := range recs {
			recs[i] = &runTimes{start: start}
			mustSchedule(t, s, string(rune('a'+i)), 30*time.Second,
				recs[i].run,
				scheduler.WithStartDelay(time.Duration(i)*10*time.Second))
		}

		synctest.Sleep(85 * time.Second)

		for i, r := range recs {
			off := time.Duration(i) * 10 * time.Second
			want := []time.Duration{off, off + 30*time.Second, off + 60*time.Second}
			if got := r.get(); !slices.Equal(got, want) {
				t.Errorf("job %d runs started at %v, want %v", i, got, want)
			}
		}
	})
}
