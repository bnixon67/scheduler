package scheduler_test

import (
	"context"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bnixon67/scheduler"
)

// TestSchedulerJob verifies that retrieval of a job by ID from Scheduler works.
func TestSchedulerJob(t *testing.T) {
	s := scheduler.NewScheduler(1, 1)

	// Use t.Cleanup to ensure resources are cleaned up.
	t.Cleanup(s.Stop)

	jobID := "test"

	wantJob := scheduler.NewJob(
		jobID,
		1*time.Second,
		func(*scheduler.Job) bool { return true },
	)
	s.AddJob(wantJob)

	gotJob := s.GetJob(jobID)

	if gotJob != wantJob {
		t.Errorf("\ngot  %v,\nwant %v\nfor s.Job()",
			gotJob, wantJob)
	}

	// Verify non-existent job ID returns nil.
	gotJob = s.GetJob("not" + jobID)
	if gotJob != nil {
		t.Errorf("\ngot  %v,\nwant %v\nfor s.Job()",
			gotJob, nil)
	}
}

// TestSchedulerWithOneJob verifies the scheduler executes a job at least once.
func TestSchedulerWithOneJob(t *testing.T) {
	var executions atomic.Int32

	s := scheduler.NewScheduler(5, 2)

	// Use t.Cleanup to ensure resources are cleaned up
	t.Cleanup(s.Stop)

	// Add the job to the scheduler
	s.AddJob(
		scheduler.NewJob(
			"test",
			time.Second,
			func(*scheduler.Job) bool {
				executions.Add(1)
				return true
			},
		),
	)

	// Use a context with timeout to wait for job to finish.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	<-ctx.Done() // Wait for the context to expire

	// Check if the job was executedi at least once
	got := executions.Load()
	if got < 1 {
		t.Errorf("got %d executions, want at least 1", got)
	}

	s.Stop()
}

// TestSchedulerWithMultipleJobs verifies that the scheduler executes
// multiple jobs.
func TestSchedulerWithMultipleJobs(t *testing.T) {
	var mu sync.Mutex
	executions := map[string]int{}

	runFunc := func(job *scheduler.Job) bool {
		mu.Lock()
		executions[job.ID()]++
		mu.Unlock()
		return true
	}

	jobs := []*scheduler.Job{
		scheduler.NewJob("test 1", 1*time.Second, runFunc),
		scheduler.NewJob("test 2", 2*time.Second, runFunc),
		scheduler.NewJob("test 3", 2*time.Second, runFunc),
	}

	s := scheduler.NewScheduler(len(jobs), len(jobs))

	// Use t.Cleanup to ensure resources are cleaned up
	t.Cleanup(s.Stop)

	for n := range jobs {
		s.AddJob(jobs[n])
	}

	// Use a context with timeout to ensure the test doesn't run too long
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	<-ctx.Done() // Wait for the context to expire

	// Check if each job was executed at least once
	mu.Lock()
	defer mu.Unlock()
	for n := range jobs {
		got := executions[jobs[n].ID()]
		if got < 1 {
			t.Errorf("got %d executions, want at least 1 for %s",
				got, jobs[n].ID())
		}
	}
}

// TestSchedulerRequeue verifies that a job is re-queued.
func TestSchedulerRequeue(t *testing.T) {
	var executions atomic.Int32

	s := scheduler.NewScheduler(5, 3)

	// Use t.Cleanup to ensure resources are cleaned up
	t.Cleanup(s.Stop)

	// Define a job that increments executionCount
	s.AddJob(
		scheduler.NewJob(
			"test",
			1*time.Second,
			func(*scheduler.Job) bool {
				executions.Add(1)
				return true
			},
		),
	)

	// Use a context with timeout to wait for job to run multiple times.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	<-ctx.Done() // Wait for the context to expire

	// Check if the job was executed multiple times
	got := executions.Load()
	want := int32(2)
	if got < want {
		t.Errorf("got %d executions, want at leat %d", got, want)
	}
}

