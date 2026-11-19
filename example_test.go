package scheduler_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
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

// This example shows the shape of a RunFunc that follows the rules in
// "Writing a RunFunc": it checks ctx between steps, passes ctx to any
// blocking call, and returns ctx.Err() once ctx is done.
func ExampleRunFunc() {
	run := func(ctx context.Context, _ *scheduler.Job) error {
		for _, item := range []string{"a", "b", "c"} {
			if err := ctx.Err(); err != nil {
				return err // canceled, so stop early
			}
			fmt.Println("handled", item) // real work passes ctx to its calls
		}
		return nil
	}

	s, err := scheduler.New(1)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Stop()

	job, err := s.Schedule("sync", time.Hour, run, scheduler.WithMaxRuns(1))
	if err != nil {
		log.Fatal(err)
	}
	<-job.Context().Done() // canceled when the job stops after its one run

	// Output:
	// handled a
	// handled b
	// handled c
}

// This example passes ctx to an HTTP request, so the request is canceled
// when the job stops or the run's timeout passes. The same goes for any
// call that takes a context, such as a database query.
func ExampleRunFunc_http() {
	// A local server stands in for a real one.
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "ok")
		}))
	defer srv.Close()

	run := func(ctx context.Context, _ *scheduler.Job) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req) // returns early if ctx is canceled
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }() // nothing to do on failure

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		fmt.Println(resp.Status, string(body))
		return nil
	}

	s, err := scheduler.New(1)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Stop()

	job, err := s.Schedule("check", time.Minute, run,
		scheduler.WithRunTimeout(5*time.Second), // bounds each request
		scheduler.WithMaxRuns(1))
	if err != nil {
		log.Fatal(err)
	}
	<-job.Context().Done()

	// Output:
	// 200 OK ok
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
