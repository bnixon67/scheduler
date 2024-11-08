// Copyright (c) 2024 Bill Nixon

package scheduler

import "errors"

var ErrJobIDExists = errors.New("job ID already exists")
var ErrWorkersStopping = errors.New("workers are stopping")