// TestSchedulerStopJob verifies that stopping a job prevents it from being
// re-queued.
func TestSchedulerStopJob(t *testing.T) {
	var executions atomic.Int32

	s := scheduler.NewScheduler(5, 3)

	// Use t.Cleanup to ensure resources are cleaned up
	t.Cleanup(s.Stop)

	// Define a job that increments executionCount
	job := scheduler.NewJob(
		"test",
		time.Second,
		func(*scheduler.Job) bool {
			executions.Add(1)
			return true
		},
	)
	s.AddJob(job)

	// Wait for the job to execute at least once
	time.Sleep(500 * time.Millisecond)

	// Stop the job to prevent it from being re-queued
	s.StopJob(job.ID())

	// Use a context with timeout to control the test duration
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	<-ctx.Done() // Wait for the context to expire

	// Check if the job was executed only once
	got := executions.Load()
	if got != 1 {
		t.Errorf("got %d executions, wanted only one", got)
	}
}

// TestSchedulerStopJobNonExistent verifies that StopJob returns an error
// for non-existent jobs.
func TestSchedulerStopJobNonExistent(t *testing.T) {
	s := scheduler.NewScheduler(5, 3)
	t.Cleanup(s.Stop)

	err := s.StopJob("non-existent-job")
	if err == nil {
		t.Errorf("didn't get error when wanted for non-existent job")
	}
}

// containsExactly checks if `slice` contains exactly the elements in `required`
// with no extras.
func containsExactly[T comparable](slice []T, required []T) bool {
	for _, item := range required {
		if !slices.Contains(slice, item) {
			return false
		}
	}

	for _, item := range slice {
		if !slices.Contains(required, item) {
			return false
		}
	}
	return true
}

// TestSchedulerJobs verifies that Jobs method returns correct job IDs.
func TestSchedulerJobs(t *testing.T) {
	s := scheduler.NewScheduler(10, 1)
	defer s.Stop()

	runFunc := func(*scheduler.Job) bool { return true }

	job1 := scheduler.NewJob("job1", time.Second, runFunc)
	job2 := scheduler.NewJob("job2", time.Second, runFunc)
	s.AddJob(job1)
	s.AddJob(job2)

	gotJobIDs := s.JobIDs()

	gotNum := len(gotJobIDs)
	wantNum := 2
	if gotNum != wantNum {
		t.Errorf("got %d jobs, expected %d", gotNum, wantNum)
	}

	wantJobIDs := []string{"job1", "job2"}

	if !containsExactly(gotJobIDs, wantJobIDs) {
		t.Errorf("got %v, want %v", gotJobIDs, wantJobIDs)
	}
}

// TestSchedulerDuplicateJobID verifies that adding a job with a duplicate
// ID returns an error.
func TestSchedulerDuplicateJobID(t *testing.T) {
	s := scheduler.NewScheduler(1, 1)
	defer s.Stop()

	runFunc := func(*scheduler.Job) bool { return true }

	job1 := scheduler.NewJob("test", time.Second, runFunc)
	job2 := scheduler.NewJob("test", time.Second, runFunc)

	if err := s.AddJob(job1); err != nil {
		t.Errorf("got error %q, want %v", err, nil)
	}

	wantErr := scheduler.ErrJobIDExists
	if err := s.AddJob(job2); err != nil && err != wantErr {
		t.Errorf("got error %q, want error %q", err, wantErr)
	}
}

// TestSchedulerConcurrentExecution checks for race conditions by concurrently
// executing jobs.
func TestSchedulerConcurrentExecution(t *testing.T) {
	var executions atomic.Int32

	s := scheduler.NewScheduler(10, 5)
	t.Cleanup(s.Stop)

	job := scheduler.NewJob(
		"test",
		100*time.Millisecond,
		func(*scheduler.Job) bool {
			executions.Add(1)
			return true
		},
	)

	s.AddJob(job)

	// Allow the job to execute several times concurrently
	time.Sleep(1 * time.Second)

	// Verify execution count
	got := executions.Load()
	want := int32(5)
	if got < want {
		t.Errorf("got %d executions, want at least %d",
			got, want)
	}
}

