package scheduler_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bnixon67/scheduler"
)

// sleepFor returns a RunFunc whose runs each take d.
func sleepFor(d time.Duration) scheduler.RunFunc {
	return func(context.Context, *scheduler.Job) error {
		time.Sleep(d)
		return nil
	}
}

// TestSkippedSlowJob verifies runs longer than the interval are counted.
func TestSkippedSlowJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		job := mustSchedule(t, s, "job", 10*time.Millisecond, sleepFor(50*time.Millisecond))
		time.Sleep(time.Second)

		if got := job.Skipped(); got < 3 {
			t.Errorf("Skipped() = %d, want at least 3", got)
		}
	})
}

// TestSkippedSaturated verifies runs delayed by waiting for a slot are
// counted, even though each run is shorter than its interval.
func TestSkippedSaturated(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 1)
		t.Cleanup(s.Stop)

		// Each run takes 30ms of a 40ms interval. With one slot, two such
		// jobs need 60ms per 40ms, so both fall behind.
		a := mustSchedule(t, s, "a", 40*time.Millisecond, sleepFor(30*time.Millisecond))
		b := mustSchedule(t, s, "b", 40*time.Millisecond, sleepFor(30*time.Millisecond))
		time.Sleep(time.Second)

		if a.Skipped() == 0 || b.Skipped() == 0 {
			t.Errorf("Skipped() = %d and %d, want both above 0",
				a.Skipped(), b.Skipped())
		}
	})
}

// TestSkippedOnSchedule verifies a job that keeps up is not counted.
func TestSkippedOnSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := mustNew(t, 4)
		t.Cleanup(s.Stop)

		job := mustSchedule(t, s, "job", 50*time.Millisecond, keepRunning)
		time.Sleep(300 * time.Millisecond)

		if got := job.Skipped(); got != 0 {
			t.Errorf("Skipped() = %d, want 0", got)
		}
	})
}

// TestSkippedLogged verifies a late run is logged at debug level.
func TestSkippedLogged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf,
			&slog.HandlerOptions{Level: slog.LevelDebug}))

		s := mustNew(t, 1, scheduler.WithLogger(logger))
		mustSchedule(t, s, "job", 10*time.Millisecond, sleepFor(30*time.Millisecond))
		time.Sleep(100 * time.Millisecond)
		s.Stop() // no more writes to buf after this

		if !strings.Contains(buf.String(), `msg="job ran late"`) {
			t.Errorf("log = %q, want a %q entry", buf.String(), "job ran late")
		}

		// The job is logged as structured fields via LogValue, not as
		// one formatted string.
		for _, field := range []string{"job.name=", "job.runs=", "job.skipped="} {
			if !strings.Contains(buf.String(), field) {
				t.Errorf("log = %q, want field %q", buf.String(), field)
			}
		}
	})
}

// TestDurationsLoggedAsText verifies durations are logged in readable form
// by the JSON handler, which otherwise writes them as nanoseconds.
func TestDurationsLoggedAsText(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf,
			&slog.HandlerOptions{Level: slog.LevelDebug}))

		s := mustNew(t, 1, scheduler.WithLogger(logger))
		mustSchedule(t, s, "job", 10*time.Millisecond, sleepFor(30*time.Millisecond),
			scheduler.WithRunTimeout(time.Second))
		time.Sleep(100 * time.Millisecond)
		s.Stop() // no more writes to buf after this

		for _, field := range []string{
			`"interval":"10ms"`, `"runTimeout":"1s"`, `"late":"20ms"`,
		} {
			if !strings.Contains(buf.String(), field) {
				t.Errorf("log = %q, want field %s", buf.String(), field)
			}
		}
	})
}
