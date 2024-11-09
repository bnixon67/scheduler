// Copyright (c) 2024 Bill Nixon

package scheduler

import "errors"

var ErrJobIDExists = errors.New("job ID already exists")
var ErrJobIsNil = errors.New("job is nil")
var ErrWorkersStopping = errors.New("workers are stopping")
