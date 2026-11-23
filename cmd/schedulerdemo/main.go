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

// sampleJobs are the jobs the demo runs. Each logs its run, then takes
// work to simulate doing something.
var sampleJobs = []struct {
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

func main() {
	cfg := parseFlags()

	logger := newLogger(cfg.logLevel, cfg.logFormat)
	slog.SetDefault(logger)

	s, err := scheduler.New(5, scheduler.WithLogger(logger))
	if err != nil {
		fatal("new scheduler", err)
	}

	jobs := scheduleJobs(s)
	waitForSignal()
	shutdown(s)

	// Print the jobs kept from Schedule, since finished jobs are removed
	// from the scheduler.
	for _, job := range jobs {
		fmt.Println(job)
	}
}

// config is the program's settings, from its flags.
type config struct {
	logLevel  slog.Level
	logFormat string
}

// parseFlags parses the command line, and exits with the usage message if
// it is invalid.
func parseFlags() config {
	cfg := config{logLevel: slog.LevelInfo}
	flag.TextVar(&cfg.logLevel, "log-level", cfg.logLevel,
		"minimum log `level`: debug, info, warn or error")
	flag.StringVar(&cfg.logFormat, "log-format", "text", "log format: text or json")
	flag.Parse()

	if cfg.logFormat != "text" && cfg.logFormat != "json" {
		fmt.Fprintf(os.Stderr,
			"invalid value %q for flag -log-format: want text or json\n",
			cfg.logFormat)
		flag.Usage()
		os.Exit(2)
	}
	return cfg
}

// newLogger returns a logger that writes to stderr in format, text or
// json, at level and above.
func newLogger(level slog.Level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	if format == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

// scheduleJobs schedules the sample jobs and a job that logs runtime
// stats, and returns them all.
func scheduleJobs(s *scheduler.Scheduler) []*scheduler.Job {
	var jobs []*scheduler.Job
	for _, j := range sampleJobs {
		jobs = append(jobs,
			mustSchedule(s, j.name, j.interval, simulateWork(j.work), j.opts...))
	}
	return append(jobs, mustSchedule(s, "stats every 5 sec", 5*time.Second, logStats))
}

// mustSchedule calls s.Schedule and exits if it fails. The jobs are fixed
// in code, so an error scheduling them is a programming mistake.
func mustSchedule(s *scheduler.Scheduler, name string, interval time.Duration,
	run scheduler.RunFunc, opts ...scheduler.JobOption,
) *scheduler.Job {
	job, err := s.Schedule(name, interval, run, opts...)
	if err != nil {
		fatal("schedule "+name, err)
	}
	return job
}

// simulateWork returns a RunFunc that logs its run, then takes d, or
// returns early if its job is stopped.
func simulateWork(d time.Duration) scheduler.RunFunc {
	return func(ctx context.Context, job *scheduler.Job) error {
		slog.Info("run", "job", job)
		select {
		case <-time.After(d):
		case <-ctx.Done():
		}
		return nil
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

// waitForSignal waits until the program is interrupted.
func waitForSignal() {
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop() // after this, a second interrupt kills the program
	<-ctx.Done()
}

// shutdown stops s, letting runs in progress finish but giving up on them
// after 5 seconds.
func shutdown(s *scheduler.Scheduler) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		slog.Warn("shutdown timed out; canceled runs still in progress", "err", err)
	}
	s.Stop() // wait for any canceled runs to return
}

// fatal logs err and exits.
func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
