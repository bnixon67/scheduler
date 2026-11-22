// Command httpdemo checks websites periodically, then prints each site's
// runs, successes and latest error. By default it checks every 10 seconds,
// three times, so it finishes in about half a minute. Press Ctrl-C to
// stop early.
//
// Usage:
//
//	httpdemo [flags] [url ...]
//
// With no URLs, it checks a built-in list of sites that shows each kind
// of result. To use it as a simple monitor, check less often and keep
// going until interrupted:
//
//	httpdemo -interval 1m -timeout 20s -runs 0 https://example.com
//
// Use -log-level debug to see the scheduler's own debug messages, such as
// late runs and jobs stopping, and -log-format json for JSON logs.
package main

import (
	"context"
	"flag"
	"fmt"
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

// minInterval is the shortest -interval accepted, so a typo such as
// -interval 1ms cannot make the program flood a site with requests.
const minInterval = time.Second

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
		fmt.Fprintf(out, "Checks each URL every -interval, -runs times, then prints a summary.\n")
		fmt.Fprintf(out, "With no URLs, checks a built-in list of sites.\n\n")
		fmt.Fprintf(out, "Flags:\n")
		flag.PrintDefaults()
	}

	interval := flag.Duration("interval", 10*time.Second,
		"time between checks of each URL, at least "+minInterval.String())
	runTimeout := flag.Duration("timeout", 5*time.Second,
		"how long a check may take before it fails; must be shorter than -interval")
	maxRuns := flag.Int("runs", 3,
		"checks per URL; 0 means keep checking until interrupted")
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

	switch {
	case *interval < minInterval:
		usageError("invalid value %v for flag -interval: want at least %v",
			*interval, minInterval)
	case *runTimeout <= 0:
		usageError("invalid value %v for flag -timeout: want more than 0",
			*runTimeout)
	case *runTimeout >= *interval:
		usageError("-timeout %v must be shorter than -interval %v",
			*runTimeout, *interval)
	case *maxRuns < 0:
		usageError("invalid value %d for flag -runs: want 0 or more", *maxRuns)
	}

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

	// Each check that takes longer than the timeout is canceled and counts
	// as a failure. The sites start at even steps through the first
	// interval, so the checks are spread out instead of all happening at
	// the same moment, and the first site is checked at once.
	checks := make([]*check, len(sites))
	for i, site := range sites {
		c := &check{site: site}
		offset := time.Duration(i) * *interval / time.Duration(len(sites))
		c.job, err = s.Schedule(site, *interval, c.run,
			scheduler.WithStartDelay(offset),
			scheduler.WithRunTimeout(*runTimeout),
			scheduler.WithMaxRuns(*maxRuns))
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
		// The cause names the signal, such as "interrupt signal received".
		logger.Info("stopping early", "reason", context.Cause(ctx))
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
	// Closing the body drains a small unread one, so the connection can be
	// reused without reading it here.
	defer func() { _ = resp.Body.Close() }()

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
