// Command schedulerdemo runs a few sample jobs until interrupted, then
// shuts down and prints each job's final state.
//
// Use -log-level debug to see the scheduler's own debug messages, such as
// late runs and jobs stopping, and -log-format json for JSON logs.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/bnixon67/scheduler"
)

func main() {
	level := slog.LevelInfo
	flag.TextVar(&level, "log-level", level,
		"minimum log level: debug, info, warn or error")
	logFormat := flag.String("log-format", "text", "log format: text or json")
	flag.Parse()

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	switch *logFormat {
	case "text":
		handler = slog.NewTextHandler(os.Stderr, opts)
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, opts)
	default:
		fmt.Fprintf(os.Stderr,
			"invalid value %q for flag -log-format: want text or json\n",
			*logFormat)
		flag.Usage()
		os.Exit(2)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	s, err := scheduler.New(5, scheduler.WithLogger(logger))
	if err != nil {
		fatal("new scheduler", err)
	}

	// Keep each job to print at the end, since finished jobs are removed
	// from the scheduler.
	var scheduled []*scheduler.Job
	schedule := func(name string, interval time.Duration,
		run scheduler.RunFunc, opts ...scheduler.JobOption) {
		job, err := s.Schedule(name, interval, run, opts...)
		if err != nil {
			fatal("schedule "+name, err)
		}
		scheduled = append(scheduled, job)
	}

	// Each job logs its run and then sleeps to simulate work.
	jobs := []struct {
		name     string
		interval time.Duration
		work     time.Duration
		opts     []scheduler.JobOption
	}{
		{"5 runs @ 10 sec", 10 * time.Second, 1 * time.Second,
			[]scheduler.JobOption{scheduler.WithMaxRuns(5)}},
		{"30 sec", 30 * time.Second, 1 * time.Second, nil},
		{"1 min", 1 * time.Minute, 5 * time.Second, nil},
		{"5 min", 5 * time.Minute, 5 * time.Second, nil},
		{"1 hour", 1 * time.Hour, 10 * time.Second, nil},
	}
	for _, j := range jobs {
		work := j.work
		run := func(ctx context.Context, job *scheduler.Job) error {
			logger.Info("run", "job", job)
			select {
			case <-time.After(work):
			case <-ctx.Done(): // stopped during the run
			}
			return nil
		}
		schedule(j.name, j.interval, run, j.opts...)
	}
	schedule("stats every 5 sec", 5*time.Second, logStats)

	// Run until interrupted.
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	stop() // a second interrupt now kills the program

	// Let runs in progress finish, but give up on them after 5 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		logger.Warn("shutdown timed out; canceled runs still in progress",
			"err", err)
	}
	s.Stop() // wait for any canceled runs to return

	for _, job := range scheduled {
		fmt.Println(job)
	}
}

// logStats logs memory use and the number of goroutines.
func logStats(_ context.Context, job *scheduler.Job) error {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	slog.Info("stats", "job", job.Name(),
		"allocKB", m.Alloc/1024,
		"totalAllocKB", m.TotalAlloc/1024,
		"sysKB", m.Sys/1024,
		"goroutines", runtime.NumGoroutine())
	return nil
}

// fatal logs err and exits. The jobs are fixed in code, so an error
// scheduling them is a programming mistake.
func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
