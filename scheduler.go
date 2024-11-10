// Copyright (c) 2024 Bill Nixon

// Package scheduler manages and executes jobs periodically. It uses worker
// goroutines to handle concurrent job executions and provides mechanisms
// to start, stop, and monitor jobs.
package scheduler

import (
	"log/slog"
	"sync"
)

// Scheduler manages job scheduling and execution using worker goroutines.
// It maintains a collection of jobs and handles distribution and lifecycle.
type Scheduler struct {
	workers *Workers
	jobs    sync.Map // Map of job IDs to *Job
	logger  *slog.Logger
}

// NewScheduler creates a new Scheduler and starts the workers immediately.
func NewScheduler(bufferSize int, workerCount int, opts ...SchedulerOption) *Scheduler {
	s := &Scheduler{
		logger: slog.Default(),
	}

	// Apply each option to the scheduler
	for _, opt := range opts {
		opt(s)
	}

	s.workers = newWorkers(bufferSize, s.logger)
	s.workers.start(workerCount)

	return s
}

type SchedulerOption func(*Scheduler)

// WithLogger sets a custom logger for the Scheduler.
func WithLogger(logger *slog.Logger) SchedulerOption {
	return func(s *Scheduler) {
		s.logger = logger
	}
}

// Jobs returns a slice of job IDs for the jobs currently scheduled.
func (s *Scheduler) Jobs() []string {
	var ids []string
	s.jobs.Range(func(key, value interface{}) bool {
		id, ok := key.(string)
		if ok {
			ids = append(ids, id)
		}
		return true
	})
	return ids
}

// Job retrieves a job by its ID.
func (s *Scheduler) Job(id string) *Job {
	value, ok := s.jobs.Load(id)
	if !ok {
		return nil
	}
	job, _ := value.(*Job)
	return job
}

// AddJob submits a job to the scheduler and stores it in the jobs map.
// It returns an error if the job could not be added to the job queue.
func (s *Scheduler) AddJob(job *Job) error {
	if job == nil {
		return ErrJobIsNil
	}

	// Assign Scheduler's logger to Job
	job.logger = s.logger

	// Check for duplicate job IDs
	if _, loaded := s.jobs.LoadOrStore(job.id, job); loaded {
		return ErrJobIDExists
	}

	job.start(s.workers)
	return nil
}

// Stop stops the Scheduler and all its workers.
func (s *Scheduler) Stop() {
	s.workers.stop()
}

// StopJob stops a specific job from being re-queued.
func (s *Scheduler) StopJob(jobID string) error {
	value, ok := s.jobs.LoadAndDelete(jobID)
	if !ok {
		return ErrJobNotFound
	}

	job := value.(*Job)
	job.Stop()

	return nil
}
