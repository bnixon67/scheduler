// Copyright (c) 2024 Bill Nixon

// Package scheduler provides a framework for managing and executing jobs
// periodically with configurable options for interval, maximum executions,
// and panic recovery. It supports concurrent job execution using worker
// goroutines and provides functionality to start, stop, and retrieve jobs
// by ID.
package scheduler

import (
	"log/slog"
	"sync"
)

// Scheduler manages job scheduling and execution using worker goroutines.
type Scheduler struct {
	workers *Workers
	jobs    sync.Map // Map of job IDs to *Job
	logger  *slog.Logger
}

// NewScheduler creates a Scheduler with a job queue buffer and a specified
// number of worker goroutines.
//
// bufferSize controls the maximum number of jobs that can be queued at
// once. If the queue is full, new jobs may be rejected until space becomes
// available.
//
// workerCount sets the number of goroutines that process jobs
// concurrently. More workers allow multiple jobs to run at the same time.
//
// opts like WithLogger allow further customization.
func NewScheduler(bufferSize int, workerCount int, opts ...SchedulerOption) *Scheduler {
	s := &Scheduler{
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}

	s.workers = newWorkers(bufferSize, s.logger)
	s.workers.start(workerCount)

	return s
}

// SchedulerOption configures optional settings for a Scheduler.
type SchedulerOption func(*Scheduler)

// WithLogger sets a custom logger for the Scheduler.
func WithLogger(l *slog.Logger) SchedulerOption {
	return func(s *Scheduler) { s.logger = l }
}

// JobIDs returns the IDs of the scheduled jobs.
func (s *Scheduler) JobIDs() []string {
	var ids []string

	s.jobs.Range(func(key, value interface{}) bool {
		if id, ok := key.(string); ok {
			ids = append(ids, id)
		}
		return true
	})

	return ids
}

// GetJob retrieves a job by its ID.
func (s *Scheduler) GetJob(id string) *Job {
	value, ok := s.jobs.Load(id)
	if !ok {
		return nil
	}

	return value.(*Job)
}

// AddJob adds a job to the scheduler for execution.
// It returns an error if the job ID already exists.
func (s *Scheduler) AddJob(job *Job) error {
	if job == nil {
		return ErrNilJob
	}

	// Assign Scheduler's logger to Job
	job.logger = s.logger

	// Check for duplicate job IDs
	if _, loaded := s.jobs.LoadOrStore(job.id, job); loaded {
		return ErrJobIDExists
	}

	//job.schedule(s.workers)
	s.workers.scheduleJob(job)
	return nil
}

// Stop halts all the workers.
func (s *Scheduler) Stop() {
	s.workers.stop()
}

// StopJob stops a specific job from being re-queued.
func (s *Scheduler) StopJob(jobID string) error {
	value, ok := s.jobs.LoadAndDelete(jobID)
	if !ok {
		return ErrJobNotFound
	}

	value.(*Job).Stop()

	return nil
}
