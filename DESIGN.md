# Design

This document explains how the `scheduler` package works and why. For how
to use it, see the package documentation (`go doc -all`).

## Goals

- Run named jobs at a fixed interval, with a limit on how many run at once.
- Never run a job concurrently with itself.
- Never let one job's failure or panic affect the program or other jobs.
- Stop cleanly: either let runs finish or cancel them, and always wait for
  them to return.
- Stay small: no dependencies beyond the standard library, and no I/O of
  its own.

## Non-goals

- Cron expressions or calendar schedules. Only fixed intervals.
- Persistence. Jobs exist only while the process runs.
- Catching up. Missed runs are skipped, never replayed (see
  [Timing](#timing)).
- Priorities or fairness between jobs. Priority only matters when every
  slot is busy, and it would replace the semaphore with a priority queue
  that can starve low-priority jobs. To guarantee capacity for some jobs,
  give them their own `Scheduler`.
- Changing a job's interval. A job's settings never change after it is
  scheduled. To change one, stop the job and schedule a new one; the name
  can be reused at once.
- Forcing a run to stop. Go cannot kill a goroutine, so stopping and
  timeouts cancel a run's context and rely on the run to return.

## Vocabulary

- A **job** is the long-lived, named thing that is scheduled. It has an
  interval, a `RunFunc`, options and a lifecycle.
- A **run** is one call of a job's `RunFunc`.

The API follows this split: `Job.Runs`, `WithMaxRuns`, `RunFunc`.

## Why RunFunc takes no arguments

`RunFunc` is `func(ctx context.Context, job *Job) error`. A run gets its
data by capturing it in a closure or by being a method value, such as
`s.Schedule(name, d, c.run)`, as `http.HandlerFunc` and
`errgroup.Group.Go` callbacks do. The alternatives were rejected:

- **An `args ...any` parameter or an `any` field on the job.** Every run
  would have to type-assert its arguments, and mistakes would show up only
  at run time. A closure gets the same data, checked by the compiler.
- **A generic `Schedule[T]` taking `func(ctx, *Job, T) error`.** Go
  methods cannot have type parameters, so this needs a package-level
  function or a generic `Job[T]`, which spreads through `Scheduler.Job`
  and the rest of the API, for nothing a closure cannot already do.
- **A `Runner` interface, as `http.Handler` is to `HandlerFunc`.** A method
  value already works as a `RunFunc`, so the interface would add a type
  without letting callers do anything new.

The two parameters are what a closure cannot supply. `ctx` comes from the
scheduler: it is canceled when the job stops, and with `WithRunTimeout`
each run gets its own. `job` is the run's own `*Job`, which a closure
cannot safely capture: `Schedule` starts the first run before it returns
the `*Job`, so a captured variable may still be nil. With the parameter,
a run can call `job.Stop`, log `job`, or read `job.Runs` without that
race.

## Structure

```
Scheduler
├── jobs    map[name]*Job      registry of jobs that have not stopped
├── slots   chan struct{}      semaphore, capacity maxConcurrent
├── quit    context            canceled by Stop and Shutdown
└── wg      WaitGroup          one count per job goroutine

Job
├── fixed:   name, interval, run, options
├── counts:  runs, skipped     (atomic)
├── status:  running, lastRun, lastErr   (one mutex, read together)
└── ctx      context           passed to every run, canceled by Job.Stop
```

`Schedule` registers the job and starts one goroutine for it, `runJob`,
which loops until the job finishes:

1. `acquireSlot`: wait for a free slot by sending on `slots`, or return
   if the job or scheduler is stopped. Then check again for a stop,
   because `select` picks randomly when several cases are ready.
2. Record how late the run is, then `execute` the run, which calls the
   `RunFunc` and decides whether the job runs again.
3. `releaseSlot`, and return if the job should not run again.
4. `waitFor` the next tick, or return if stopped.

Before the loop, `waitFor` also waits out the start delay, if any.

However `runJob` returns, it calls `Job.Stop`, which removes the job from
the registry.

### Why a goroutine per job

A goroutine per job plus a semaphore is simpler than a pool of workers
fed by a queue, and gives the guarantees directly:

- **No overlap.** A job's runs happen one after another in its own
  goroutine, so it cannot run concurrently with itself, with no extra
  bookkeeping.
- **Delayed, never dropped.** A job waiting for a slot blocks on the
  semaphore. There is no queue that can fill up and no submit error to
  handle.
- **Simple stop.** Each goroutine watches its job's context and the
  scheduler's `quit` context, and the `WaitGroup` tracks them all.

The cost is one goroutine and one timer per scheduled job, even while it is
idle. `WithMaxJobs` bounds that when untrusted input decides what is
scheduled.

Measured on Go 1.27 with jobs whose run does nothing, an idle job takes
about 6 KB: 4 KB of goroutine stack and 2 KB of heap for the `Job`, its
contexts, its ticker and its registry entry. That is about 60 MB for
10,000 jobs and 600 MB for 100,000. Idle jobs use no CPU, since each is
blocked in a `select` and the runtime's timer heap wakes only jobs that
are due. Runs happen on the job's goroutine, so a run that needs a deep
stack grows it, until the garbage collector shrinks it again.

Memory is rarely the first limit. Only `maxConcurrent` runs happen at
once, so jobs keep to their schedule only if

```
jobs × average run time ÷ maxConcurrent < interval
```

For example, 10,000 jobs that each take 200 ms, with `maxConcurrent` 5
and a one-minute interval, need 400 s per pass: runs start late and
`Job.Skipped` grows, long before memory matters. The work itself, such as
open connections or file descriptors, often runs out before that too.

A goroutine per job suits up to about 100,000 jobs. Far beyond that, a
single timer heap feeding a pool of workers would take a few hundred bytes
per job, at the cost of the simplicity above.

The semaphore makes no promise about which waiting job gets the next slot.

## Timing

A job runs once as soon as it is scheduled, then on a fixed grid: one
interval after scheduling, two intervals after, and so on. A run's start
time does not move the grid, so slow runs don't make the schedule drift.

`WithStartDelay` moves the first run, and with it the whole grid, later
by its delay. `runJob` waits on a timer before creating the ticker, so
the grid starts at the first run rather than at scheduling. The wait
holds no slot and ends at once if the job or scheduler stops. Since each
job keeps its own grid, jobs given different delays stay apart for good,
which is how to stop jobs that share an interval from all running at the
same moment. The package picks no delay itself: callers choose random
(`rand.N(interval)`), even or name-based offsets as they need.

The grid comes from a `time.Ticker`. A ticker keeps at most one pending
tick and drops the rest, which is what makes the scheduler **skip** missed
runs instead of making them up:

- If a run takes longer than the interval, or the job waits for a slot,
  the next run starts late, as soon as it can.
- The times that passed in the meantime are skipped. The value of the kept
  tick is when it fired, so `time.Since(due)` measures the whole delay,
  from both a slow previous run and waiting for a slot.
- `Job.Skipped` adds the number of whole intervals each run was late by,
  and a debug log message records it.

Skipping fits periodic work, such as polling or cleanup, where running
several times in a row to catch up would be wasted work or would overload
whatever is behind the job.

`WithMaxRuns` stops the job right after its last allowed run, not one
interval later.

## Contexts and stopping

Each job has its own context, created from `context.Background()` and
passed to every run. It is canceled only by `Job.Stop`. It is deliberately
**not** derived from the scheduler's `quit` context, so that `Shutdown` can
stop new runs from starting without canceling runs in progress.

With `WithRunTimeout`, each run instead gets a child of the job's context
with that timeout, so stopping the job still cancels it. A run that
returns because its time is up reports `context.DeadlineExceeded` to the
error handler like any other error, so no separate timeout callback is
needed, and `WithStopOnError` applies to it too. The timeout is
cooperative: a run that ignores its context keeps its slot until it
returns.

| Call | New runs | Runs in progress | Waits |
| --- | --- | --- | --- |
| `Job.Stop` | none for that job | context canceled, allowed to return | no |
| `Scheduler.Shutdown(ctx)` | none | keep running with their context intact | until they return, or `ctx` is done |
| `Scheduler.Stop` | none | context canceled | until they return |

- `Shutdown` cancels `quit`, then waits on the `WaitGroup`. If its `ctx` is
  done first, it stops every job, which cancels their contexts, and
  returns `ctx.Err()` without waiting further, since a run that ignores
  its context could block forever. Calling `Stop` afterward waits for
  those runs.
- `Stop` cancels `quit`, stops every job and waits.
- All three are safe to call more than once and in any combination.
  `Job.Stop` uses a `sync.Once`, and `Stopped` reads the context, so a
  stopped job never restarts.
- After either scheduler call, `Schedule` returns `ErrSchedulerStopped`.
  The `stopped` flag is set under the mutex before jobs are collected, so
  no job can be added after the list of jobs to stop is taken.
- `wg.Go` adds each job goroutine under the same mutex, so it cannot race
  with `wg.Wait`.

### Runs that ignore cancellation

Since cancellation only works if runs cooperate, the package documents
the rules a `RunFunc` must follow in one place, the "Writing a RunFunc"
section of `doc.go`, and detects runs that break them.

For each run, `call` registers a `context.AfterFunc` on the run's
context. If the context is canceled, by any of the calls above or a
run timeout, a timer starts. If the run has not returned when it fires, after
`WithCancelGrace` (5s by default), a warning names the job and the reason
for the cancellation, and a second warning says how long the run took to
return. A run that returns before its context is canceled removes the
watch, so well-behaved runs cost one `AfterFunc` registration and nothing
else.

The function the run returns through waits for the watcher to finish, so
both warnings are written before the job's goroutine exits, and never
after `Stop` returns.

### Registry and name reuse

A job is removed from the registry as soon as it stops, for any reason,
even while its last run is still in progress. Its name can be reused at
once, so the old job's goroutine and a new job with the same name can
briefly exist together. `remove` checks that the registered job is the
same one before deleting it, so the old job cannot remove the new one.

`WithMaxJobs` counts the registry, so a job stopped during a run that
ignores its context stops counting before its goroutine exits.

## Errors

A `RunFunc` returns:

- `nil` to keep running.
- `ErrStop`, or an error wrapping it, to stop without reporting an error.
- Any other error, which goes to the job's error handler. The job keeps
  running unless it has `WithStopOnError(true)`.

A panic in a run is recovered and turned into a `*PanicError` holding the
panic value and stack trace, then handled like any other error. One
failing job therefore cannot crash the program. Recovering can leave
shared state half updated, which is why `WithStopOnError` exists.

The error handler is caller code running on the job's goroutine too, so a
panic in it is also recovered and logged, and the job continues as if the
handler had returned. That log has the handler's panic value and stack
but not the run's error, because a custom handler may exist to redact it.

An error that only says the job's context was canceled, returned after the
job stopped, is not a failure and is not reported. That is what a
well-behaved run returns during `Stop`.

`New` and `Schedule` return errors for bad arguments instead of panicking,
because names and intervals may come from configuration or users. Errors
about a specific job wrap a sentinel with details, so callers compare with
`errors.Is`.

## Logging

The scheduler logs through `log/slog`, using `slog.Default()` unless
`WithLogger` is given:

- **Error:** failed and panicked runs, from the default error handler.
- **Debug:** late runs, jobs stopping and reaching their maximum runs.

`Job` implements `slog.LogValuer`, so it logs as a group of fields
(`job.name`, `job.interval`, `job.runTimeout`, `job.runs`, …) rather
than one string, and the fields are only read when a record is written. Durations are logged as
strings such as `10s`, because `slog.JSONHandler` would otherwise write
them as integer nanoseconds.

## Security

The package does no network, file or process I/O, but the program around
it may schedule work based on untrusted input. The relevant controls are
documented in `doc.go` and the README:

- `WithMinInterval` stops a tiny interval from making a job run almost
  continuously.
- `WithMaxJobs` bounds goroutines and timers.
- Job names have no length limit, so callers should check untrusted ones.
- The default error handler logs errors, panic values and stacks in full,
  which may contain secrets. `WithErrorHandler` can redact them.
- `Job.String` quotes the name, so a name with newlines cannot forge extra
  lines in plain-text logs.

## Testing

Timing-dependent tests use `testing/synctest`, which runs them on a fake
clock, so they are fast and deterministic rather than relying on real
sleeps. The runnable examples in `example_test.go` can't use it, so they
use short real intervals.
Tests are split by topic: lifecycle, limits, shutdown, skipped runs, and
the job and scheduler basics. Run them with the race detector:

```
go test -race ./...
```
