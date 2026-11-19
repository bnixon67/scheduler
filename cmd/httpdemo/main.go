// Command httpdemo checks a few websites every 10 seconds, three times
// each, then prints each site's runs, successes and latest error.
// Press Ctrl-C to stop early.
//
// Usage:
//
//	httpdemo [flags] [url ...]
//
// With no URLs, it checks a built-in list of sites that shows each kind
// of result.
//
// Use -log-level debug to see the scheduler's own debug messages, such as
// late runs and jobs stopping, and -log-format json for JSON logs.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bnixon67/scheduler"
)

// Every site is checked every interval, maxRuns times. A check that takes
// longer than runTimeout is canceled and counts as a failure.
const (
	interval   = 10 * time.Second
	runTimeout = 5 * time.Second
	maxRuns    = 3
)

// defaultSites are checked when no URLs are given. Each shows a
// different result.
var defaultSites = []string{
	"https://bn67.net",               // success
	"https://foo.bn67.net",           // DNS failure
	"https://httpbin.org/status/500", // HTTP error
	"https://httpbin.org/delay/10",   // timeout
}

func main() {
	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintf(out, "Usage: httpdemo [flags] [url ...]\n\n")
		fmt.Fprintf(out, "Checks each URL every %v, %d times, then prints a summary.\n",
			interval, maxRuns)
		fmt.Fprintf(out, "With no URLs, checks a built-in list of sites.\n\n")
		fmt.Fprintf(out, "Flags:\n")
		flag.PrintDefaults()
	}

	level := slog.LevelInfo
	flag.TextVar(&level, "log-level", level,
		"minimum log `level`: debug, info, warn or error")
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
		usageError("invalid value %q for flag -log-format: want text or json",
			*logFormat)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	sites := flag.Args()
	if len(sites) == 0 {
		sites = defaultSites
	}
	for _, site := range sites {
		if u, err := url.Parse(site); err != nil || u.Host == "" ||
			(u.Scheme != "http" && u.Scheme != "https") {
			usageError("invalid URL %q: want http://host or https://host", site)
		}
	}

	s, err := scheduler.New(5, scheduler.WithLogger(logger))
	if err != nil {
		fatal("new scheduler", err)
	}

	checks := make([]*check, len(sites))
	for i, site := range sites {
		c := &check{site: site}
		c.job, err = s.Schedule(site, interval, c.run,
			scheduler.WithRunTimeout(runTimeout),
			scheduler.WithMaxRuns(maxRuns))
		if err != nil {
			fatal("schedule "+site, err)
		}
		checks[i] = c
	}

	// Run until interrupted or every job has finished.
	allDone := make(chan struct{})
	go func() {
		for _, c := range checks {
			<-c.job.Context().Done()
		}
		close(allDone)
	}()

	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case <-allDone:
	}
	stop() // a second interrupt now kills the program

	// Let runs in progress finish, but give up on them after 5 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		logger.Warn("shutdown timed out; canceled runs still in progress",
			"err", err)
	}
	s.Stop() // wait for any canceled runs to return

	fmt.Println()
	for _, c := range checks {
		runs := c.job.Runs()
		succeeded := c.successes.Load()
		fmt.Printf("%s:\n\t%d runs, %d succeeded, %d failed\n\tlast error: %v\n\n",
			c.site, runs, succeeded, int64(runs)-succeeded, c.job.LastError())
	}
}

// check is one site being checked, and its results.
type check struct {
	site      string
	job       *scheduler.Job // set after Schedule returns; not read by run
	successes atomic.Int64
}

// run is the job's [scheduler.RunFunc]. It fetches the site and returns
// an error if the request fails or the response has an error status.
func (c *check) run(ctx context.Context, _ *scheduler.Job) error {
	slog.Debug("check", "site", c.site)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.site, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// Read and discard the body so the connection can be reused.
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("GET %s: %s", c.site, resp.Status)
	}

	c.successes.Add(1)
	slog.Info("response", "site", c.site, "status", resp.Status)
	return nil
}

// usageError prints a message and the usage, then exits with status 2,
// as the flag package does for a bad flag.
func usageError(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	flag.Usage()
	os.Exit(2)
}

// fatal logs err and exits.
func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
