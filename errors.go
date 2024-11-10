// Copyright (c) 2024 Bill Nixon

package scheduler

import "errors"

var (
	ErrJobIDExists     = errors.New("job ID already exists")
	ErrJobIsNil        = errors.New("job is nil")
	ErrWorkersStopping = errors.New("workers are stopping")
	ErrJobQueueIsFull  = errors.New("job queue is full")
	ErrJobNotFound     = errors.New("job not found")
)
