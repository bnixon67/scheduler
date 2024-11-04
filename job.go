// Copyright (c) 2024 Bill Nixon

package scheduler

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// Job represents a job to be executed periodically.
type Job struct {
	id       string
	interval time.Duration
	run      func()
	stop     atomic.Bool   // Atomic flag to stop the job from re-queuing
	stopCh   chan struct{} // Channel to signal the job to stop
}

// NewJob creates a new Job with a specified interval and a function to
// run periodically.
// The interval must be a positive duration, and the run function must not
// be nil. If either are invalid, NewJob will panic.
func NewJob(interval time.Duration, run func()) *Job {
	if interval <= 0 {
		panic("interval must be positive")
	}
	if run == nil {
		panic("run function must not be nil")
	}

	return &Job{
		id:       uuid.NewString(),
		interval: interval,
		run:      run,
		stopCh:   make(chan struct{}),
	}
}

// String returns a human-readable representation of the Job.
func (job *Job) String() string {
	isStopped := "running"
	if job.stop.Load() {
		isStopped = "stopped"
	}
	return fmt.Sprintf("Job{id: %s, interval: %s, status: %s}",
		job.id, job.interval, isStopped)
}

// start initiates the job's execution loop in a new goroutine.
func (job *Job) start(wp *workers) {
	go func() {
		if job.stop.Load() {
			return
		}

		// Submit the job immediately
		if err := wp.submit(job); err != nil {
			logger.Error("failed to submit job",
				"job", job, "err", err)
		}

		// Start the ticker with the job's interval
		ticker := time.NewTicker(job.interval)
		defer ticker.Stop()

		for {
			select {
			case <-wp.ctx.Done():
				return
			case <-job.stopCh:
				return
			case <-ticker.C:
				if job.stop.Load() {
					return
				}
				if err := wp.submit(job); err != nil {
					logger.Error("failed to submit job",
						"job", job, "err", err)
				}
			}
		}
	}()
}

// Stop sets the stop flag to prevent the job from being re-queued.
func (job *Job) Stop() {
	if job.stop.CompareAndSwap(false, true) {
		close(job.stopCh)
	}
}

// Interval returns the interval at which the job is scheduled to run.
func (job *Job) Interval() time.Duration {
	return job.interval
}

// ID returns the interval at which the job is scheduled to run.
func (job *Job) ID() string {
	return job.id
}
