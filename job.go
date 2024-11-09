// Copyright (c) 2024 Bill Nixon

package scheduler

import (
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
)

// Job defines a periodic task with an interval and a function to execute.
type Job struct {
	id            string
	interval      time.Duration
	run           func(string)
	recoverFunc   func(*Job, any)
	maxExecutions int64         // Max executions; 0 for indefinite
	executions    atomic.Int64  // Current number of executions
	stopCh        chan struct{} // Channel to signal the job to stop
	logger        *slog.Logger
}

// JobOption defines a function type for setting optional parameters in a Job.
type JobOption func(*Job)

// WithRecoverFunc sets a custom recover function for the job.
func WithRecoverFunc(recoverFunc func(*Job, any)) JobOption {
	return func(j *Job) {
		j.recoverFunc = recoverFunc
	}
}

// WithMaxExecutions sets a maximum number of executions for the job.
// If 0, run indefinitely.
func WithMaxExecutions(n int64) JobOption {
	return func(j *Job) {
		j.maxExecutions = n
	}
}

// NewJob returns a new Job with the given ID, interval, and run function.
// Panics if the interval is non-positive or the run function is nil.
func NewJob(id string, interval time.Duration, run func(string), opts ...JobOption) *Job {
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

// ID returns the id of the job.
func (job *Job) ID() string {
	return job.id
}

// Interval returns the interval at which the job is scheduled to run.
func (job *Job) Interval() time.Duration {
	return job.interval
}

// Stop prevents the job from being re-queued.
func (job *Job) Stop() {
	job.logger.Debug("stop", "job", job)

	select {
	case <-job.stopCh: // If already closed, do nothing
		job.logger.Warn("stopCh is already closed", "job", job)
	default:
		close(job.stopCh)
	}
}

// String returns a human-readable representation of the Job.
func (job *Job) String() string {
	return fmt.Sprintf("Job{id: %s, interval: %s, isStopped: %t}",
		job.id, job.interval, isChannelClosed(job.stopCh))
}

func (job *Job) handleSubmitError(err error) {
	logLevel := job.logger.Error
	if errors.Is(err, ErrWorkersStopping) {
		logLevel = job.logger.Info
	}

	logLevel("failed to submit job", "job", job, "err", err)
}

// start begins the job's periodic execution in a separate goroutine.
// Execution will stop if the job is marked as stopped or the context is done.
func (job *Job) start(wp *Workers) {
	log := job.logger.With("job", job) // Attach job context once

	// Helper function
	submitJob := func() {
		if err := wp.submit(job); err != nil {
			job.handleSubmitError(err)
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

// isChannelClosed returns true if ch is closed, otherwise false.
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
