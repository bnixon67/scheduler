// Copyright (c) 2024 Bill Nixon

// Package scheduler manages and executes jobs periodically. It uses worker
// goroutines to handle concurrent job executions and provides mechanisms
// to start, stop, and monitor jobs.
package scheduler

import (
	"fmt"
	"sync"
)

// Scheduler manages job scheduling and execution using worker goroutines.
// It maintains a collection of jobs and handles distribution and lifecycle.
type Scheduler struct {
	workers *workers
	jobs    sync.Map // Map of job IDs to *Job
}

// NewScheduler creates a new Scheduler and starts the workers immediately.
func NewScheduler(bufferSize int, workerCount int) *Scheduler {
	s := &Scheduler{
		workers: newWorkers(bufferSize),
	}
	s.workers.start(workerCount)
	return s
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
	if value, ok := s.jobs.Load(jobID); ok {
		job := value.(*Job)
		job.Stop() // Set the stop flag to true
		s.jobs.Delete(jobID)
	} else {
		return fmt.Errorf("job %s not found", jobID)
	}
	return nil
}
