// Copyright (c) 2024 Bill Nixon

package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// RunFunc is the function a [Job] runs. ctx is canceled when the job stops,
// so a long run should watch it and return early. job is the Job being
// run.
//
// Returning nil keeps the job running. Returning [ErrStop], or an error
// wrapping it, stops the job without reporting an error. Any other error is
// passed to the job's error handler (see [WithErrorHandler]), and the job
// keeps running unless it was scheduled [WithStopOnError](true). A panic is
// recovered and handled like an error, as a [*PanicError].
type RunFunc func(ctx context.Context, job *Job) error

// Job is a periodic task scheduled by [Scheduler.Schedule]. It has a name,
// an interval, a function to run and options, which never change, plus
// runtime state: run and skipped counts, and whether it has stopped.
// It is passed to its [RunFunc] and returned by [Scheduler.Schedule] and
// [Scheduler.Job].
//
// A job keeps running until one of these happens:
//   - It is stopped with [Job.Stop], [Scheduler.Stop] or
//     [Scheduler.Shutdown].
//   - It reaches its maximum run count, set with [WithMaxRuns].
//   - Its run function returns [ErrStop].
//   - Its run function returns an error or panics, and the job was
//     scheduled [WithStopOnError](true).
//
// A stopped Job is never restarted; schedule a new one, which may reuse
// the name.
type Job struct {
	// Set when the job is scheduled and never changed.
	name        string
	interval    time.Duration
	run         RunFunc
	onError     func(*Job, error)
	stopOnError bool
	maxRuns     int
	logger      *slog.Logger
	sched       *Scheduler // nil only in tests that run a job directly

	runs    atomic.Int64
	skipped atomic.Int64 // whole intervals runs started late by

	// ctx is passed to each run and canceled when the job stops. It is
	// stored because it spans the job's whole life, like the context of
	// an http.Request.
	ctx      context.Context
	cancel   context.CancelFunc
	stopOnce sync.Once
}

// JobOption configures optional Job settings.
type JobOption func(*Job)

// WithErrorHandler sets the function called when a run returns an error
// other than [ErrStop], or panics, in which case the error is a
// [*PanicError]. A nil handler means the default, which logs the error.
// To ignore errors, pass a function that does nothing.
//
// An error that only reports that the job's context was canceled, returned
// after the job stopped, is not passed to the handler.
//
// The default handler logs the whole error, and for a panic the panic
// value and stack trace, which can include secrets such as credentials or
// connection strings. If errors or panic values may hold sensitive data,
// supply a handler that redacts it before logging.
func WithErrorHandler(handler func(*Job, error)) JobOption {
	return func(j *Job) {
		if handler == nil {
			handler = defaultErrorHandler
		}
		j.onError = handler
	}
}

// WithMaxRuns limits the number of times a job will run. If n is 0
// or negative, there is no limit.
func WithMaxRuns(n int) JobOption {
	return func(j *Job) { j.maxRuns = n }
}

// WithStopOnError configures whether the job stops after a run returns an
// error or panics. By default it keeps running.
//
// A recovered panic can leave data the run was changing half updated, and
// later runs then work with that inconsistent state. Pass true for jobs
// where that is unsafe, such as those that touch security-sensitive state.
func WithStopOnError(b bool) JobOption {
	return func(j *Job) { j.stopOnError = b }
}

// newJob validates its arguments and returns a Job ready to run.
func newJob(name string, interval time.Duration, run RunFunc,
	logger *slog.Logger, opts ...JobOption,
) (*Job, error) {
	switch {
	case name == "":
		return nil, ErrEmptyName
	case interval <= 0:
		return nil, fmt.Errorf("%w: %v", ErrInvalidInterval, interval)
	case run == nil:
		return nil, ErrNilRun
	}

	ctx, cancel := context.WithCancel(context.Background())
	j := &Job{
		name:     name,
		interval: interval,
		run:      run,
		onError:  defaultErrorHandler,
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
	}
	for _, opt := range opts {
		opt(j)
	}

	return j, nil
}

