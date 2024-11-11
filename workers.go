// Copyright (c) 2024 Bill Nixon

package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Workers manages the execution of jobs using goroutines.
// It handles job queuing, cancellation, and synchronization.
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

func (w *Workers) submit(job *Job) error {
	select {
	case w.jobQueueCh <- job:
		return nil // job queued
	default:
		return ErrJobQueueFull
	}
}

func (w *Workers) schedule(job *Job) {
	log := job.logger.With("job", job)

	go func() {
		// Initial submission
		if err := w.submit(job); err != nil {
			log.Error("failed to submit", "error", err)
		}

		ticker := time.NewTicker(job.interval)
		defer ticker.Stop()

		for {
			select {
			case <-w.ctx.Done():
				log.Debug("workers done")
				return
			case <-job.ctx.Done():
				log.Debug("job done")
				return
			case <-ticker.C:
				// Re-queue
				if err := w.submit(job); err != nil {
					log.Error("failed to submit",
						"error", err)
					return
				}
			}
		}
	}()
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
			if !job.execute() {
				job.Stop()
			}
		case <-w.ctx.Done():
			return
		}
	}
}

// stop stops workers and waits for them to finish.
func (w *Workers) stop() {
	w.cancel()  // Signal all workers to stop
	w.wg.Wait() // Wait for all workers to complete
}
