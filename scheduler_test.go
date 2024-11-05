package scheduler_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/bnixon67/scheduler"
)

// TestJobExecution verifies that a job is executed by the worker pool at least once.
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

// TestMultipleJobExecution verifies that multiple jobs are executed by the worker pool.
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
