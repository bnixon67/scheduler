// Copyright (c) 2024 Bill Nixon

// Package scheduler runs jobs periodically, with a limit on how many run
// at the same time.
//
// Create a [Scheduler] with [New], then call [Scheduler.Schedule] with a
// name, an interval and a [RunFunc]. Each job is driven by its own
// goroutine and never runs concurrently with itself. A run that starts
// late skips the scheduled times it missed instead of making them up.
//
// A RunFunc returns nil to keep running, [ErrStop] to stop, or any other
// error to report a failure to the job's error handler. Panics are
// recovered and reported as a [*PanicError].
//
// Stop a single job with [Job.Stop]. Stop the whole Scheduler with
// [Scheduler.Shutdown], which lets runs in progress finish, or with
// [Scheduler.Stop], which cancels them.
//
// # Security
//
// The package does no network, file or process I/O of its own, but a
// program using it should keep the following in mind.
//
// If untrusted input, such as users or configuration, decides what is
// scheduled, bound the resources it can claim. Use [WithMinInterval] so a
// tiny interval cannot make a job run almost continuously, and
// [WithMaxJobs] so the number of jobs, each holding a goroutine and a
// timer, cannot grow without limit. Job names have no length limit, so
// check the length of untrusted names before scheduling them.
//
// The default error handler logs each error in full, and for a panic the
// panic value and stack trace, which can contain secrets. Use
// [WithErrorHandler] to redact them, and never show a [*PanicError] to end
// users.
//
// A recovered panic can leave shared state inconsistent, and by default
// the job runs again anyway. Use [WithStopOnError] for jobs where that is
// unsafe.
//
// A run that ignores its context can outlive a [Scheduler.Shutdown]
// deadline. Watch ctx in long runs, and call [Scheduler.Stop] after
// Shutdown returns an error.
package scheduler
