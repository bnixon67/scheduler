package scheduler

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

// TestNewJob verifies that NewJob properly creates a Job.
func TestNewJob(t *testing.T) {
	id := "test"
	interval := time.Second
	called := false

	runFunc := func(*Job) bool {
		called = true
		return true
	}

	job := NewJob(id, interval, runFunc)

	// Test the Job's ID
	if job.id != id {
		t.Errorf("job.id = %q; want %q", job.id, id)
	}

	// Test the Job's interval
	if job.interval != interval {
		t.Errorf("job.interval = %v; want %v", job.interval, interval)
	}

	// Test that the run function is set and callable
	if !job.runFunc(job) {
		t.Errorf("job.runFunc() returned false; want true")
	}
	if !called {
		t.Errorf("job.runFunc() was not called")
	}

	// Test that the recover function is set
	if job.recoverFunc == nil {
		t.Errorf("job.recoverFunc is nil; want non-nil")
	}

	// Test that stopOnPanic is false
	if job.stopOnPanic != false {
		t.Errorf("job.stopOnPanic = %t; want false", job.stopOnPanic)
	}

	// Test that maxExecutions is set to zero (unlimited)
	if job.maxExecutions != 0 {
		t.Errorf("job.maxExecutions = %d; want 0", job.maxExecutions)
	}

	// Test that executions counter is zero
	if job.executions.Load() != 0 {
		t.Errorf("job.executions = %d; want 0", job.executions.Load())
	}

	// Test that the logger is set
	if job.logger == nil {
		t.Errorf("job.logger is nil; want non-nil")
	}

	// Test that the context and cancel function are set
	if job.cancelCtx == nil {
		t.Errorf("job.cancelCtx is nil; want non-nil")
	}
	if job.cancelFunc == nil {
		t.Errorf("job.cancelFunc is nil; want non-nil")
	}
}

// TestWithRecoveryHandler verifies that the custom function to handle panics
// works properly.
func TestWithRecoveryHandler(t *testing.T) {
	called := false

	job := NewJob("test",
		time.Second,
		func(*Job) bool {
			panic("test recovery handler")
			return true
		},
		WithRecoveryHandler(
			func(*Job, any) {
				called = true
			},
		),
	)

	shouldContinue := job.execute()

	if !shouldContinue {
		t.Errorf("shouldContinue is false, want true")
	}

	if !called {
		t.Errorf("recoveryHandler was not called")
	}
}

// TestWithMaxExecutions confirms that WithMaxExecutions option is observed.
func TestWithMaxExecutions(t *testing.T) {
	tests := []struct {
		maxExecutions uint64
		wantCounts    []uint64
	}{
		{maxExecutions: 1, wantCounts: []uint64{1, 1}},
		{maxExecutions: 2, wantCounts: []uint64{1, 2, 2}},
		{maxExecutions: 3, wantCounts: []uint64{1, 2, 3, 3}},
	}

	for _, tc := range tests {
		name := fmt.Sprintf("maxExecutions=%d", tc.maxExecutions)
		t.Run(name, func(t *testing.T) {
			job := NewJob("test",
				time.Second,
				func(*Job) bool { return true },
				WithMaxExecutions(tc.maxExecutions),
			)

			for i, expectedCount := range tc.wantCounts {
				gotContinue := job.execute()
				gotExecutions := job.executions.Load()
				wantContinue := i < int(tc.maxExecutions)

				if gotExecutions != expectedCount {
					t.Errorf("Run %d: job.executions = %d, want %d", i+1, gotExecutions, expectedCount)
				}
				if gotContinue != wantContinue {
					t.Errorf("Run %d: continue = %t, want %t", i+1, gotContinue, wantContinue)
				}
			}
		})
	}
}

func TestWithStopOnPanic(t *testing.T) {
	tests := []struct {
		name         string
		stopOnPanic  bool
		wantContinue bool
	}{
		{name: "continue", stopOnPanic: false, wantContinue: true},
		{name: "stop", stopOnPanic: true, wantContinue: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var executions atomic.Uint64

			job := NewJob(tc.name,
				time.Second,
				func(*Job) bool {
					executions.Add(1)
					panic(tc.name)
					return true
				},
				WithStopOnPanic(tc.stopOnPanic),
				WithRecoveryHandler(func(*Job, any) {}),
			)

			gotContinue := job.execute()
			if gotContinue != tc.wantContinue {
				t.Errorf("continue = %t, want %t",
					gotContinue, tc.wantContinue)
			}

			gotExecutions := executions.Load()
			wantExecutions := uint64(1)
			if gotExecutions != wantExecutions {
				t.Errorf("executions = %d, want %d",
					gotExecutions, wantExecutions)
			}
		})
	}

}

// TestDefaultRecoveryHandler verifies that the defaultRecoveryHandler logs
// the panic as expected.
func TestDefaultRecoveryHandler(t *testing.T) {
	var logBuffer bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logBuffer, nil))

	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{
		logger:     logger,
		cancelCtx:  ctx,
		cancelFunc: cancel,
	}

	panicValue := "TestDefaultRecoveryHandler"
	defaultRecoveryHandler(job, panicValue)

	logOutput := logBuffer.String()
	expectedMsg := "job panicked"

	if !bytes.Contains([]byte(logOutput), []byte(expectedMsg)) {
		t.Errorf("logOutput = %q, want to contain msg %q",
			logOutput, expectedMsg)
	}

	if !bytes.Contains([]byte(logOutput), []byte(panicValue)) {
		t.Errorf("logOutput = %q, want to contain value %q",
			logOutput, panicValue)
	}
}

// TestStop verifies if Stop correctly sets the job to be stopped.
func TestJobStop(t *testing.T) {
	job := NewJob("test", time.Second, func(*Job) bool { return true })
	job.Stop()

	got := job.IsStopped()
	want := true
	if got != want {
		t.Errorf("IsStopped() = %t, want %t", got, want)
	}
}
