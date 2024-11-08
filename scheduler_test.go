package scheduler_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/bnixon67/scheduler"
)

// TestJobExecution verifies that a job is executed at least once.
func TestJobExecution(t *testing.T) {
	var mu sync.Mutex
	executedJobs := []int{}

	s := scheduler.NewScheduler(10, 2)

	// Use t.Cleanup to ensure resources are cleaned up
	t.Cleanup(s.Stop)

	// Add the job to the scheduler
	s.AddJob(scheduler.NewJob("test", 1*time.Second, func(id string) {
		mu.Lock()
		executedJobs = append(executedJobs, 1)
		mu.Unlock()
	}))

	// Use a context with timeout for better control over test duration
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	<-ctx.Done() // Wait for the context to expire

	// Check if the job was executed
	mu.Lock()
	defer mu.Unlock()
	if len(executedJobs) < 1 {
		t.Errorf("Expected job to be executed at least once, but got %d executions", len(executedJobs))
	}
}

// TestMultipleJobExecution verifies that multiple jobs are executed workers.
func TestMultipleJobExecution(t *testing.T) {
	var mu sync.Mutex
	executedJobs := map[int]int{}

	s := scheduler.NewScheduler(10, 3)

	// Use t.Cleanup to ensure resources are cleaned up
	t.Cleanup(s.Stop)

	// Define multiple jobs with different IDs and intervals
	s.AddJob(scheduler.NewJob("test 1", 1*time.Second, func(id string) {
		mu.Lock()
		executedJobs[1]++
		mu.Unlock()
	}))
	s.AddJob(scheduler.NewJob("test 2", 2*time.Second, func(id string) {
		mu.Lock()
		executedJobs[2]++
		mu.Unlock()
	}))

	// Use a context with timeout to ensure the test doesn't run too long
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	<-ctx.Done() // Wait for the context to expire

	// Check if each job was executed at least once
	mu.Lock()
	defer mu.Unlock()
	if executedJobs[1] == 0 {
		t.Errorf("Expected job 1 to be executed, but it was not")
	}
	if executedJobs[2] == 0 {
		t.Errorf("Expected job 2 to be executed, but it was not")
	}
}

// TestJobRequeue verifies that a job is re-queued and executed multiple times.
func TestJobRequeue(t *testing.T) {
	var mu sync.Mutex
	executionCount := 0

	s := scheduler.NewScheduler(10, 1)

	// Use t.Cleanup to ensure resources are cleaned up
	t.Cleanup(s.Stop)

	// Define a job that increments executionCount
	s.AddJob(scheduler.NewJob("test", 1*time.Second, func(id string) {
		mu.Lock()
		executionCount++
		mu.Unlock()
	}))

	// Use a context with timeout to control the test duration
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	<-ctx.Done() // Wait for the context to expire

	// Check if the job was executed multiple times
	mu.Lock()
	defer mu.Unlock()
	if executionCount < 2 {
		t.Errorf("Expected job to be executed at least 2 times, but got %d executions", executionCount)
	}
}

// TestStopJob verifies that stopping a job prevents it from being re-queued.
func TestStopJob(t *testing.T) {
	var mu sync.Mutex
	executionCount := 0

	s := scheduler.NewScheduler(10, 1)

	// Use t.Cleanup to ensure resources are cleaned up
	t.Cleanup(s.Stop)

	// Define a job that increments executionCount
	job := scheduler.NewJob("test", 1*time.Second, func(id string) {
		mu.Lock()
		executionCount++
		mu.Unlock()
	})
	s.AddJob(job)

	// Wait for the job to execute at least once
	time.Sleep(500 * time.Millisecond) // Slightly less than 1 second to ensure execution

	// Stop the job to prevent it from being re-queued
	s.StopJob(job.ID())

	// Use a context with timeout to control the test duration
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	<-ctx.Done() // Wait for the context to expire

	// Check if the job was executed only once
	mu.Lock()
	defer mu.Unlock()
	if executionCount != 1 {
		t.Errorf("Expected job to be executed only once, but got %d executions", executionCount)
	}
}

// TestNewJobPanics verifies that NewJob panics on invalid inputs.
func TestNewJobPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected NewJob to panic on invalid inputs, but it did not")
		}
	}()
	// This should panic due to non-positive interval
	scheduler.NewJob("test", 0, func(id string) {})
}

