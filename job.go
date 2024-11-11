// Copyright (c) 2024 Bill Nixon

package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
)

// Job represents a periodic task that executes at a specified interval.
// A Job runs until it is stopped, reaches its maximum executions, or its
// `run` function returns false.  Optional behaviors, like panic recovery
// and execution limits, can be configured.
type Job struct {
	id            string
	interval      time.Duration
	run           func(*Job) bool
	recoverFunc   func(*Job, any)
	maxExecutions uint64
	executions    atomic.Uint64
	logger        *slog.Logger
	ctx           context.Context    // Job's context for cancellation
	cancel        context.CancelFunc // Function to cancel the job
}

// String returns a human-readable representation of the Job.
func (job *Job) String() string {
	return fmt.Sprintf("Job{id: %s, interval: %s, maxExecutions: %d, executions: %d, isStopped: %t}",
		job.id, job.interval, job.maxExecutions, job.executions.Load(), job.isStopped())
}

// JobOption configures optional Job parameters.
type JobOption func(*Job)

// WithRecoverFunc configures a custom function to handle panics during
// the job's execution. If the job's `run` function panics, this recovery
// function is called with the Job and the panic value, enabling custom
// error handling or cleanup.
func WithRecoverFunc(recoverFunc func(*Job, any)) JobOption {
	return func(j *Job) { j.recoverFunc = recoverFunc }
}

// WithMaxExecutions limits the number of times a job will run. If `n` is 0,
// the job runs indefinitely. Use this option to control how many times the
// job executes before stopping automatically.
func WithMaxExecutions(n uint64) JobOption {
	return func(j *Job) { j.maxExecutions = n }
}

// NewJob creates a periodic job with a unique ID, a positive interval between
// executions, and a `run` function that executes at each interval. Optional
// settings can be applied using JobOptions. The job stops re-queuing if the
// `run` function returns false.
//
// Panics if the interval is non-positive or if `run` is nil.
//
// Returns a pointer to the configured Job.
func NewJob(id string, interval time.Duration, run func(*Job) bool, opts ...JobOption) *Job {
	if interval <= 0 || run == nil {
		panic("interval must be positive; run function cannot be nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{
		id:          id,
		interval:    interval,
		run:         run,
		recoverFunc: defaultRecoverFunc,
		logger:      slog.Default(),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Apply each option to the job
	for _, opt := range opts {
		opt(job)
	}

	return job
}

// ID returns the unique identifier for the Job, which can be used to
// reference or manage the job within a scheduler.
func (job *Job) ID() string {
	return job.id
}

// Interval returns the time duration between each execution of the Job.
func (job *Job) Interval() time.Duration {
	return job.interval
}

// Stop prevents the job from being re-queued.
func (job *Job) Stop() {
	job.cancel() // Cancel the job context
	job.logger.Debug("stopped", "job", job)
}

// isStopped checks if the job's context has been canceled.
func (job *Job) isStopped() bool {
	select {
	case <-job.ctx.Done():
		return true
	default:
		return false
	}
}

// defaultRecoverFunc is called when a panic occurs in a job's run function
// and no custom recover function is provided.
func defaultRecoverFunc(job *Job, v any) {
	job.logger.Error("job panicked", "job", job, "error", v)
}

// executeJob runs the job's function and recovers from any panics to prevent
// a failing job from crashing the workers.
func (job *Job) execute() bool {
	defer func() {
		if r := recover(); r != nil && job.recoverFunc != nil {
			job.recoverFunc(job, r)
		}
	}()

	if job.maxExecutions > 0 && job.executions.Load() >= job.maxExecutions {
		job.logger.Debug("max executions reached", "job", job)
		return false
	}

	job.executions.Add(1) // Increment the execution count
	return job.run(job)   // Execute the job
}
