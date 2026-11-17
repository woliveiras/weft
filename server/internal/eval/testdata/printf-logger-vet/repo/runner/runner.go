// Package runner runs jobs and logs their outcome.
package runner

import "example.com/jobs/logx"

// Job is a named unit of work.
type Job struct {
	Name string
	Fn   func() error
}

// Runner runs jobs one after another.
type Runner struct {
	log logx.Logger
}

// New returns a Runner that logs to log.
func New(log logx.Logger) *Runner {
	return &Runner{log: log}
}

// Run runs every job, logs each outcome and returns how many failed.
func (r *Runner) Run(jobs []Job) int {
	r.log.Log("starting", len(jobs), "jobs")
	failed := 0
	for _, j := range jobs {
		if err := j.Fn(); err != nil {
			r.log.Log("job", j.Name, "failed:", err)
			failed++
			continue
		}
		r.log.Log("job", j.Name, "done")
	}
	r.log.Log("finished,", failed, "failed")
	return failed
}
