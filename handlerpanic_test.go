package scheduler_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bnixon67/scheduler"
)

// TestErrorHandlerPanic verifies a panic in the error handler is
// recovered and logged without the run's error, and that the job then
// continues as if the handler had returned: it keeps running by default,
// and stops with WithStopOnError(true).
func TestErrorHandlerPanic(t *testing.T) {
	for _, stopOnError := range []bool{false, true} {
		name := "keep running"
		if stopOnError {
			name = "stop on error"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var buf bytes.Buffer
				s := mustNew(t, 1, scheduler.WithLogger(warnLogger(&buf)))

				failure := errors.New("password=hunter2")
				job := mustSchedule(t, s, "job", time.Minute,
					func(context.Context, *scheduler.Job) error { return failure },
					scheduler.WithStopOnError(stopOnError),
					scheduler.WithErrorHandler(func(*scheduler.Job, error) {
						panic("handler bug")
					}))

				synctest.Sleep(90 * time.Second) // runs at 0 and 1m
				runs, stopped := job.Runs(), job.Stopped()
				s.Stop() // no more writes to buf after this

				wantRuns := 2
				if stopOnError {
					wantRuns = 1
				}
				if runs != wantRuns || stopped != stopOnError {
					t.Errorf("Runs(), Stopped() = %d, %t; want %d, %t",
						runs, stopped, wantRuns, stopOnError)
				}

				log := buf.String()
				if got := strings.Count(log, "job error handler panicked"); got != wantRuns {
					t.Errorf("logged %d handler panics, want %d:\n%s", got, wantRuns, log)
				}
				if !strings.Contains(log, "handler bug") {
					t.Errorf("log is missing the handler's panic value:\n%s", log)
				}
				if strings.Contains(log, "hunter2") {
					t.Errorf("log contains the run's error, which the handler may redact:\n%s", log)
				}
			})
		})
	}
}
