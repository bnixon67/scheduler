// Copyright (c) 2024 Bill Nixon

package scheduler

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"
)

// Scheduler owns a set of jobs and runs each one periodically.
//
// A job's runs are scheduled at fixed times: once when it is scheduled, or
// after the delay set with [WithStartDelay], then every interval after
// that. At most maxConcurrent jobs run at once; a job whose run is due
// while all slots are busy waits for one. A job never runs concurrently
// with itself.
//
// A run can start late, because the previous run took longer than the
// interval or because the job waited for a slot. When a run starts late,
// the scheduled times that passed in the meantime are skipped, not made up
// later. [Job.Skipped] counts them.
type Scheduler struct {
	mu      sync.Mutex
	jobs    map[string]*Job
	stopped bool

	slots chan struct{} // semaphore limiting concurrent runs

	minInterval time.Duration // 0 means no minimum
	maxJobs     int           // 0 means no limit

	// quit is canceled to stop jobs from starting new runs. It is not
	// linked to the jobs' contexts, so runs in progress are undisturbed;
	// stopAll cancels those.
	quit       context.Context
	cancelQuit context.CancelFunc

	wg     sync.WaitGroup // tracks every job goroutine
	logger *slog.Logger
}

// New creates a Scheduler that runs at most maxConcurrent jobs at the same
// time. Jobs waiting for a free slot are delayed, never dropped. Options
// such as [WithLogger] allow further customization.
//
// New returns [ErrInvalidMaxConcurrent] if maxConcurrent is not positive.
func New(maxConcurrent int, opts ...Option) (*Scheduler, error) {
	if maxConcurrent <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidMaxConcurrent, maxConcurrent)
	}

	s := &Scheduler{
		jobs:   make(map[string]*Job),
		slots:  make(chan struct{}, maxConcurrent),
		logger: slog.Default(),
	}
	s.quit, s.cancelQuit = context.WithCancel(context.Background())
	for _, opt := range opts {
		opt(s)
	}

	return s, nil
}

// Option configures optional settings for a Scheduler.
type Option func(*Scheduler)

// WithLogger sets a custom logger for the Scheduler. A nil logger means
// [slog.Default].
func WithLogger(l *slog.Logger) Option {
	return func(s *Scheduler) {
		if l == nil {
			l = slog.Default()
		}
		s.logger = l
	}
}

// WithMinInterval sets the shortest interval [Scheduler.Schedule]
// accepts; a shorter one returns [ErrIntervalTooShort]. If d is 0 or
// negative, there is no minimum, which is the default.
//
// Set a minimum when intervals come from untrusted input, such as users
// or configuration. A very short interval, even 1ns, is otherwise valid,
// and its job then runs almost continuously, using CPU and often a
// concurrency slot.
func WithMinInterval(d time.Duration) Option {
	return func(s *Scheduler) { s.minInterval = max(d, 0) }
}

// WithMaxJobs limits how many jobs can be scheduled at once; beyond that,
// [Scheduler.Schedule] returns [ErrTooManyJobs]. A job stops counting
// toward the limit as soon as it stops, as for [Scheduler.JobNames]. If n
// is 0 or negative, there is no limit, which is the default.
//
// Set a limit when untrusted input decides what is scheduled. Each job
// holds a goroutine and a timer until it stops, and the maxConcurrent
// limit passed to [New] bounds only how many run at once, not how many
// are scheduled.
//
// A job stopped while a run is in progress stops counting before that run
// returns, so if runs ignore their context, more goroutines than the
// limit can briefly exist.
func WithMaxJobs(n int) Option {
	return func(s *Scheduler) { s.maxJobs = max(n, 0) }
}

// JobNames returns the names of the scheduled jobs in sorted order. A job
// that has finished, for any reason, is no longer listed.
func (s *Scheduler) JobNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Sorted(maps.Keys(s.jobs))
}

// Jobs returns the scheduled jobs, sorted by name like [Scheduler.JobNames].
// They are read together, so unlike calling [Scheduler.Job] for each name,
// no job can stop between the listing and the lookup. A job can still stop
// after Jobs returns; its *Job stays valid and reports its final state.
//
// A job that has finished, for any reason, is no longer listed. To report
// on jobs after they stop, keep the *Job that [Scheduler.Schedule] returns.
func (s *Scheduler) Jobs() []*Job {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.SortedFunc(maps.Values(s.jobs), func(a, b *Job) int {
		return cmp.Compare(a.name, b.name)
	})
}

// Job returns the scheduled job with the given name. ok is false if no
// scheduled job has that name.
func (s *Scheduler) Job(name string) (job *Job, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok = s.jobs[name]
	return job, ok
}

