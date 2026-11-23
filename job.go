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
// or when the run's time is up if the job has [WithRunTimeout]. A run must
// watch ctx and return soon after it is canceled; see the package
// documentation's section "Writing a RunFunc". job is the Job being run;
// use it rather than capturing the result of [Scheduler.Schedule], which
// may not be set yet when the first run starts.
//
// To pass data to a run, capture it in a closure, or use a method value
// such as c.run, where c's type has a method with RunFunc's signature.
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
	runTimeout  time.Duration // 0 means no timeout
	cancelGrace time.Duration // 0 means no warning
	startDelay  time.Duration // 0 means run as soon as scheduled
	logger      *slog.Logger
	sched       *Scheduler // nil only in tests that run a job directly

	runs    atomic.Int64
	skipped atomic.Int64 // whole intervals runs started late by

	// status describes the latest run. It is guarded by a mutex rather
	// than kept in atomics so its fields are always read together.
	statusMu sync.Mutex
	status   runStatus

	// ctx is passed to each run and canceled when the job stops. It is
	// stored because it spans the job's whole life, like the context of
	// an http.Request.
	ctx      context.Context
	cancel   context.CancelFunc
	stopOnce sync.Once
}

// runStatus is what [Job.Running], [Job.LastRun] and [Job.LastError]
// report.
type runStatus struct {
	running bool
	lastRun time.Time // start of the latest run; zero before the first
	lastErr error     // error reported for the latest finished run
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
//
// The handler runs on the job's goroutine while the run's concurrency
// slot is still held, so a slow handler delays other jobs too. If it
// panics, the panic is recovered and logged with the handler's
// panic value and stack trace, but not the run's error, and the job then
// continues as if the handler had returned.
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

// WithRunTimeout limits how long each run may take. The ctx passed to the
// [RunFunc] is canceled d after the run starts. A run that returns
// because of it, with ctx.Err() or an error wrapping it, has that error
// passed to the error handler, where errors.Is(err,
// [context.DeadlineExceeded]) reports true. The job keeps running
// unless it was scheduled [WithStopOnError](true). If d is 0 or negative,
// runs have no time limit, which is the default.
//
// The timeout is cooperative: Go cannot stop a goroutine from outside, so
// a run that ignores ctx keeps running, and keeps its concurrency slot,
// until it returns.
func WithRunTimeout(d time.Duration) JobOption {
	return func(j *Job) { j.runTimeout = max(d, 0) }
}

// WithStartDelay delays a job's first run by d. Later runs follow every
// interval after the first, so d shifts the job's whole schedule. If d is
// 0 or negative, the job first runs as soon as it is scheduled, which is
// the default.
//
// Pass the interval to wait one full interval before the first run. To
// spread jobs that share an interval, so they do not all run at the same
// moment, give each a different delay, such as rand.N(interval) from
// math/rand/v2. The offsets last, because each job's later runs keep to
// its own schedule.
//
// The job holds no concurrency slot while it waits, and [Job.Stop],
// [Scheduler.Stop] and [Scheduler.Shutdown] end the wait at once.
func WithStartDelay(d time.Duration) JobOption {
	return func(j *Job) { j.startDelay = max(d, 0) }
}

// DefaultCancelGrace is how long a run may keep going after its context
// is canceled before the scheduler logs a warning; see [WithCancelGrace].
const DefaultCancelGrace = 5 * time.Second

// WithCancelGrace sets how long a run may keep going after its context is
// canceled, by [Job.Stop], [Scheduler.Stop], a [Scheduler.Shutdown]
// deadline or [WithRunTimeout], before the scheduler logs a warning that
// it ignored the cancellation. When such a run does return, a second
// warning says how long it took. The default is [DefaultCancelGrace]. If
// d is 0 or negative, no warning is logged.
//
// The warning does not stop the run, which Go cannot do from outside; it
// shows which job breaks the rules in the package documentation's section
// "Writing a RunFunc". Raise d for a job that needs time to clean up after
// being canceled.
func WithCancelGrace(d time.Duration) JobOption {
	return func(j *Job) { j.cancelGrace = max(d, 0) }
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
		name:        name,
		interval:    interval,
		run:         run,
		onError:     defaultErrorHandler,
		cancelGrace: DefaultCancelGrace,
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
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
			"runTimeout: %s, "+
			"startDelay: %s, "+
			"runs: %d, "+
			"skipped: %d, "+
			"stopped: %t}",
		j.name,
		j.interval,
		j.stopOnError,
		j.maxRuns,
		j.runTimeout,
		j.startDelay,
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
		slog.String("runTimeout", j.runTimeout.String()),
		slog.String("startDelay", j.startDelay.String()),
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

// Running reports whether a run of the job is in progress.
func (j *Job) Running() bool {
	j.statusMu.Lock()
	defer j.statusMu.Unlock()
	return j.status.running
}

// LastRun returns when the job's latest run started, including a run in
// progress. It returns the zero time if the job has not run yet.
func (j *Job) LastRun() time.Time {
	j.statusMu.Lock()
	defer j.statusMu.Unlock()
	return j.status.lastRun
}

// LastError returns the error from the job's latest finished run: the
// error passed to its error handler, which is a [*PanicError] if the run
// panicked. It returns nil if that run succeeded, if it returned
// [ErrStop], if it only reported that its context was canceled after the
// job stopped, or if no run has finished yet. While a run is in progress,
// it reports the run before.
func (j *Job) LastError() error {
	j.statusMu.Lock()
	defer j.statusMu.Unlock()
	return j.status.lastErr
}

// Context returns the job's context, which is canceled when the job stops,
// including when a [Scheduler.Shutdown] deadline passes. Each run gets this
// context, or with [WithRunTimeout] a child of it with that timeout.
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
func (j *Job) execute() (again bool) {
	if j.hasReachedMaxRuns() {
		j.logger.Debug("max runs reached", "job", j)
		return false
	}

	j.markRunning()
	err := j.call()
	stop, failed := j.classify(err)

	// Before the error handler, so the handler sees this run's result.
	j.markFinished(failed, err)

	switch {
	case stop:
		return false
	case failed:
		j.handleError(err)
		if j.stopOnError {
			return false
		}
	}

	// Stop right after the last allowed run instead of one interval later.
	return !j.hasReachedMaxRuns()
}

// markRunning records that a run is starting.
func (j *Job) markRunning() {
	j.runs.Add(1)

	j.statusMu.Lock()
	defer j.statusMu.Unlock()
	j.status.running = true
	j.status.lastRun = time.Now()
}

// classify reports whether a run's error asks the job to stop, and
// whether it is a failure to report. ErrStop is a request, not a failure,
// and neither is a run that returns ctx.Err() after the job was stopped.
func (j *Job) classify(err error) (stop, failed bool) {
	stop = errors.Is(err, ErrStop)
	canceled := j.Stopped() && errors.Is(err, context.Canceled)
	return stop, err != nil && !stop && !canceled
}

// markFinished records that a run has returned, and its error if it
// failed.
func (j *Job) markFinished(failed bool, err error) {
	j.statusMu.Lock()
	defer j.statusMu.Unlock()
	j.status.running = false
	j.status.lastErr = nil
	if failed {
		j.status.lastErr = err
	}
}

// call runs the job's function, recovering a panic as a *PanicError so a
// failing job cannot crash the program.
func (j *Job) call() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &PanicError{Value: r, Stack: debug.Stack()}
		}
	}()

	ctx := j.ctx
	if j.runTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, j.runTimeout)
		defer cancel()
	}

	// Deferred after cancel, so it runs first: the watch is removed before
	// cancel runs, and a run that returns before its timeout never starts
	// the watcher.
	if j.cancelGrace > 0 {
		defer j.watchCancel(ctx)()
	}

	return j.run(ctx, j)
}

