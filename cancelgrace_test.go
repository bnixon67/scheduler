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

// warnLogger returns a logger that writes warnings and above to buf.
func warnLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf,
		&slog.HandlerOptions{Level: slog.LevelWarn}))
}

// ignoreCtx returns a RunFunc that sleeps for d without watching its
// context, breaking the RunFunc contract on purpose.
func ignoreCtx(d time.Duration) scheduler.RunFunc {
	return func(context.Context, *scheduler.Job) error {
		time.Sleep(d)
		return nil
	}
}

// TestCancelGraceWarns verifies a run that ignores cancellation is logged
// once the grace period passes, and again when it finally returns.
func TestCancelGraceWarns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var buf bytes.Buffer
		s := mustNew(t, 1, scheduler.WithLogger(warnLogger(&buf)))

		job := mustSchedule(t, s, "stubborn", time.Hour, ignoreCtx(20*time.Second))
		synctest.Sleep(time.Second)
		job.Stop()
		s.Stop() // waits the remaining 19s; no more writes to buf after this

		log := buf.String()
		for _, want := range []string{
			`msg="run ignored cancellation" job.name=stubborn`,
			`reason="context canceled" after=5s`,
			`msg="run returned late after cancellation" job.name=stubborn`,
			`after=19s`,
		} {
			if !strings.Contains(log, want) {
				t.Errorf("log = %q, want it to contain %q", log, want)
			}
		}
	})
}

// TestCancelGraceTimeout verifies the warning also covers runs that ignore
// a WithRunTimeout deadline, and says so.
func TestCancelGraceTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var buf bytes.Buffer
		s := mustNew(t, 1, scheduler.WithLogger(warnLogger(&buf)))

		mustSchedule(t, s, "slow", time.Hour, ignoreCtx(10*time.Second),
			scheduler.WithRunTimeout(time.Second),
			scheduler.WithCancelGrace(2*time.Second))
		synctest.Sleep(15 * time.Second)
		s.Stop()

		if want := `reason="context deadline exceeded" after=2s`; !strings.Contains(buf.String(), want) {
			t.Errorf("log = %q, want it to contain %q", buf.String(), want)
		}
	})
}

// TestCancelGraceNoWarning verifies runs that follow the contract, or are
// never canceled, log nothing, and that a zero grace disables the warning.
func TestCancelGraceNoWarning(t *testing.T) {
	tests := map[string]struct {
		run  scheduler.RunFunc
		opts []scheduler.JobOption
	}{
		"watches ctx": {run: waitForCtx},
		"returns at its timeout": {
			run: waitForCtx,
			opts: []scheduler.JobOption{scheduler.WithRunTimeout(time.Second),
				scheduler.WithErrorHandler(ignoreErrors)},
		},
		"finishes before canceled": {run: ignoreCtx(time.Second)},
		"grace disabled": {
			run:  ignoreCtx(20 * time.Second),
			opts: []scheduler.JobOption{scheduler.WithCancelGrace(0)},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var buf bytes.Buffer
				s := mustNew(t, 1, scheduler.WithLogger(warnLogger(&buf)))

				job := mustSchedule(t, s, "job", time.Hour, tc.run, tc.opts...)
				time.Sleep(3 * time.Second)
				job.Stop()
				s.Stop()

				if buf.Len() != 0 {
					t.Errorf("log = %q, want nothing", buf.String())
				}
			})
		})
	}
}
