// Copyright (c) 2024 Bill Nixon

package scheduler

import (
	"context"
	"sync"
)

// Workers manages the execution of jobs using goroutines.
// It handles job queuing, context-based cancellation, and synchronization.
type Workers struct {
	jobQueueCh chan *Job
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// newWorkers creates and initializes a new instance of workers with a given
// job queue buffer size.
func newWorkers(bufferSize int) *Workers {
	ctx, cancel := context.WithCancel(context.Background())
	return &Workers{
		jobQueueCh: make(chan *Job, bufferSize),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// start launches the specified number of workers.
func (w *Workers) start(workerCount int) {
	for i := 0; i < workerCount; i++ {
		w.wg.Add(1)
		go w.worker(i)
	}
}

// submit adds a job to the job queue or returns an error if the queue is full.
func (w *Workers) submit(job *Job) error {
	// Check if the job has reached max executions and stop if so
	if job.maxExecutions > 0 && job.executions.Load() >= job.maxExecutions {
		job.Stop()
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

	for {
		select {
		case job := <-w.jobQueueCh:
			w.executeJob(job)
		case <-w.ctx.Done():
			return
		}
	}
}

// executeJob runs the job's function and recovers from any panics to prevent
// a failing job from crashing the workers.
func (w *Workers) executeJob(job *Job) {
	defer func() {
		if r := recover(); r != nil && job.recoverFunc != nil {
			job.recoverFunc(job, r)
		}
	}()

	job.run(job.id)       // Execute the job
	job.executions.Add(1) // Increment the execution count
}

// stop gracefully stops all workers and waits for them to finish.
func (w *Workers) stop() {
	w.cancel()  // Signal all workers to stop
	w.wg.Wait() // Wait for all workers to complete
}