// String returns a human-readable representation of the Job.
func (j *Job) String() string {
	return fmt.Sprintf(
		"Job{"+
			"name: %q, "+
			"interval: %s, "+
			"stopOnError: %t, "+
			"maxRuns: %d, "+
			"runs: %d, "+
			"skipped: %d, "+
			"stopped: %t}",
		j.name,
		j.interval,
		j.stopOnError,
		j.maxRuns,
		j.Runs(),
		j.Skipped(),
		j.Stopped(),
	)
}

// LogValue implements [slog.LogValuer], so a Job logs as structured
// fields. The fields are read only when a record is actually written, so
// disabled debug logging costs almost nothing.
func (j *Job) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("name", j.name),
		slog.String("interval", j.interval.String()),
		slog.Int("runs", j.Runs()),
		slog.Int("skipped", j.Skipped()),
		slog.Bool("stopped", j.Stopped()),
	)
}

// Name returns the job's name, which identifies it within its [Scheduler]
// until it stops.
func (j *Job) Name() string {
	return j.name
}

// Interval returns the time between scheduled runs.
func (j *Job) Interval() time.Duration {
	return j.interval
}

// Runs returns how many times the job has run, including runs that
// failed or panicked.
func (j *Job) Runs() int {
	return int(j.runs.Load())
}

// Skipped returns how many scheduled runs the job has missed. Each time a
// run starts late, the number of whole intervals it was late by is added.
// A run is late when the previous run took longer than the interval, or
// when it waited for a free slot because the [Scheduler] was saturated. A
// count that keeps growing means the job is not getting its intended
// schedule.
func (j *Job) Skipped() int {
	return int(j.skipped.Load())
}

// Context returns the context passed to each run, which is canceled when
// the job stops, including when a [Scheduler.Shutdown] deadline passes.
// Inside a [RunFunc], use the ctx argument instead; this method is for
// code outside the run function that needs to follow the job's lifetime.
func (j *Job) Context() context.Context {
	return j.ctx
}

// Stop prevents the job from running again and removes it from its
// [Scheduler], so its name can be reused at once. A run already in
// progress is allowed to finish; its context is canceled so it can return
// early. Stop is safe to call more than once, including from the job's
// own [RunFunc].
func (j *Job) Stop() {
	j.stopOnce.Do(func() {
		j.cancel()
		if j.sched != nil {
			j.sched.remove(j)
		}
		j.logger.Debug("stopped", "job", j)
	})
}

// Stopped reports whether the job will no longer run.
func (j *Job) Stopped() bool {
	return j.ctx.Err() != nil
}

// recordLateness adds to the skipped count when a run starts at least one
// whole interval after it was due.
func (j *Job) recordLateness(late time.Duration) {
	if late < j.interval {
		return
	}

	n := int64(late / j.interval)
	j.skipped.Add(n)
	j.logger.Debug("job ran late", "job", j, "late", late.String(), "skipped", n)
}

// hasReachedMaxRuns reports whether a limit is set (> 0) and the
// run count has reached it.
func (j *Job) hasReachedMaxRuns() bool {
	return j.maxRuns > 0 && j.Runs() >= j.maxRuns
}

// execute runs the job once and returns whether it should run again.
func (j *Job) execute() bool {
	if j.hasReachedMaxRuns() {
		j.logger.Debug("max runs reached", "job", j)
		return false
	}

	j.runs.Add(1)

	if err := j.call(); err != nil {
		if errors.Is(err, ErrStop) {
			return false
		}
		// A run that returns ctx.Err() after being stopped has not failed.
		canceled := j.Stopped() && errors.Is(err, context.Canceled)
		if !canceled {
			j.onError(j, err)
		}
		if j.stopOnError {
			return false
		}
	}

	// Stop right after the last allowed run instead of one interval later.
	return !j.hasReachedMaxRuns()
}

// call runs the job's function, recovering a panic as a *PanicError so a
// failing job cannot crash the program.
func (j *Job) call() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &PanicError{Value: r, Stack: debug.Stack()}
		}
	}()

	return j.run(j.ctx, j)
}

// defaultErrorHandler logs a run's error. A panic is logged with its stack
// trace.
func defaultErrorHandler(j *Job, err error) {
	var pe *PanicError
	if errors.As(err, &pe) {
		j.logger.Error("job panicked",
			"job", j, "value", pe.Value, "stack", string(pe.Stack))
		return
	}
	j.logger.Error("job failed", "job", j, "err", err)
}
