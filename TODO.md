General
- [ ] Create an errors file?
- [ ] Move logger to Scheduler to avoid package global
- [ ] Split out test functions from scheduler_test.go

Scheduler
- [ ] Don't delete from map in StopJob to allow restart?
- [ ] Consider allowing job restart or should it be Stop and then Add?

Job
- [ ] NewJob - allow user to provide ID
- [ ] Update String() - stopped: true/false instead of status: stopped/running
- [ ] allow run to return bool to stop or continue
- [ ] Stop() - update comment to indicate it closes the stopCh
- [ ] ID() - fix function comment