// TestNewJobWithNilRunFunction verifies that NewJob panics if the run function is nil.
func TestNewJobWithNilRunFunction(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected NewJob to panic when run function is nil, but it did not")
		}
	}()
	// This should panic due to nil run function
	scheduler.NewJob("test", 1*time.Second, nil)
}

// TestStopNonExistentJob verifies that StopJob returns an error for non-existent jobs.
func TestStopNonExistentJob(t *testing.T) {
	s := scheduler.NewScheduler(10, 1)
	defer s.Stop()

	err := s.StopJob("non-existent-job")
	if err == nil {
		t.Errorf("Expected error when stopping a non-existent job, but got nil")
	}
}

// TestSchedulerJobs verifies that Jobs method returns correct job IDs.
func TestSchedulerJobs(t *testing.T) {
	s := scheduler.NewScheduler(10, 1)
	defer s.Stop()

	job1 := scheduler.NewJob("job1", 1*time.Second, func(id string) {})
	job2 := scheduler.NewJob("job2", 2*time.Second, func(id string) {})
	s.AddJob(job1)
	s.AddJob(job2)

	jobIDs := s.Jobs()
	if len(jobIDs) != 2 {
		t.Errorf("Expected 2 jobs, but got %d", len(jobIDs))
	}
	if jobIDs[0] != "job1" || jobIDs[1] != "job2" {
		t.Errorf("Expected job IDs to be [job1, job2], but got %v", jobIDs)
	}
}

// TestDuplicateJobID verifies that adding a job with a duplicate ID returns an error.
func TestDuplicateJobID(t *testing.T) {
	s := scheduler.NewScheduler(10, 1)
	defer s.Stop()

	job := scheduler.NewJob("test", 1*time.Second, func(id string) {})
	s.AddJob(job)

	err := s.AddJob(scheduler.NewJob("test", 2*time.Second, func(id string) {}))
	if err != scheduler.ErrJobIDExists {
		t.Errorf("Expected error %v, but got %v", scheduler.ErrJobIDExists, err)
	}
}

// TestConcurrentJobExecution checks for race conditions by concurrently executing jobs.
func TestConcurrentJobExecution(t *testing.T) {
	var mu sync.Mutex
	executionCount := 0

	s := scheduler.NewScheduler(10, 5) // Increase workers to allow concurrent execution
	defer s.Stop()

	// Define a job that increments executionCount
	job := scheduler.NewJob("test", 100*time.Millisecond, func(id string) {
		mu.Lock()
		executionCount++
		mu.Unlock()
	})

	// Add the job and wait a moment to allow multiple executions
	s.AddJob(job)

	// Allow the job to execute several times concurrently
	time.Sleep(1 * time.Second)

	// Verify execution count
	mu.Lock()
	defer mu.Unlock()
	if executionCount < 5 {
		t.Errorf("Expected at least 5 executions, got %d", executionCount)
	}
}

// TestConcurrentSchedulerStop verifies stopping the scheduler concurrently with job execution.
func TestConcurrentSchedulerStop(t *testing.T) {
	s := scheduler.NewScheduler(10, 2)
	defer s.Stop()

	// Define a job that continuously runs
	job := scheduler.NewJob("test", 100*time.Millisecond, func(id string) {
		time.Sleep(50 * time.Millisecond)
	})

	// Add job and start stopping scheduler concurrently
	s.AddJob(job)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		time.Sleep(200 * time.Millisecond)
		s.Stop()
	}()

	// Wait for scheduler to stop without race conditions
	wg.Wait()
}

// TestMultipleJobConcurrentExecution verifies multiple jobs run concurrently without interference.
func TestMultipleJobConcurrentExecution(t *testing.T) {
	var mu sync.Mutex
	executions := make(map[string]int)

	s := scheduler.NewScheduler(10, 5)
	defer s.Stop()

	// Add multiple jobs
	for i := 1; i <= 5; i++ {
		jobID := "job" + strconv.Itoa(i)
		s.AddJob(scheduler.NewJob(jobID, 200*time.Millisecond, func(id string) {
			mu.Lock()
			executions[id]++
			mu.Unlock()
		}))
	}

	// Allow jobs to execute concurrently for a short period
	time.Sleep(1 * time.Second)

	// Verify each job executed at least once
	mu.Lock()
	defer mu.Unlock()
	for i := 1; i <= 5; i++ {
		jobID := "job" + strconv.Itoa(i)
		if executions[jobID] == 0 {
			t.Errorf("Expected job %s to execute, but it did not", jobID)
		}
	}
}