// TestSchedulerConcurrentStop verifies stopping the scheduler concurrently
// with job execution.
func TestSchedulerConcurrentStop(t *testing.T) {
	s := scheduler.NewScheduler(10, 2)
	defer s.Stop()

	var executions atomic.Int32

	// Define a job that increments the counter and simulates some work
	job := scheduler.NewJob(
		"test",
		100*time.Millisecond,
		func(*scheduler.Job) bool {
			executions.Add(1)
			time.Sleep(50 * time.Millisecond)
			return true
		},
	)

	// Add the job to the scheduler
	s.AddJob(job)

	// Start stopping the scheduler concurrently after a short delay
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(200 * time.Millisecond) // Allow some executions
		s.Stop()
	}()

	// Wait for the scheduler to stop
	wg.Wait()

	// Capture the final execution count after the scheduler has stopped
	finalCount := executions.Load()

	// Check that at least one execution occurred
	if finalCount == 0 {
		t.Error("got 0 executions, want at least 1")
	}

	// Check that no additional jobs were executed after stopping
	time.Sleep(200 * time.Millisecond)
	if executions.Load() != finalCount {
		t.Errorf("got %d executions, want %d after stopping",
			executions.Load(), finalCount)
	}
}

// TestSchedulerMultipleJobsConcurrent verifies multiple jobs run concurrently
// without interference.
func TestSchedulerMultipleJobsConcurrent(t *testing.T) {
	var mu sync.Mutex
	executions := make(map[string]int)

	s := scheduler.NewScheduler(10, 5)
	t.Cleanup(s.Stop)

	// Add multiple jobs
	for i := 1; i <= 5; i++ {
		jobID := "job" + strconv.Itoa(i)
		s.AddJob(
			scheduler.NewJob(
				jobID,
				200*time.Millisecond,
				func(job *scheduler.Job) bool {
					mu.Lock()
					executions[job.ID()]++
					mu.Unlock()
					return true
				},
			),
		)
	}

	// Allow jobs to execute concurrently for a short period
	time.Sleep(1 * time.Second)

	// Verify each job executed at least once
	mu.Lock()
	defer mu.Unlock()
	for i := 1; i <= 5; i++ {
		jobID := "job" + strconv.Itoa(i)
		if executions[jobID] == 0 {
			t.Errorf("want job %s to execute, but it didn't", jobID)
		}
	}
}

// TestSchedulerAddJobNil verifies adding a nil job returns error.
func TestSchedulerAddJobNil(t *testing.T) {
	s := scheduler.NewScheduler(1, 1)
	t.Cleanup(s.Stop)

	gotErr := s.AddJob(nil)
	wantErr := scheduler.ErrNilJob
	if gotErr != wantErr {
		t.Errorf("got error %q, want %q", gotErr, wantErr)
	}

}

// TestSchedulerWithJobPanic verifies the scheduler handles a run function
// that panics.
func TestSchedulerWithJobPanic(t *testing.T) {
	var executions atomic.Int32
	var panics atomic.Int32

	s := scheduler.NewScheduler(5, 2)

	// Use t.Cleanup to ensure resources are cleaned up
	t.Cleanup(s.Stop)

	job := scheduler.NewJob(
		"test",
		500*time.Millisecond,
		func(*scheduler.Job) bool {
			executions.Add(1)
			panic("panic job")
			return true
		},
		scheduler.WithRecoveryHandler(func(job *scheduler.Job, v any) {
			panics.Add(1)
		}),
	)
	s.AddJob(job)

	// Use a context with timeout to wait for job to finish.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	<-ctx.Done() // Wait for the context to expire

	// Check if the job was executed at least once
	gotExecutions := executions.Load()
	if gotExecutions < 1 {
		t.Errorf("got %d executions, want at least 1", gotExecutions)
	}

	// Check if panic function was executed at least once
	gotPanics := panics.Load()
	if gotExecutions < 1 {
		t.Errorf("got %d panics, want at least 1", gotPanics)
	}

	if gotExecutions != gotPanics {
		t.Errorf("want executions %d to match panics %d",
			gotExecutions, gotPanics)
	}
}

