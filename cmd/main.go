package main

import (
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/bnixon67/scheduler"
)

func init() {
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()
}

func printStats() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	fmt.Printf("Alloc = %v KB,", memStats.Alloc/1024)
	fmt.Printf("TotalAlloc = %v KB,", memStats.TotalAlloc/1024)
	fmt.Printf("Sys = %v KB,", memStats.Sys/1024)
	fmt.Printf("NumGoroutines = %v\n", runtime.NumGoroutine())
}

func main() {
	s := scheduler.NewScheduler(10, 5)

	var (
		timesMap      = make(map[string][]time.Time)
		timesMapMutex = sync.Mutex{}
	)

	jobs := []*scheduler.Job{
		scheduler.NewJob(
			"stats",
			30*time.Second,
			func(job *scheduler.Job) bool {
				log.Println(job)
				timesMapMutex.Lock()
				timesMap[job.ID()] = append(timesMap[job.ID()], time.Now())
				timesMapMutex.Unlock()
				printStats()
				return true
			},
		),
		scheduler.NewJob(
			"30 sec",
			30*time.Second,
			func(job *scheduler.Job) bool {
				log.Println(job)
				timesMapMutex.Lock()
				timesMap[job.ID()] = append(timesMap[job.ID()], time.Now())
				timesMapMutex.Unlock()
				time.Sleep(1 * time.Second)
				return true
			},
		),
		scheduler.NewJob(
			"5 mins",
			5*time.Minute,
			func(job *scheduler.Job) bool {
				log.Println(job)
				timesMapMutex.Lock()
				timesMap[job.ID()] = append(timesMap[job.ID()], time.Now())
				timesMapMutex.Unlock()
				time.Sleep(5 * time.Second)
				return true
			},
		),
		scheduler.NewJob(
			"1 hour",
			1*time.Hour,
			func(job *scheduler.Job) bool {
				log.Println(job)
				timesMapMutex.Lock()
				timesMap[job.ID()] = append(timesMap[job.ID()], time.Now())
				timesMapMutex.Unlock()
				time.Sleep(10 * time.Second)
				return true
			},
		),
	}

	for n := range jobs {
		s.AddJob(jobs[n])
	}

	// Set up a channel to listen for interrupt signals
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	// Block until we receive a signal
	<-signalChan

	// Stop the scheduler gracefully
	s.Stop()

	for n := range jobs {
		fmt.Printf("%d %s\n", n, jobs[n])
		for _, t := range timesMap[jobs[n].ID()] {
			fmt.Println("\t", t.Format("15:04:05"))
		}
	}

}
