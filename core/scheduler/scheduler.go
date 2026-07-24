// Package scheduler triggers workflow runs on cron schedules. It is a domain
// component, not a plugin module: scheduling is a mechanism (fire on time), not a
// swappable policy, so the kernel composes one directly — the same way it owns
// the HTTP server. It depends only on a narrow Runner interface (satisfied by
// core/workflow.Service), so a scheduled run flows through the normal pipeline
// and inherits routing, fallback, tracing, cost, and run persistence.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/conductor-ai/conductor/core/ports"
	"github.com/robfig/cron/v3"
)

// JobSpec is the declarative definition of one scheduled job, mapped from
// configuration by the kernel. Kept here (not in ports) because only the kernel
// and this package speak it.
type JobSpec struct {
	Name     string
	Workflow string
	Cron     string
	Inputs   map[string]string
}

// Runner executes a named workflow. core/workflow.Service satisfies it.
type Runner interface {
	// RunTriggered runs the workflow, tagging the resulting run with trigger.
	RunTriggered(ctx context.Context, name string, inputs map[string]string, trigger string) (ports.WorkflowRun, error)
	// Has reports whether a workflow with the given name is registered.
	Has(name string) bool
}

// RunInfo is a compact record of a job's most recent fire, surfaced by Jobs().
type RunInfo struct {
	ID          string          `json:"id"`
	Status      ports.RunStatus `json:"status"`
	CreatedUnix int64           `json:"created"`
}

// Job is one scheduled workflow run: a parsed cron schedule plus the workflow to
// run and the inputs to pass. Mutable state (running, last, next) is guarded by mu.
type Job struct {
	Name     string
	Workflow string
	CronExpr string
	Inputs   map[string]string

	schedule cron.Schedule

	mu      sync.Mutex
	running bool
	last    *RunInfo
	next    time.Time
}

// JobStatus is a point-in-time, serializable snapshot of a job for /v1/schedules.
type JobStatus struct {
	Name     string   `json:"name"`
	Workflow string   `json:"workflow"`
	Cron     string   `json:"cron"`
	NextUnix int64    `json:"next"`
	LastRun  *RunInfo `json:"last_run,omitempty"`
}

// Scheduler fires jobs whose cron schedule is due. One goroutine (started by
// Start) evaluates all jobs every tick; each fire runs in its own goroutine so a
// slow workflow never blocks the loop or other jobs.
type Scheduler struct {
	jobs   []*Job
	runner Runner
	log    *slog.Logger

	// interval is how often the loop evaluates schedules. One second gives
	// minute-granularity cron the precision it needs without busy-spinning.
	interval time.Duration
}

// New parses and validates every job, failing fast on a bad cron expression, a
// duplicate job name, or a workflow that does not exist — so misconfiguration
// surfaces at startup, never silently at fire time.
func New(jobs []JobSpec, runner Runner, log *slog.Logger) (*Scheduler, error) {
	if log == nil {
		log = slog.Default()
	}
	s := &Scheduler{runner: runner, log: log, interval: time.Second}
	seen := make(map[string]bool, len(jobs))
	for _, j := range jobs {
		if j.Name == "" {
			return nil, fmt.Errorf("scheduler: job missing name")
		}
		if seen[j.Name] {
			return nil, fmt.Errorf("scheduler: duplicate job name %q", j.Name)
		}
		seen[j.Name] = true
		if !runner.Has(j.Workflow) {
			return nil, fmt.Errorf("scheduler: job %q references unknown workflow %q", j.Name, j.Workflow)
		}
		sched, err := cron.ParseStandard(j.Cron)
		if err != nil {
			return nil, fmt.Errorf("scheduler: job %q invalid cron %q: %w", j.Name, j.Cron, err)
		}
		s.jobs = append(s.jobs, &Job{
			Name: j.Name, Workflow: j.Workflow, CronExpr: j.Cron,
			Inputs: j.Inputs, schedule: sched,
		})
	}
	return s, nil
}

// Len reports how many jobs are scheduled.
func (s *Scheduler) Len() int { return len(s.jobs) }

// Start launches the evaluation loop; it returns immediately and runs until ctx
// is cancelled. A no-op when no jobs are configured.
func (s *Scheduler) Start(ctx context.Context) {
	if len(s.jobs) == 0 {
		return
	}
	go s.loop(ctx)
}

func (s *Scheduler) loop(ctx context.Context) {
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.tick(ctx, now)
		}
	}
}

// tick fires every job due at or before now. It is the whole scheduling policy,
// isolated from the timer so tests can drive it with explicit times and no
// real waiting. The first tick a job sees only seeds its next-fire time — a job
// never fires on the very first evaluation, which also means a restart cannot
// replay a schedule that was already due while the process was down (fire-forward,
// no backfill).
func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	for _, j := range s.jobs {
		j.mu.Lock()
		if j.next.IsZero() {
			j.next = j.schedule.Next(now)
			j.mu.Unlock()
			continue
		}
		if now.Before(j.next) {
			j.mu.Unlock()
			continue
		}
		// Advance to the next fire regardless of whether we skip this one, so a
		// long-running job cannot cause a fire pile-up when it finally frees up.
		j.next = j.schedule.Next(now)
		if j.running {
			s.log.Warn("scheduler: skip fire, previous run still in progress", "job", j.Name)
			j.mu.Unlock()
			continue
		}
		j.running = true
		j.mu.Unlock()

		go func(j *Job) {
			defer func() {
				j.mu.Lock()
				j.running = false
				j.mu.Unlock()
			}()
			s.fire(ctx, j)
		}(j)
	}
}

// fire runs a job's workflow and records the outcome. Failures are logged, not
// propagated: an unattended schedule has no caller to return an error to, and the
// failed run is already persisted by the workflow service.
func (s *Scheduler) fire(ctx context.Context, j *Job) {
	s.log.Info("scheduler: firing job", "job", j.Name, "workflow", j.Workflow)
	run, err := s.runner.RunTriggered(ctx, j.Workflow, j.Inputs, ports.TriggerSchedule)
	if err != nil {
		s.log.Error("scheduler: job failed to start", "job", j.Name, "err", err)
		return
	}
	j.mu.Lock()
	j.last = &RunInfo{ID: run.ID, Status: run.Status, CreatedUnix: run.CreatedUnix}
	j.mu.Unlock()
}

// Jobs returns a snapshot of every job's schedule and last run, for the
// /v1/schedules endpoint.
func (s *Scheduler) Jobs() []JobStatus {
	out := make([]JobStatus, 0, len(s.jobs))
	for _, j := range s.jobs {
		j.mu.Lock()
		st := JobStatus{
			Name: j.Name, Workflow: j.Workflow, Cron: j.CronExpr, LastRun: j.last,
		}
		if !j.next.IsZero() {
			st.NextUnix = j.next.Unix()
		}
		j.mu.Unlock()
		out = append(out, st)
	}
	return out
}
