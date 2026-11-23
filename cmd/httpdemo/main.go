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
	cfg := parseFlags()

	logger := newLogger(cfg.logLevel, cfg.logFormat)
	slog.SetDefault(logger)

	s, err := scheduler.New(5, scheduler.WithLogger(logger))
	if err != nil {
		fatal("new scheduler", err)
	}

	checks := scheduleChecks(s, cfg)
	waitForChecksOrSignal(checks)
	shutdown(s)
	printSummary(checks)
}

// config is the program's settings, from its flags and arguments.
type config struct {
	interval  time.Duration
	timeout   time.Duration
	runs      int
	logLevel  slog.Level
	logFormat string
	sites     []string
}

// parseFlags parses the command line, and exits with the usage message if
// it is invalid.
func parseFlags() config {
	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintf(out, "Usage: httpdemo [flags] [url ...]\n\n")
		fmt.Fprintf(out, "Checks each URL every -interval, -runs times, then prints a summary.\n")
		fmt.Fprintf(out, "With no URLs, checks a built-in list of sites.\n\n")
		fmt.Fprintf(out, "Flags:\n")
		flag.PrintDefaults()
	}

	cfg := config{logLevel: slog.LevelInfo}
	flag.DurationVar(&cfg.interval, "interval", 10*time.Second,
		"time between checks of each URL, at least "+minInterval.String())
	flag.DurationVar(&cfg.timeout, "timeout", 5*time.Second,
		"how long a check may take before it fails; must be shorter than -interval")
	flag.IntVar(&cfg.runs, "runs", 3,
		"checks per URL; 0 means keep checking until interrupted")
	flag.TextVar(&cfg.logLevel, "log-level", cfg.logLevel,
		"minimum log `level`: debug, info, warn or error")
	flag.StringVar(&cfg.logFormat, "log-format", "text", "log format: text or json")
	flag.Parse()

	cfg.sites = flag.Args()
	if len(cfg.sites) == 0 {
		cfg.sites = defaultSites
	}

	if err := cfg.validate(); err != nil {
		usageError(err)
	}
	return cfg
}

// validate reports the first invalid setting in cfg.
func (cfg config) validate() error {
	switch {
	case cfg.logFormat != "text" && cfg.logFormat != "json":
		return fmt.Errorf("invalid value %q for flag -log-format: want text or json",
			cfg.logFormat)
	case cfg.interval < minInterval:
		return fmt.Errorf("invalid value %v for flag -interval: want at least %v",
			cfg.interval, minInterval)
	case cfg.timeout <= 0:
		return fmt.Errorf("invalid value %v for flag -timeout: want more than 0",
			cfg.timeout)
	case cfg.timeout >= cfg.interval:
		return fmt.Errorf("-timeout %v must be shorter than -interval %v",
			cfg.timeout, cfg.interval)
	case cfg.runs < 0:
		return fmt.Errorf("invalid value %d for flag -runs: want 0 or more", cfg.runs)
	}
	for _, site := range cfg.sites {
		if !isWebURL(site) {
			return fmt.Errorf("invalid URL %q: want http://host or https://host", site)
		}
	}
	return nil
}

// isWebURL reports whether s is an http or https URL with a host.
func isWebURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https")
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

// scheduleChecks schedules a check of each site. Each check that takes
// longer than the timeout is canceled and counts as a failure. The sites
// start at even steps through the first interval, so the checks are
// spread out instead of all happening at the same moment, and the first
// site is checked at once.
func scheduleChecks(s *scheduler.Scheduler, cfg config) []*check {
	checks := make([]*check, len(cfg.sites))
	for i, site := range cfg.sites {
		c := &check{site: site}
		offset := time.Duration(i) * cfg.interval / time.Duration(len(cfg.sites))

		var err error
		c.job, err = s.Schedule(site, cfg.interval, c.run,
			scheduler.WithStartDelay(offset),
			scheduler.WithRunTimeout(cfg.timeout),
			scheduler.WithMaxRuns(cfg.runs))
		if err != nil {
			fatal("schedule "+site, err)
		}
		checks[i] = c
	}
	return checks
}

// waitForChecksOrSignal waits until every check has finished its runs, or
// the program is interrupted.
func waitForChecksOrSignal(checks []*check) {
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop() // after this, a second interrupt kills the program

	select {
	case <-ctx.Done():
		// The cause names the signal, such as "interrupt signal received".
		slog.Info("stopping early", "reason", context.Cause(ctx))
	case <-allFinished(checks):
	}
}

// allFinished returns a channel that is closed once every check's job has
// stopped.
func allFinished(checks []*check) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		for _, c := range checks {
			<-c.job.Context().Done()
		}
		close(done)
	}()
	return done
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

// printSummary prints each check's runs, successes and latest error.
func printSummary(checks []*check) {
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

// usageError prints err and the usage, then exits with status 2, as the
// flag package does for a bad flag.
func usageError(err error) {
	fmt.Fprintln(os.Stderr, err)
	flag.Usage()
	os.Exit(2)
}

// fatal logs err and exits.
func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
