// Copyright (c) 2024 Bill Nixon

package scheduler

import "errors"

var (
	ErrJobIDExists     = errors.New("job ID already exists")
	ErrJobNotFound     = errors.New("job not found")
	ErrJobQueueFull    = errors.New("job queue full")
	ErrNilJob          = errors.New("job is nil")
	ErrWorkersStopping = errors.New("workers are stopping")
)
