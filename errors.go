// Copyright (c) 2024 Bill Nixon

package scheduler

import (
	"errors"
	"fmt"
)

// ErrStop is returned by a [RunFunc] to stop its job without reporting an
// error. It may be wrapped.
var ErrStop = errors.New("scheduler: stop job")

// Errors returned by [Scheduler.Schedule]. An error about a specific job
// is wrapped with its name, so compare errors with [errors.Is].
var (
	// ErrJobNameExists is returned when a job that has not yet stopped
	// already uses the name.
	ErrJobNameExists = errors.New("scheduler: job name already exists")

	// ErrSchedulerStopped is returned after [Scheduler.Stop] or
	// [Scheduler.Shutdown].
	ErrSchedulerStopped = errors.New("scheduler: scheduler is stopped")

	// ErrIntervalTooShort is returned when a job's interval is shorter
	// than the Scheduler's minimum, set with [WithMinInterval].
	ErrIntervalTooShort = errors.New("scheduler: job interval is below the minimum")

	// ErrTooManyJobs is returned when the Scheduler already has the
	// maximum number of jobs, set with [WithMaxJobs].
	ErrTooManyJobs = errors.New("scheduler: too many jobs")
)

// Errors returned by [New] and [Scheduler.Schedule] for invalid arguments.
var (
	ErrEmptyName            = errors.New("scheduler: job name is empty")
	ErrInvalidInterval      = errors.New("scheduler: job interval must be positive")
	ErrNilRun               = errors.New("scheduler: job run function is nil")
	ErrInvalidMaxConcurrent = errors.New("scheduler: maxConcurrent must be positive")
)

// PanicError is the error passed to a job's error handler when its
// [RunFunc] panics.
//
// Value and Stack can contain sensitive data, such as arguments or
// secrets held by the run, and Stack reveals the program's internals. Log
// them where only operators can read them, and do not send them to end
// users, for example in an HTTP response.
type PanicError struct {
	Value any    // the value passed to panic
	Stack []byte // the panicking goroutine's stack trace
}

// Error returns the panic value as a message. It omits the stack trace.
func (e *PanicError) Error() string {
	return fmt.Sprintf("scheduler: job panicked: %v", e.Value)
}
