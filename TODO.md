Scheduler
- [x] Provide a graceful stop (Shutdown) on Scheduler to allow active jobs to complete within a specified timeout, avoiding abrupt termination.

Job
- [x] Pass a context.Context to the run function, canceled when the job or scheduler stops.
- [x] Let the run function return an error, handled by an error handler.
- [x] Add a maximum time for each run (WithRunTimeout).
- [ ] Report what a job is doing and how its last run went: Running(), LastRun(), LastError(), and possibly Waiting() for a job blocked on a slot.
