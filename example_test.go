package scheduler_test

import (
	"context"
	"errors"
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
// when the job stops. Add scheduler.WithRunTimeout to bound how long each
// request may take. The same goes for any call that takes a context, such
// as a database query.
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

	job, err := s.Schedule("check", time.Minute, run, scheduler.WithMaxRuns(1))
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

// This example checks whether a run is in progress, during and after it.
func ExampleJob_Running() {
	s, err := scheduler.New(1)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Stop()

	started, release := make(chan struct{}), make(chan struct{})
	job, err := s.Schedule("work", time.Hour,
		func(context.Context, *scheduler.Job) error {
			close(started)
			<-release // the work
			return nil
		},
		scheduler.WithMaxRuns(1))
	if err != nil {
		log.Fatal(err)
	}

	<-started
	fmt.Println("running:", job.Running())

	close(release)
	<-job.Context().Done() // canceled when the job stops after its one run
	fmt.Println("running:", job.Running())

	// Output:
	// running: true
	// running: false
}

// This example checks that a job has run recently, as a health check
// might.
func ExampleJob_LastRun() {
	s, err := scheduler.New(1)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Stop()

	job, err := s.Schedule("backup", time.Hour,
		func(context.Context, *scheduler.Job) error { return nil },
		scheduler.WithMaxRuns(1))
	if err != nil {
		log.Fatal(err)
	}
	<-job.Context().Done()

	fmt.Println("ran in the last minute:", time.Since(job.LastRun()) < time.Minute)

	// Output:
	// ran in the last minute: true
}

// This example reports a job as unhealthy if its latest run failed.
func ExampleJob_LastError() {
	s, err := scheduler.New(1)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Stop()

	job, err := s.Schedule("sync", time.Hour,
		func(context.Context, *scheduler.Job) error {
			return errors.New("database unavailable")
		},
		scheduler.WithMaxRuns(1),
		// Don't log the error; it is checked below instead.
		scheduler.WithErrorHandler(func(*scheduler.Job, error) {}))
	if err != nil {
		log.Fatal(err)
	}
	<-job.Context().Done()

	if err := job.LastError(); err != nil {
		fmt.Println("unhealthy:", err)
	}

	// Output:
	// unhealthy: database unavailable
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

// This example spreads three jobs that share an interval across it, so
// they take turns instead of all running at the same moment. Each keeps
// its offset on every run. For an offset that differs on each start, use
// rand.N(interval) from math/rand/v2; to wait one whole interval before
// the first run, pass the interval itself.
func ExampleWithStartDelay() {
	s, err := scheduler.New(3)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Stop()

	const interval = 150 * time.Millisecond
	names := []string{"a", "b", "c"}
	var jobs []*scheduler.Job
	for i, name := range names {
		offset := time.Duration(i) * interval / time.Duration(len(names))
		job, err := s.Schedule(name, interval,
			func(_ context.Context, job *scheduler.Job) error {
				fmt.Println(job.Name(), "run", job.Runs())
				return nil
			},
			scheduler.WithStartDelay(offset), // 0, 50ms, 100ms
			scheduler.WithMaxRuns(2))
		if err != nil {
			log.Fatal(err)
		}
		jobs = append(jobs, job)
	}

	for _, job := range jobs {
		<-job.Context().Done()
	}

	// Output:
	// a run 1
	// b run 1
	// c run 1
	// a run 2
	// b run 2
	// c run 2
}

// This example lists every scheduled job, as a status page might.
func ExampleScheduler_Jobs() {
	s, err := scheduler.New(2)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Stop()

	noop := func(context.Context, *scheduler.Job) error { return nil }
	for _, name := range []string{"reports", "cleanup", "backup"} {
		if _, err := s.Schedule(name, time.Hour, noop); err != nil {
			log.Fatal(err)
		}
	}

	for _, job := range s.Jobs() { // sorted by name
		fmt.Printf("%s every %v, stopped: %t\n",
			job.Name(), job.Interval(), job.Stopped())
	}

	// Output:
	// backup every 1h0m0s, stopped: false
	// cleanup every 1h0m0s, stopped: false
	// reports every 1h0m0s, stopped: false
}
