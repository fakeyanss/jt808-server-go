package cron

import (
	"sort"
	"time"

	"github.com/fakeYanss/jt808-server-go/pkg/routines"
)

// Entry consists of a schedule and the job to be executed on that schedule.
type Entry struct {
	Schedule Schedule
	Job      Job

	// the next time the job will run. This is zero time if Cron has not been
	// started or invalid schedule.
	Next time.Time

	// the last time the job was run. This is zero time if the job has not been
	// run.
	Prev time.Time
}

// byTime is a handy wrapper to chronologically sort entries.
type byTime []*Entry

func (b byTime) Len() int { return len(b) }

func (b byTime) Swap(i, j int) { b[i], b[j] = b[j], b[i] }

// Less reports `earliest` time i should sort before j.
// zero time is not `earliest` time.
func (b byTime) Less(i, j int) bool {
	if b[i].Next.IsZero() {
		return false
	}
	if b[j].Next.IsZero() {
		return true
	}

	return b[i].Next.Before(b[j].Next)
}

// Job is the interface that wraps the basic Run method.
//
// Run executes the underlying func.
type Job interface {
	JobID() string
	Run()
}

type BaseJob struct {
	ID      string
	JobFunc func()
}

func (job *BaseJob) JobID() string {
	return job.ID
}

func (job *BaseJob) Run() {
	job.JobFunc()
}

// Cron provides a convenient interface for scheduling job such as to clean-up
// database entry every month.
//
// Cron keeps track of any number of entries, invoking the associated func as
// specified by the schedule. It may also be started, stopped and the entries
// may be inspected.
type Cron struct {
	entries []*Entry
	running bool
	add     chan *Entry
	cancel  chan string
	stop    chan struct{}
}

// New instantiates new Cron instant c.
func New() *Cron {
	return &Cron{
		stop:   make(chan struct{}),
		add:    make(chan *Entry),
		cancel: make(chan string),
	}
}

// Start signals cron instant c to get up and running.
func (c *Cron) Start() {
	c.running = true
	routines.GoSafe(func() { c.run() })
}

// Add appends schedule, job to entries.
//
// if cron instant is not running, adding to entries is trivial.
// otherwise, to prevent data-race, adds through channel.
func (c *Cron) Add(s Schedule, j Job) {
	entry := &Entry{
		Schedule: s,
		Job:      j,
	}

	if !c.running {
		c.entries = append(c.entries, entry)
		return
	}
	c.add <- entry
}

// AddFunc registers the Job function for the given Schedule.
func (c *Cron) AddFunc(s Schedule, jobID string, j func()) {
	c.Add(s, &BaseJob{
		ID:      jobID,
		JobFunc: j,
	})
}

// Cancel job from entries
//
// if cron instant is not running, remove job from entries directly.
// otherwise, to prevent data-race, removes from channel.
func (c *Cron) Cancel(jobID string) {
	if !c.running {
		c.cancelJob(jobID)
		return
	}
	c.cancel <- jobID
}

func (c *Cron) cancelJob(jonID string) {
	idx := -1
	for i, entry := range c.entries {
		if jonID == entry.Job.JobID() {
			idx = i
			break
		}
	}
	if idx != -1 {
		c.entries = append(c.entries[:idx], c.entries[idx+1:]...)
	}
}

// Stop halts cron instant c from running.
func (c *Cron) Stop() {
	if !c.running {
		return
	}
	c.running = false
	c.stop <- struct{}{}
}

var after = time.After

// run the scheduler...
//
// It needs to be private as it's responsible of synchronizing a critical
// shared state: `running`.
func (c *Cron) run() {
	var effective time.Time
	now := time.Now().Local()

	// to figure next trig time for entries, referenced from now
	for _, e := range c.entries {
		e.Next = e.Schedule.Next(now)
	}

	for {
		sort.Sort(byTime(c.entries))
		if len(c.entries) > 0 {
			effective = c.entries[0].Next
		} else {
			effective = now.AddDate(15, 0, 0) // to prevent phantom jobs.
		}

		select {
		case now = <-after(effective.Sub(now)):
			// entries with same time gets run.
			for _, entry := range c.entries {
				if entry.Next != effective {
					break
				}
				entry.Prev = now
				entry.Next = entry.Schedule.Next(now)
				go entry.Job.Run()
			}
		case e := <-c.add:
			e.Next = e.Schedule.Next(time.Now())
			c.entries = append(c.entries, e)
		case cancelJobID := <-c.cancel:
			c.cancelJob(cancelJobID)
		case <-c.stop:
			return // terminate go-routine.
		}
	}
}

// Entries returns cron etn
func (c Cron) Entries() []*Entry {
	return c.entries
}
