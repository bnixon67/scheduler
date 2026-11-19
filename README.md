# Scheduler Package

The `scheduler` package is a Go library designed for periodic recurring
job scheduling and execution with a limit on how many jobs run at once.  The package
provides an easy-to-use API to manage job lifecycles, handle concurrent
execution, and stop jobs gracefully when required.

---

## Features
- **Job Scheduling**: Schedule jobs to run at specified intervals.
- **Bounded Concurrency**: At most `maxConcurrent` jobs run at once; jobs waiting for a slot are delayed, never dropped.
- **No Overlap**: A job never runs concurrently with itself. If a run starts late, because the previous run was slow or the job waited for a slot, the runs it missed are skipped, not made up later, and are counted in `Job.Skipped()`.
- **Error Handling**: A run returns `nil` to keep going, `scheduler.ErrStop` to stop, or an error to report it to the job's error handler. Panics are recovered and reported as a `*scheduler.PanicError`, so a failing job cannot crash the program.
- **Clean Lifecycle**: Jobs that finish for any reason, including `Job.Stop`, are removed from the scheduler at once, so their names can be reused.
- **Graceful Shutdown**: `Shutdown(ctx)` starts no new runs and lets runs in progress finish; if `ctx` is done first, it cancels them and returns `ctx.Err()`. `Stop` cancels runs in progress immediately and waits for them to return. Each run receives a `context.Context`, canceled when the job stops, so long runs can return early.
- **Run Timeouts**: `WithRunTimeout` cancels a run's context after a set time, and the run's error, which matches `context.DeadlineExceeded`, goes to the error handler.
- **Cancellation Checks**: A run must watch its context and return when it is canceled; the package documentation's "Writing a RunFunc" section lists the rules. A run that is still going `WithCancelGrace` (5s by default) after being canceled is logged as a warning, naming the job.
- **Job Status**: `Job.Running()`, `Job.LastRun()` and `Job.LastError()` report whether a run is in progress, when the latest one started, and the error from the latest finished run.
- **Resource Limits**: `WithMinInterval` rejects intervals that are too short and `WithMaxJobs` caps how many jobs can be scheduled, for programs where untrusted input decides what runs.

## Security Notes
- If users or configuration control what is scheduled, set `WithMinInterval` and `WithMaxJobs`, and limit the length of job names.
- The default error handler logs errors in full, and for a panic the panic value and stack trace, which can contain secrets. Use `WithErrorHandler` to redact them, and never show a `*PanicError` to end users.
- A recovered panic can leave shared state inconsistent, and by default the job runs again. Use `WithStopOnError(true)` where that is unsafe.

For usage, see the examples in the [package documentation](https://pkg.go.dev/github.com/bnixon67/scheduler), or run `go doc -all github.com/bnixon67/scheduler`. A demo program is in `cmd/schedulerdemo`. For how the package works internally and why, see [DESIGN.md](DESIGN.md).
