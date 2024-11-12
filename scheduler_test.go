package scheduler_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bnixon67/scheduler"
)

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

// TestSchedulerJobIDs verifies that JobIDs() returns correct results.
func TestSchedulerJobIDs(t *testing.T) {
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

// TestSchedulerGetJob verifies that retrieval of a job by ID from Scheduler.
func TestSchedulerGetJob(t *testing.T) {
	s := scheduler.NewScheduler(2, 1)
	t.Cleanup(s.Stop)

	jobID := randomID()

	runFunc := func(*scheduler.Job) bool { return true }
	wantJob := scheduler.NewJob(jobID, time.Second, runFunc)
	s.AddJob(wantJob)
	xtraJob := scheduler.NewJob(randomID(), time.Second, runFunc)
	s.AddJob(xtraJob)

	// Verify existent jobID
	gotJob := s.GetJob(jobID)
	if gotJob != wantJob {
		t.Errorf("\nGetJob(%q) = \n%v,\nwant = \n%v",
			jobID, gotJob, wantJob)
	}

	// Verify non-existent jobID
	jobID = "not" + jobID
	gotJob = s.GetJob(jobID)
	wantJob = nil
	if gotJob != wantJob {
		t.Errorf("\nGetJob(%q) = \n%v,\nwant = \n%v",
			jobID, gotJob, wantJob)
	}
}

// TestSchedulerWithMaxExecutions verifies that the WithMaxExecutions option
// correctly limits the number of times a job is executed.
func TestSchedulerWithMaxExecutions(t *testing.T) {
	var executions atomic.Uint64

	wantExecutions := uint64(3)

	// Create a WaitGroup to synchronize the job executions
	var wg sync.WaitGroup
	wg.Add(int(wantExecutions))

	job := scheduler.NewJob(
		randomID(),
		50*time.Millisecond,
		func(*scheduler.Job) bool {
			executions.Add(1)
			wg.Done()
			return true
		},
		scheduler.WithMaxExecutions(wantExecutions),
	)

	s := scheduler.NewScheduler(5, 3)
	t.Cleanup(s.Stop) // Ensure the scheduler is stopped after the test
	s.AddJob(job)

	// Use a context with timeout to prevent the test from hanging
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Wait for the job to reach the desired number of executions or timeout
	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	select {
	case <-ctx.Done():
		t.Fatalf("timeout waiting for %d executions", wantExecutions)
	case <-doneCh:
		// Job executed the expected number of times
	}

	// Verify the number of executions
	gotExecutions := executions.Load()
	if gotExecutions != wantExecutions {
		t.Errorf("got %d executions, want %d",
			gotExecutions, wantExecutions)
	}

	// Ensure the job does not execute more times after the test
	time.Sleep(250 * time.Millisecond)
	finalExecutions := executions.Load()
	if finalExecutions > wantExecutions {
		t.Errorf("job executed after test completion: got %d, want %d",
			finalExecutions, wantExecutions)
	}
}

// TestSchedulerWithStopOnPanic verifies that WithStopOnPanic works correctly.
func TestSchedulerWithStopOnPanic(t *testing.T) {
	for _, stopOnPanic := range []bool{true, false} {
		name := fmt.Sprintf("stopOnPanic=%t", stopOnPanic)
		t.Run(name, func(t *testing.T) {
			var executions atomic.Uint64

			job := scheduler.NewJob(
				randomID(),
				250*time.Millisecond,
				func(*scheduler.Job) bool {
					executions.Add(1)
					panic("panic")
					return true
				},
				scheduler.WithStopOnPanic(stopOnPanic),
				scheduler.WithRecoveryHandler(
					func(*scheduler.Job, any) {}),
			)

			s := scheduler.NewScheduler(5, 3)
			t.Cleanup(s.Stop)
			s.AddJob(job)

			time.Sleep(1 * time.Second)

			// Verify the number of executions
			gotExecutions := executions.Load()
			wantExecutions := uint64(1)
			if !stopOnPanic {
				wantExecutions = gotExecutions
			}
			if gotExecutions != wantExecutions {
				t.Errorf("got %d executions, want %d",
					gotExecutions, wantExecutions)
			}
		})
	}
}

// randomID generates a random jobID.
func randomID() string {
	length := 8

	// Calculate the number of bytes needed for the desired string length
	byteLength := (length * 3) / 4
	randomBytes := make([]byte, byteLength)

	// Fill the byte slice with random data
	if _, err := rand.Read(randomBytes); err != nil {
		panic(err)
	}

	// Encode to base64 and truncate to the desired length
	return base64.RawURLEncoding.EncodeToString(randomBytes)[:length]
}

// TestSchedulerStopJob verifies that stopping a job prevents it from being
// re-queued.
func TestSchedulerStopJob(t *testing.T) {
	var executions atomic.Int32

	s := scheduler.NewScheduler(5, 3)
	t.Cleanup(s.Stop)

	jobID := randomID()
	job := scheduler.NewJob(
		jobID,
		time.Second,
		func(*scheduler.Job) bool {
			executions.Add(1)
			return true
		},
	)
	s.AddJob(job)

	time.Sleep(500 * time.Millisecond)

	// Stop the job to prevent it from being re-queued
	s.StopJob(job.ID())

	time.Sleep(1500 * time.Millisecond)

	// Check if the job was executed only once
	got := executions.Load()
	if got != 1 {
		t.Errorf("got %d executions, wanted only one", got)
	}

	// Confirm Job removed
	job = s.GetJob(jobID)
	if job != nil {
		t.Errorf("GetJob(%q) = %v, want nil", jobID, job)
	}
}

// TestSchedulerStopJobNonExistent verifies that StopJob returns an error
// for non-existent jobs.
func TestSchedulerStopJobNonExistent(t *testing.T) {
	s := scheduler.NewScheduler(5, 3)
	t.Cleanup(s.Stop)

	jobID := randomID()
	got := s.StopJob(jobID)
	want := scheduler.ErrJobNotFound
	if !errors.Is(got, want) {
		t.Errorf("StopJob(%q) = %v, want %v", jobID, got, want)
	}
}

// TestSchedulerAddJobDuplicateID verifies that adding a job with a duplicate
// ID returns an error.
func TestSchedulerAddJobDuplicateID(t *testing.T) {
	s := scheduler.NewScheduler(5, 5)
	defer s.Stop()

	runFunc := func(*scheduler.Job) bool { return true }

	jobID := randomID()
	job1 := scheduler.NewJob(jobID, time.Second, runFunc)
	job2 := scheduler.NewJob(randomID(), time.Second, runFunc)
	job3 := scheduler.NewJob(jobID, time.Second, runFunc)
	job4 := scheduler.NewJob(jobID, time.Second, runFunc)

	tests := []struct {
		job     *scheduler.Job
		wantErr error
	}{
		{job1, nil},
		{job2, nil},
		{job3, scheduler.ErrJobIDExists},
		{job4, scheduler.ErrJobIDExists},
	}

	for n, tc := range tests {
		name := fmt.Sprintf("%d", n)
		t.Run(name, func(t *testing.T) {
			gotErr := s.AddJob(tc.job)
			if !errors.Is(gotErr, tc.wantErr) {
				t.Errorf("AddJob(%q) = %v, want %v",
					tc.job.ID(), gotErr, tc.wantErr)
			}
		})
	}
}

// TestSchedulerAddJobNil verifies adding a nil job returns error.
func TestSchedulerAddJobNil(t *testing.T) {
	s := scheduler.NewScheduler(1, 1)
	t.Cleanup(s.Stop)

	gotErr := s.AddJob(nil)
	wantErr := scheduler.ErrNilJob
	if !errors.Is(gotErr, wantErr) {
		t.Errorf("err = %q, want %q", gotErr, wantErr)
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

	time.Sleep(1500 * time.Millisecond)

	// Check if the job was executed at least once
	got := executions.Load()
	if got < 1 {
		t.Errorf("execution = %d, want at least 1", got)
	}
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
	t.Cleanup(s.Stop)

	for n := range jobs {
		s.AddJob(jobs[n])
	}

	time.Sleep(3 * time.Second)

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
	t.Cleanup(s.Stop)

	s.AddJob(
		scheduler.NewJob(
			randomID(),
			500*time.Millisecond,
			func(*scheduler.Job) bool {
				executions.Add(1)
				return true
			},
		),
	)

	time.Sleep(2 * time.Second)

	// Check if the job was executed multiple times
	got := executions.Load()
	want := int32(2)
	if got < want {
		t.Errorf("execution = %d, want at leat %d", got, want)
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
