General
- [ ] Move logger to Scheduler to avoid package global

Scheduler
- [ ] Provide a GracefulStop method on Scheduler to allow active jobs to complete within a specified timeout, avoiding abrupt termination.

Job
- [ ] Update String() - stopped: true/false instead of status: stopped/running
- [ ] allow run to return bool to stop or continue the job
- [ ] Stop() - update comment to indicate it closes the stopCh
- [ ] In Job.start, the error from submit(job) is logged, but it may benefit from more detailed handling (e.g., retries or backoff if a queue is full) instead of silently ignoring issues.
- [ ] Add a interation limit to Job
- [ ] Adding a context.Context parameter to Job.run allows each job to handle cancellation signals, deadlines, and other context-related information, making it easier to stop jobs gracefully.
- [ ] Add job states (e.g., Pending, Running, Completed, Failed) to track and query job statuses
- [ ] Allow jobs to adjust their interval dynamically
- [ ] Implement a priority system for jobs, allowing high-priority jobs to be executed before others.
- [ ] add a maximum execution time for each job and an optional onTimeout function 

Workers
- [ ] Enhance comment for executeJob to not that it handle panics and won’t halt workers