// watchCancel logs a warning if the run using ctx has not returned
// cancelGrace after ctx is canceled, and another when it then returns.
// Call the returned function when the run returns; it waits for any
// warning to be logged, so none is written after the job's goroutine
// exits.
func (j *Job) watchCancel(ctx context.Context) (returned func()) {
	done := make(chan struct{})     // closed when the run returns
	finished := make(chan struct{}) // closed when the watcher exits
	stop := context.AfterFunc(ctx, func() {
		defer close(finished)

		canceled := time.Now()
		t := time.NewTimer(j.cancelGrace)
		defer t.Stop()

		select {
		case <-t.C:
			j.logger.Warn("run ignored cancellation", "job", j,
				"reason", ctx.Err(), "after", j.cancelGrace.String())
		case <-done:
			return
		}

		// Say when it finally returns, so the log shows how long it held
		// its slot.
		<-done
		j.logger.Warn("run returned late after cancellation", "job", j,
			"after", time.Since(canceled).String())
	})

	return func() {
		if stop() {
			return // ctx was not canceled during the run
		}
		close(done)
		<-finished
	}
}

// handleError passes a run's error to the job's error handler. The handler
// is caller code running on the job's goroutine, so a panic in it is
// recovered and logged, like a panic in a run, instead of crashing the
// program. The log leaves out err, which the handler may exist to redact.
func (j *Job) handleError(err error) {
	defer func() {
		if r := recover(); r != nil {
			j.logger.Error("job error handler panicked",
				"job", j, "value", r, "stack", string(debug.Stack()))
		}
	}()
	j.onError(j, err)
}

// defaultErrorHandler logs a run's error. A panic is logged with its stack
// trace.
func defaultErrorHandler(j *Job, err error) {
	if pe, ok := errors.AsType[*PanicError](err); ok {
		j.logger.Error("job panicked",
			"job", j, "value", pe.Value, "stack", string(pe.Stack))
		return
	}
	j.logger.Error("job failed", "job", j, "err", err)
}
