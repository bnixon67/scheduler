package scheduler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// mustNewJob calls newJob with logger and fails the test if it returns an
// error. The job has no Scheduler, so tests drive it by calling execute.
func mustNewJob(t testing.TB, logger *slog.Logger, run RunFunc,
	opts ...JobOption,
) *Job {
	t.Helper()
	job, err := newJob("test", time.Second, run, logger, opts...)
	if err != nil {
		t.Fatalf("newJob() = %v", err)
	}
	return job
}

// keepRunning is a RunFunc that always asks to run again.
func keepRunning(context.Context, *Job) error { return nil }

// logTo returns a logger that writes text records at every level to buf.
func logTo(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf,
		&slog.HandlerOptions{Level: slog.LevelDebug}))
}

// TestNewJob verifies that newJob sets every field and starts with fresh
// runtime state.
func TestNewJob(t *testing.T) {
	logger := slog.Default()
	called := false
	run := func(context.Context, *Job) error {
		called = true
		return nil
	}

	job, err := newJob("test", time.Second, run, logger)
	if err != nil {
		t.Fatalf("newJob() = %v", err)
	}

	if job.name != "test" || job.interval != time.Second {
		t.Errorf("name, interval = %q, %v; want %q, %v",
			job.name, job.interval, "test", time.Second)
	}
	if err := job.run(t.Context(), nil); err != nil || !called {
		t.Errorf("job.run() = %v, called = %t; want nil, true", err, called)
	}
	if job.onError == nil {
		t.Error("job.onError is nil; want the default handler")
	}
	if job.stopOnError || job.maxRuns != 0 {
		t.Errorf("stopOnError, maxRuns = %t, %d; want false, 0",
			job.stopOnError, job.maxRuns)
	}
	if job.logger != logger {
		t.Error("logger not set")
	}
	if job.Runs() != 0 || job.Skipped() != 0 {
		t.Errorf("Runs(), Skipped() = %d, %d; want 0, 0",
			job.Runs(), job.Skipped())
	}
	if job.Context() == nil || job.Stopped() {
		t.Errorf("want a live context, got Stopped() = %t", job.Stopped())
	}
}

// TestExecuteResults verifies how each kind of RunFunc result affects
// whether the job runs again and whether the error handler is called.
func TestExecuteResults(t *testing.T) {
	failure := errors.New("failure")

	tests := []struct {
		name        string
		run         RunFunc
		stopOnError bool
		wantAgain   bool
		wantErr     error // passed to the handler; nil if not called
	}{
		{"nil", keepRunning, false, true, nil},
		{"ErrStop", func(context.Context, *Job) error { return ErrStop }, false, false, nil},
		{"wrapped ErrStop", func(context.Context, *Job) error {
			return fmt.Errorf("done: %w", ErrStop)
		}, false, false, nil},
		{"error", func(context.Context, *Job) error { return failure }, false, true, failure},
		{"error, stop on error", func(context.Context, *Job) error { return failure }, true, false, failure},
		{"panic", func(context.Context, *Job) error { panic(failure) }, false, true, &PanicError{}},
		{"panic, stop on error", func(context.Context, *Job) error { panic(failure) }, true, false, &PanicError{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got error
			job := mustNewJob(t, slog.Default(), tc.run,
				WithStopOnError(tc.stopOnError),
				WithErrorHandler(func(_ *Job, err error) { got = err }))

			if again := job.execute(); again != tc.wantAgain {
				t.Errorf("execute() = %t, want %t", again, tc.wantAgain)
			}

			switch want := tc.wantErr.(type) {
			case nil:
				if got != nil {
					t.Errorf("handler got %v, want it not called", got)
				}
			case *PanicError:
				var pe *PanicError
				if !errors.As(got, &pe) || pe.Value != failure || len(pe.Stack) == 0 {
					t.Errorf("handler got %#v, want a *PanicError with value and stack", got)
				}
			default:
				if got != want {
					t.Errorf("handler got %v, want %v", got, want)
				}
			}
		})
	}
}

