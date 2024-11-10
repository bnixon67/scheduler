// Copyright (c) 2024 Bill Nixon

package scheduler

import (
	"context"
	"log/slog"
	"sync"
)

// Workers manages the execution of jobs using goroutines.
// It handles job queuing, context-based cancellation, and synchronization.
type Workers struct {
	jobQueueCh chan *Job
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	logger     *slog.Logger
}

// newWorkers creates and initializes a new instance of workers with a given
// job queue buffer size.
func newWorkers(bufferSize int, logger *slog.Logger) *Workers {
	ctx, cancel := context.WithCancel(context.Background())
	return &Workers{
		jobQueueCh: make(chan *Job, bufferSize),
		ctx:        ctx,
		cancel:     cancel,
		logger:     logger,
	}
}

// start launches the specified number of workers.
func (w *Workers) start(count int) {
	for i := 0; i < count; i++ {
		w.wg.Add(1)
		go w.worker(i)
	}
}

// submit adds a job to the job queue or returns an error if the queue is full.
func (w *Workers) submit(job *Job) error {
	// Stop if the job has reached max executions
	if job.maxExecutions > 0 && job.executions.Load() >= job.maxExecutions {
		job.Stop()
		job.logger.Debug("max executions reached", "job", job)
		return nil
	}

	select {
	case <-w.ctx.Done():
		return ErrWorkersStopping
	case w.jobQueueCh <- job:
		return nil
	default:
		return ErrJobQueueIsFull
	}
}

// worker is a goroutine that continuously processes jobs from the jobQueue.
// It executes jobs until the context is canceled, ensuring that each job
// is run safely and any panic during execution is handled gracefully.
func (w *Workers) worker(id int) {
	defer w.wg.Done()

	w.logger.Debug("started", "workerID", id)
	defer w.logger.Debug("stopped", "workerID", id)

	for {
		select {
		case job := <-w.jobQueueCh:
			// Execute the job and stop if run returns false
			w.logger.Debug("executing", "workerID", id, "job", job)
			if !executeJob(job) {
				job.Stop()
			}
		case <-w.ctx.Done():
			return
		}
	}
}

// executeJob runs the job's function and recovers from any panics to prevent
// a failing job from crashing the workers.
func executeJob(job *Job) bool {
	defer func() {
		if r := recover(); r != nil && job.recoverFunc != nil {
			job.recoverFunc(job, r)
		}
	}()

	job.executions.Add(1)  // Increment the execution count
	result := job.run(job) // Execute the job

	return result
}

// stop stops workers and waits for them to finish.
func (w *Workers) stop() {
	w.cancel()  // Signal all workers to stop
	w.wg.Wait() // Wait for all workers to complete
}
