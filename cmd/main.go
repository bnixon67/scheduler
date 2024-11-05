package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/bnixon67/scheduler"
)

func main() {
	s := scheduler.NewScheduler(3, 3)

	var (
		timesMap      = make(map[int][]time.Time)
		timesMapMutex = sync.Mutex{}
	)

	var jobs []*scheduler.Job

	jobs = append(jobs,
		scheduler.NewJob(
			"every 30 sec",
			30*time.Second,
			func(id string) {
				log.Println("Job", id)
				timesMapMutex.Lock()
				timesMap[0] = append(timesMap[0], time.Now())
				timesMapMutex.Unlock()
				time.Sleep(1 * time.Second)

			}),
	)
	jobs = append(jobs,
		scheduler.NewJob(
			"every 5 minutes",
			5*time.Minute,
			func(id string) {
				log.Println("Job", id)
				timesMapMutex.Lock()
				timesMap[1] = append(timesMap[1], time.Now())
				timesMapMutex.Unlock()
				time.Sleep(5 * time.Second)

			}),
	)
	jobs = append(jobs,
		scheduler.NewJob(
			"every hour",
			1*time.Hour,
			func(id string) {
				log.Println("Job", id)
				timesMapMutex.Lock()
				timesMap[2] = append(timesMap[2], time.Now())
				timesMapMutex.Unlock()
				time.Sleep(10 * time.Second)
			}),
	)

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
		for _, t := range timesMap[n] {
			fmt.Println("\t", t.Format("15:04:05"))
		}
	}

}