// TestExecuteCanceledAfterStop verifies a run that returns ctx.Err() after
// its job is stopped is not reported as a failure.
func TestExecuteCanceledAfterStop(t *testing.T) {
	called := false
	job := mustNewJob(t, slog.Default(),
		func(ctx context.Context, j *Job) error {
			j.Stop()
			return ctx.Err()
		},
		WithErrorHandler(func(*Job, error) { called = true }))

	job.execute()

	if called {
		t.Error("error handler called for a run canceled by Stop")
	}
}

// TestWithMaxRuns confirms that WithMaxRuns option is observed.
func TestWithMaxRuns(t *testing.T) {
	tests := []struct {
		maxRuns    int
		wantCounts []int
	}{
		{maxRuns: 1, wantCounts: []int{1, 1}},
		{maxRuns: 2, wantCounts: []int{1, 2, 2}},
		{maxRuns: 3, wantCounts: []int{1, 2, 3, 3}},
		{maxRuns: 0, wantCounts: []int{1, 2, 3, 4}},
		{maxRuns: -1, wantCounts: []int{1, 2, 3, 4}},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("maxRuns=%d", tc.maxRuns), func(t *testing.T) {
			job := mustNewJob(t, slog.Default(), keepRunning,
				WithMaxRuns(tc.maxRuns))

			for i, wantCount := range tc.wantCounts {
				gotAgain := job.execute()
				// The run that reaches the limit already reports
				// that the job should stop.
				wantAgain := tc.maxRuns <= 0 || i+1 < tc.maxRuns

				if got := job.Runs(); got != wantCount {
					t.Errorf("run %d: runs = %d, want %d", i+1, got, wantCount)
				}
				if gotAgain != wantAgain {
					t.Errorf("run %d: again = %t, want %t", i+1, gotAgain, wantAgain)
				}
			}
		})
	}
}

// TestDefaultErrorHandler verifies the default handler logs errors, and
// logs panics with their value and stack trace.
func TestDefaultErrorHandler(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{"error", errors.New("disk full"), []string{`msg="job failed"`, "disk full"}},
		{"panic", &PanicError{Value: "boom", Stack: []byte("main.go:1")},
			[]string{`msg="job panicked"`, "value=boom", "main.go:1"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			job := mustNewJob(t, logTo(&buf), keepRunning)

			defaultErrorHandler(job, tc.err)

			for _, want := range tc.want {
				if !strings.Contains(buf.String(), want) {
					t.Errorf("log = %q, want it to contain %q", buf.String(), want)
				}
			}
		})
	}
}

// TestWithErrorHandlerNil verifies a nil handler falls back to the
// default, so errors are still logged rather than silently swallowed.
func TestWithErrorHandlerNil(t *testing.T) {
	var buf bytes.Buffer
	job := mustNewJob(t, logTo(&buf),
		func(context.Context, *Job) error { panic("nil handler") },
		WithErrorHandler(nil))

	if !job.execute() {
		t.Error("execute() = false, want true (stopOnError is off)")
	}
	if !strings.Contains(buf.String(), "job panicked") {
		t.Errorf("log = %q, want a %q entry", buf.String(), "job panicked")
	}
}

// TestJobStop verifies Stop marks the job stopped, cancels its context, and
// logs only once however often it is called.
func TestJobStop(t *testing.T) {
	var buf bytes.Buffer
	job := mustNewJob(t, logTo(&buf), keepRunning)

	job.Stop()
	job.Stop()

	if !job.Stopped() {
		t.Error("Stopped() = false, want true")
	}
	if job.Context().Err() == nil {
		t.Error("Context() not canceled after Stop")
	}
	if n := strings.Count(buf.String(), `msg=stopped`); n != 1 {
		t.Errorf("logged %d stop records, want 1:\n%s", n, buf.String())
	}
}
