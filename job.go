// Copyright (c) 2024 Bill Nixon

package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
)

// Job represents a periodic task that executes at specified intervals.
//
// A Job continues to run until one of the following conditions is met:
//   - It is explicitly stopped using Stop().
//   - It reaches its maximum execution count, set with WithMaxExecutions().
//   - Its run function returns false, indicating it should not be rescheduled.
//   - A panic occurs during execution and job created WithStopOnPanic(true).
//
// A RecoveryHandler can be provided via WithRecoveryHandler() to handle
// panics that occur during the execution of the run function. The default
// Recovery Handler logs an error.
//
// Users can configure the Job to stop running after a panic using the
// WithStopOnPanic(). The default behavior is to continue running after
// a panic.
type Job struct {
	id            string
	interval      time.Duration
	runFunc       func(*Job) bool
	recoverFunc   func(*Job, any)
	stopOnPanic   bool
	maxExecutions uint64
	executions    atomic.Uint64
	logger        *slog.Logger
	cancelCtx     context.Context
	cancelFunc    context.CancelFunc
}

// String returns a human-readable representation of the Job.
func (j *Job) String() string {
	return fmt.Sprintf(
		"Job{"+
			"id: %s, "+
			"interval: %s, "+
			"maxExecutions: %d, "+
			"executions: %d, "+
			"stopOnPanic: %t, "+
			"isStopped: %t}",
		j.id,
		j.interval,
		j.maxExecutions,
		j.executions.Load(),
		j.stopOnPanic,
		j.IsStopped(),
	)
}

// NewJob creates and returns a pointer to a job that runs periodically
// when addded to a Scheduler. The job includes an ID, that must be unique
// within each Scheduler, a positive interval between executions, and a
// `run` function that executes at each interval. Optional settings can be
// applied using JobOptions.
//
// A Job continues to run until one of the following conditions is met:
//   - It is explicitly stopped using Stop().
//   - It reaches its maximum execution count, set with WithMaxExecutions().
//   - Its run function returns false, indicating it should not be rescheduled.
//   - A panic occurs during execution and job created WithStopOnPanic(true).
//
// If the interval is non-positive or `run` is nil, NewJob panics.
func NewJob(id string, interval time.Duration, run func(*Job) bool, opts ...JobOption) *Job {
	if interval <= 0 || run == nil {
		panic("interval must be positive; run function cannot be nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{
		id:          id,
		interval:    interval,
		runFunc:     run,
		recoverFunc: defaultRecoveryHandler,
		logger:      slog.Default(),
		cancelCtx:   ctx,
		cancelFunc:  cancel,
	}

	// Configure optional parameters.
	for _, opt := range opts {
		opt(job)
	}

	return job
}

// JobOption configures optional Job parameters.
type JobOption func(*Job)

// WithRecoveryHandler configures a custom function to handle panics during
// the job's execution. If the job's `run` function panics, this recovery
// function is called with the Job and the panic value, enabling custom
// error handling or cleanup.
func WithRecoveryHandler(recoveryHandler func(*Job, any)) JobOption {
	return func(j *Job) { j.recoverFunc = recoveryHandler }
}

// WithMaxExecutions limits the number of times a job will run. If `n` is 0,
// the job runs indefinitely. Use this option to control how many times the
// job executes before stopping automatically.
func WithMaxExecutions(n uint64) JobOption {
	return func(j *Job) { j.maxExecutions = n }
}

// WithStopOnPanic configures the Job to stop running after a panic occurs
// in the run function. If set to true, the Job will not be rescheduled
// after a panic. The default behavior is to continue running after a panic.
func WithStopOnPanic(b bool) JobOption {
	return func(j *Job) { j.stopOnPanic = b }
}

// ID returns the unique identifier for the Job, which can be used to
// reference or manage the job within a scheduler.
func (j *Job) ID() string {
	return j.id
}

// Interval returns the time duration between each execution of the Job.
func (j *Job) Interval() time.Duration {
	return j.interval
}

// Stop prevents the job from being re-queued.
func (j *Job) Stop() {
	j.cancelFunc() // Cancel the job context
	j.logger.Debug("stopped", "job", j)
}

// IsStopped returns true if the Job will no longer be scheduled for execution.
func (j *Job) IsStopped() bool {
	select {
	case <-j.cancelCtx.Done():
		return true
	default:
		return false
	}
}

// defaultRecoveryHandler is called when a panic occurs in a job's run function
// and no custom recover function is provided.
func defaultRecoveryHandler(j *Job, v any) {
	j.logger.Error("job panicked", "job", j, "error", v)
}

// hasReachedMaxExecutions checks if the Job has reached its maximum number
// of executions. It returns true if maxExecutions is set (> 0) and the
// current execution count is equal to or exceeds it.
func (j *Job) hasReachedMaxExecutions() bool {
	return j.maxExecutions > 0 && j.executions.Load() >= j.maxExecutions
}

// execute calls the run function of the Job and recovers from any panics
// to prevent a failing job from crashing the workers. It returns a boolean
// indicating whether the Job should be rescheduled.
//
// A job will not be rescheduled if the Job reaches its maximum executions,
// its run function returns false, or if a panic occurs and the Job is
// configured WithStopOnPanic(true).
func (j *Job) execute() bool {
	if j.hasReachedMaxExecutions() {
		j.logger.Debug("max executions reached", "job", j)
		return false
	}

	j.executions.Add(1)

	// Run the job's function within a closure to handle panics.
	var shouldReschedule bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				if j.recoverFunc != nil {
					j.recoverFunc(j, r)
				}

				shouldReschedule = !j.stopOnPanic
			}
		}()

		// Execute the job
		shouldReschedule = j.runFunc(j)
	}()

	return shouldReschedule
}