// TestSchedulerWithMaxExecutions verifies the scheduler handles max executions
// for a job.
func TestSchedulerWithMaxExecutions(t *testing.T) {
	var executions atomic.Uint64

	s := scheduler.NewScheduler(5, 2)

	// Use t.Cleanup to ensure resources are cleaned up
	t.Cleanup(s.Stop)

	wantExecutions := uint64(3)

	job := scheduler.NewJob(
		"test",
		500*time.Millisecond,
		func(*scheduler.Job) bool {
			executions.Add(1)
			return true
		},
		scheduler.WithMaxExecutions(wantExecutions),
	)
	s.AddJob(job)

	// Use a context with timeout to wait for job to finish.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	<-ctx.Done() // Wait for the context to expire

	// Check if the job was executed at least once
	gotExecutions := executions.Load()
	if gotExecutions != wantExecutions {
		t.Errorf("got %d executions, want %d executions",
			gotExecutions, wantExecutions)
	}
}

// TestSchedulerWithRunFalse verifies the scheduler stops requeue of job if
// run returns false.
func TestSchedulerWithRunFalse(t *testing.T) {
	var executions atomic.Int64

	s := scheduler.NewScheduler(5, 2)

	// Use t.Cleanup to ensure resources are cleaned up
	t.Cleanup(s.Stop)

	wantExecutions := int64(1)

	job := scheduler.NewJob(
		"test",
		500*time.Millisecond,
		func(*scheduler.Job) bool {
			executions.Add(1)
			return false
		},
	)
	s.AddJob(job)

	// Use a context with timeout to wait for job to finish.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	<-ctx.Done() // Wait for the context to expire

	// Check if the job was executed correctly
	gotExecutions := executions.Load()
	if gotExecutions != wantExecutions {
		t.Errorf("got %d executions, want %d executions",
			gotExecutions, wantExecutions)
	}
}

// TestSchedulerWithRunStop verifies the scheduler stops requeue if run
// uses job.Stop().
func TestSchedulerWithRunStop(t *testing.T) {
	var executions atomic.Int64

	s := scheduler.NewScheduler(5, 2)

	// Use t.Cleanup to ensure resources are cleaned up
	t.Cleanup(s.Stop)

	wantExecutions := int64(1)

	job := scheduler.NewJob(
		"test",
		500*time.Millisecond,
		func(job *scheduler.Job) bool {
			executions.Add(1)
			job.Stop()
			return true
		},
	)
	s.AddJob(job)

	// Use a context with timeout to wait for job to finish.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	<-ctx.Done() // Wait for the context to expire

	// Check if the job was executed correctly
	gotExecutions := executions.Load()
	if gotExecutions != wantExecutions {
		t.Errorf("got %d executions, want %d executions",
			gotExecutions, wantExecutions)
	}
}

// TestHighFrequencyJob verifies that a high-frequency job can handle rapid
// re-queuing and execution.
func TestHighFrequencyJob(t *testing.T) {
	var executionCount atomic.Int32
	s := scheduler.NewScheduler(10, 5)

	t.Cleanup(s.Stop)

	// Define a high-frequency job with a 1ms interval
	highFreqJob := scheduler.NewJob(
		"high_freq_test",
		1*time.Millisecond,
		func(*scheduler.Job) bool {
			executionCount.Add(1)
			return true
		},
	)

	// Add the high-frequency job to the scheduler
	s.AddJob(highFreqJob)

	// Use a context with a timeout to control the test duration
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Wait for the context to expire
	<-ctx.Done()

	// Capture the number of executions within the test duration
	executions := executionCount.Load()

	// Check that the job executed a reasonable number of times.
	// This threshold can be adjusted depending on machine speed.
	if executions < 50 {
		t.Errorf("expected high-frequency job to execute at least 50 times, got %d", executions)
	}
}
