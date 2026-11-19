Scheduler
- [x] Provide a graceful stop (Shutdown) on Scheduler to allow active jobs to complete within a specified timeout, avoiding abrupt termination.

Job
- [x] Pass a context.Context to the run function, canceled when the job or scheduler stops.
- [x] Let the run function return an error, handled by an error handler.
- [ ] Add job states (e.g., Pending, Running, Completed, Failed) to track and query job statuses
- [ ] Allow jobs to adjust their interval dynamically
- [ ] Implement a priority system for jobs, allowing high-priority jobs to be executed before others.
- [ ] add a maximum execution time for each job and an optional onTimeout function 
