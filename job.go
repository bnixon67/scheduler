// Copyright (c) 2024 Bill Nixon

package scheduler

import (
	"errors"
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
	maxExecutions uint64        // Max executions; 0 for indefinite
	executions    atomic.Uint64 // Current number of executions
	stopCh        chan struct{} // Channel to signal the job to stop
	logger        *slog.Logger
}

// String returns a human-readable representation of the Job, including its
// ID, interval, maximum executions, current execution count, and whether
// the job is stopped.
func (job *Job) String() string {
	return fmt.Sprintf("Job{id: %s, interval: %s, maxExecutions: %d, executions: %d, isStopped: %t}",
		job.id, job.interval, job.maxExecutions, job.executions.Load(), isChannelClosed(job.stopCh))
}

// JobOption defines a configuration function for customizing Job behavior.
type JobOption func(*Job)

// WithRecoverFunc configures a custom function to handle panics during
// the job's execution. If the job's `run` function panics, this recovery
// function is called with the Job and the panic value, enabling custom
// error handling or cleanup.
func WithRecoverFunc(recoverFunc func(*Job, any)) JobOption {
	return func(j *Job) {
		j.recoverFunc = recoverFunc
	}
}

// WithMaxExecutions limits the number of times a job will run. If `n` is 0,
// the job runs indefinitely. Use this option to control how many times the
// job executes before stopping automatically.
func WithMaxExecutions(n uint64) JobOption {
	return func(j *Job) {
		j.maxExecutions = n
	}
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
	if interval <= 0 {
		panic("interval must be positive")
	}
	if run == nil {
		panic("run function cannot be nil")
	}

	job := &Job{
		id:          id,
		interval:    interval,
		run:         run,
		recoverFunc: defaultRecoverFunc,
		stopCh:      make(chan struct{}),
		logger:      slog.Default(),
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

// Stop signals the Job to halt further execution. Once stopped, the Job
// will not be re-queued or run again.
func (job *Job) Stop() {

	select {
	case <-job.stopCh: // If already closed, do nothing
		job.logger.Warn("stopCh is already closed", "job", job)
	default:
		close(job.stopCh)
		job.logger.Debug("stopped", "job", job)
	}
}

// logSubmitError logs an error encountered when submitting the Job for
// execution.  Adjusts the log level based on the error type, using Info
// for expected stop conditions (like ErrWorkersStopping) and Error for
// other cases.
func (job *Job) logSubmitError(err error) {
	logLevel := job.logger.Error
	if errors.Is(err, ErrWorkersStopping) {
		logLevel = job.logger.Info
	}

	logLevel("failed to submit job", "job", job, "err", err)
}

// start begins the periodic execution of the Job in a separate goroutine.
// The Job will continue to execute at each interval until it is stopped,
// its `run` function returns false, or the context of the workers is canceled.
func (job *Job) start(wp *Workers) {
	log := job.logger.With("job", job) // Attach job context once

	// Helper function
	submitJob := func() {
		if err := wp.submit(job); err != nil {
			job.logSubmitError(err)
		}
	}

	go func() {
		submitJob() // Initial job submission

		// Start the periodic execution ticker
		ticker := time.NewTicker(job.interval)
		defer ticker.Stop()

		for {
			select {
			case <-wp.ctx.Done():
				log.Debug("context done", "job", job)
				return
			case <-job.stopCh: // Listen for stop signal from stopCh
				log.Debug("stopCh closed", "job", job)
				return
			case <-ticker.C:
				log.Debug("requeue", "job", job)
				submitJob() // Requeue job
			}
		}
	}()
}

// isChannelClosed checks if a given channel has been closed. It returns true
// if the channel is closed, and false if it is still open.
func isChannelClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		// If a value is received, the channel is closed
		return true
	default:
		// If no value is received, the channel is still open
		return false
	}
}

// defaultRecoverFunc is called when a panic occurs in a job's run function
// and no custom recover function is provided. It logs the panic if logger
// is defined.
func defaultRecoverFunc(job *Job, v any) {
	job.logger.Error("job panicked", "job", job, "err", v)
}
