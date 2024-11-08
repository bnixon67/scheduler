// Copyright (c) 2024 Bill Nixon

package scheduler

import (
	"fmt"
	"sync/atomic"
	"time"
)

// Job defines a periodic task with an interval and a function to execute.
type Job struct {
	id        string
	interval  time.Duration
	run       func(string)
	isStopped atomic.Bool   // Atomic flag to stop the job from re-queuing
	stopCh    chan struct{} // Channel to signal the job to stop
}

// NewJob returns a new Job with the given ID, interval, and run function.
// Panics if the interval is non-positive or the run function is nil.
func NewJob(id string, interval time.Duration, run func(string)) *Job {
	if interval <= 0 {
		panic("interval must be positive")
	}
	if run == nil {
		panic("run function cannot be nil")
	}

	return &Job{
		id:       id,
		interval: interval,
		run:      run,
		stopCh:   make(chan struct{}),
	}
}

// String returns a human-readable representation of the Job.
func (job *Job) String() string {
	return fmt.Sprintf("Job{id: %s, interval: %s, isStopped: %t}",
		job.id, job.interval, job.isStopped.Load())
}

// start begins the job's periodic execution in a separate goroutine.
// Execution will stop if the job is marked as stopped or the context is done.
func (job *Job) start(wp *workers) {
	go func() {
		if job.isStopped.Load() {
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
				if job.isStopped.Load() {
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
	if job.isStopped.CompareAndSwap(false, true) {
		close(job.stopCh)
	}
}

// Interval returns the interval at which the job is scheduled to run.
func (job *Job) Interval() time.Duration {
	return job.interval
}

// ID returns the id of the job.
func (job *Job) ID() string {
	return job.id
}
