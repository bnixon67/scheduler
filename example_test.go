package scheduler_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/bnixon67/scheduler"
)

// This example runs a job three times, then waits for it to finish.
func ExampleScheduler() {
	s, err := scheduler.New(4) // at most 4 jobs run at once
	if err != nil {
		log.Fatal(err)
	}
	defer s.Stop()

	job, err := s.Schedule("greet", 10*time.Millisecond,
		func(ctx context.Context, job *scheduler.Job) error {
			fmt.Println("run", job.Runs())
			return nil // scheduler.ErrStop would stop the job
		},
		scheduler.WithMaxRuns(3),
	)
	if err != nil {
		log.Fatal(err)
	}

	<-job.Context().Done() // canceled when the job stops
	fmt.Println("runs:", job.Runs())

	// Output:
	// run 1
	// run 2
	// run 3
	// runs: 3
}

// This example stops a long-running job. Its run function watches ctx, so
// it returns as soon as the job is stopped.
func ExampleJob_Stop() {
	s, err := scheduler.New(1)
	if err != nil {
		log.Fatal(err)
	}

	started := make(chan struct{})
	job, err := s.Schedule("poll", time.Minute,
		func(ctx context.Context, _ *scheduler.Job) error {
			close(started)
			<-ctx.Done() // a real job would do work until canceled
			fmt.Println("run canceled:", ctx.Err())
			return ctx.Err() // not reported, since the job was stopped
		})
	if err != nil {
		log.Fatal(err)
	}
	<-started

	job.Stop()
	_, ok := s.Job("poll") // the name is free again at once

	s.Stop() // waits for the run to return
	fmt.Println("still scheduled:", ok)

	// Output:
	// run canceled: context canceled
	// still scheduled: false
}

// This example lets a run in progress finish before the program exits,
// but gives up on it after a deadline.
func ExampleScheduler_Shutdown() {
	s, err := scheduler.New(1)
	if err != nil {
		log.Fatal(err)
	}

	started := make(chan struct{})
	_, err = s.Schedule("report", time.Hour,
		func(ctx context.Context, _ *scheduler.Job) error {
			close(started)
			time.Sleep(10 * time.Millisecond) // work that should not be cut off
			fmt.Println("report done, canceled:", ctx.Err() != nil)
			return nil
		})
	if err != nil {
		log.Fatal(err)
	}
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	fmt.Println("shutdown:", s.Shutdown(ctx))

	// Output:
	// report done, canceled: false
	// shutdown: <nil>
}