// Schedule creates a job that calls run immediately, or after the delay
// set with [WithStartDelay], then once per interval, and returns it. The
// name must be unique among the jobs currently scheduled; it can be reused
// once its job stops. What run returns decides whether the job keeps
// running; see [RunFunc]. Optional settings are applied using [JobOption]
// values.
//
// Schedule returns [ErrEmptyName] if name is empty, [ErrInvalidInterval]
// if interval is not positive, [ErrNilRun] if run is nil,
// [ErrIntervalTooShort] if interval is below the minimum set with
// [WithMinInterval], [ErrSchedulerStopped] after [Scheduler.Stop] or
// [Scheduler.Shutdown], [ErrJobNameExists] if a job that has not yet
// stopped already uses the name, and [ErrTooManyJobs] if the limit set
// with [WithMaxJobs] has been reached.
func (s *Scheduler) Schedule(name string, interval time.Duration,
	run RunFunc, opts ...JobOption,
) (*Job, error) {
	job, err := newJob(name, interval, run, s.logger, opts...)
	if err != nil {
		return nil, err
	}
	if interval < s.minInterval {
		job.cancel()
		return nil, fmt.Errorf("%w: %v < %v",
			ErrIntervalTooShort, interval, s.minInterval)
	}

	job.sched = s

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.canAdd(name); err != nil {
		job.cancel()
		return nil, err
	}

	s.jobs[name] = job

	// wg.Add happens under mu, so it cannot race with Stop's wg.Wait.
	s.wg.Add(1)
	go s.runJob(job)

	return job, nil
}

// Stop stops every job and waits for runs in progress to return. Each
// run's context is canceled, so a run that watches it can return early.
// To let runs finish without canceling them, use [Scheduler.Shutdown].
//
// After Stop, [Scheduler.Schedule] returns [ErrSchedulerStopped]. Stop is
// safe to call more than once, and after or during Shutdown.
func (s *Scheduler) Stop() {
	s.markStopped()
	s.cancelQuit()
	s.stopAll()
	s.wg.Wait()
}

// Shutdown stops the scheduler gracefully. No new runs start, and runs in
// progress continue with their context intact. Shutdown returns nil once
// they all return.
//
// If ctx is done first, Shutdown cancels the context of every run still in
// progress and returns ctx.Err() without waiting further, since a run that
// ignores its context could block forever. Call [Scheduler.Stop] afterward
// to wait for those runs to return.
//
// After Shutdown, [Scheduler.Schedule] returns [ErrSchedulerStopped].
// Shutdown is safe to call more than once and together with Stop.
func (s *Scheduler) Shutdown(ctx context.Context) error {
	s.markStopped()
	s.cancelQuit()

	// sync.WaitGroup cannot be used in a select, so wait in a goroutine.
	// If ctx wins, this goroutine exits when the remaining runs return.
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.stopAll()
		return ctx.Err()
	}
}

// canAdd reports why a job named name cannot be added now, or nil if it
// can. s.mu must be held.
func (s *Scheduler) canAdd(name string) error {
	switch {
	case s.stopped:
		return ErrSchedulerStopped
	case s.jobs[name] != nil:
		return fmt.Errorf("%w: %q", ErrJobNameExists, name)
	case s.maxJobs > 0 && len(s.jobs) >= s.maxJobs:
		return fmt.Errorf("%w: limit is %d", ErrTooManyJobs, s.maxJobs)
	}
	return nil
}

// markStopped makes later calls to Schedule fail.
func (s *Scheduler) markStopped() {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
}

// stopAll stops every registered job before returning. Call it after
// markStopped, so no job can be scheduled after the snapshot. A job
// missing from the snapshot has already stopped.
func (s *Scheduler) stopAll() {
	s.mu.Lock()
	jobs := slices.Collect(maps.Values(s.jobs))
	s.mu.Unlock()

	for _, job := range jobs {
		job.Stop()
	}
}

// runJob drives one scheduled job until it finishes. However it finishes,
// the job is stopped, which removes it from the registry.
func (s *Scheduler) runJob(job *Job) {
	defer s.wg.Done()
	defer job.Stop()

	due := time.Now() // when the next run should start

	// Wait for the first run. The ticker is created afterward, so later
	// runs fall every interval after the first one, not after scheduling.
	if job.startDelay > 0 {
		timer := time.NewTimer(job.startDelay)
		defer timer.Stop()
		select {
		case due = <-timer.C:
		case <-job.ctx.Done():
			return
		case <-s.quit.Done():
			return
		}
	}

	ticker := time.NewTicker(job.interval)
	defer ticker.Stop()

	for {
		// Wait for a free run slot.
		select {
		case s.slots <- struct{}{}:
		case <-job.ctx.Done():
			return
		case <-s.quit.Done():
			return
		}

		// Both cases can be ready at once, and select picks randomly,
		// so check again before running.
		if s.quit.Err() != nil || job.Stopped() {
			<-s.slots
			return
		}

		job.recordLateness(time.Since(due))

		again := job.execute()
		<-s.slots

		if !again {
			return
		}

		// Wait for the next scheduled time. time.Ticker keeps one tick
		// that fires during a run and drops the rest, which is what
		// skips missed runs. The kept tick's value is when it fired, so
		// lateness covers both a slow previous run and waiting for a slot.
		select {
		case due = <-ticker.C:
		case <-job.ctx.Done():
			return
		case <-s.quit.Done():
			return
		}
	}
}

// remove deletes job from the registry. It is called once, from
// [Job.Stop]; the check guards against deleting a different job that now
// uses the name.
func (s *Scheduler) remove(job *Job) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.jobs[job.name] == job {
		delete(s.jobs, job.name)
	}
}
