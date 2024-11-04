# Scheduler Package

The `scheduler` package is a Go library designed for periodic recurring
job scheduling and execution using a pool of workers.  The package
provides an easy-to-use API to manage job lifecycles, handle concurrent
execution, and stop jobs gracefully when required.

---

## Features
- **Job Scheduling**: Schedule jobs to run at specified intervals.
- **Worker Pool Management**: Efficiently manage multiple workers to execute jobs concurrently.
- **Graceful Shutdown**: Ensure all workers stop cleanly without leaving incomplete tasks.
- **Thread-Safe Job Management**: Uses `sync.Map` for safe concurrent job access and modification.
- **Context-Based Cancellation**: Stop all running jobs and workers using Go's `context` package.
