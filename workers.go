// Copyright (c) 2024 Bill Nixon

package scheduler

import (
	"context"
	"errors"
	"sync"
)

// workers manages the execution of jobs using goroutines.
// It handles job queuing, context-based cancellation, and synchronization.
type workers struct {
	jobQueue chan *Job
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// newWorkers creates and initializes a new instance of workers with a given
// job queue buffer size.
func newWorkers(bufferSize int) *workers {
	ctx, cancel := context.WithCancel(context.Background())
	return &workers{
		jobQueue: make(chan *Job, bufferSize),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// start launches the specified number of workers.
func (w *workers) start(workerCount int) {
	for i := 0; i < workerCount; i++ {
		w.wg.Add(1)
		go w.worker(i)
	}
}

// submit adds a job to the job queue or returns an error if the queue is full.
func (w *workers) submit(job *Job) error {
	select {
	case <-w.ctx.Done():
		return ErrWorkersStopping
	case w.jobQueue <- job:
		return nil
	default:
		return errors.New("job queue is full")
	}
}

// worker is a goroutine that continuously processes jobs from the jobQueue.
// It executes jobs until the context is canceled, ensuring that each job
// is run safely and any panic during execution is handled gracefully.
func (w *workers) worker(id int) {
	defer w.wg.Done()

	for {
		select {
		case job := <-w.jobQueue:
			if !job.isStopped.Load() {
				w.executeJob(job)
			}
		case <-w.ctx.Done():
			return
		}
	}
}

// executeJob runs the job's function and recovers from any panics to prevent
// a failing job from crashing the workers.
func (w *workers) executeJob(job *Job) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("job panicked", "job", job, "err", r)
		}
	}()
	job.run(job.id)
}

// stop gracefully stops all workers and waits for them to finish.
func (w *workers) stop() {
	w.cancel()  // Signal all workers to stop
	w.wg.Wait() // Wait for all workers to complete
}
