// Package maintenance schedules bounded, periodic Server housekeeping.
package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"k8s.io/utils/clock"
)

const DefaultJobTimeout = 10 * time.Minute

type Job struct {
	Name       string
	Interval   time.Duration
	Timeout    time.Duration
	RunOnStart bool
	Run        func(context.Context) error
}

type Coordinator struct {
	jobs   []Job
	logger *slog.Logger
	clock  clock.Clock
}

func NewCoordinator(logger *slog.Logger, jobs ...Job) (*Coordinator, error) {
	if logger == nil {
		return nil, errors.New("maintenance logger is required")
	}
	if len(jobs) == 0 {
		return nil, errors.New("at least one maintenance job is required")
	}

	seen := make(map[string]struct{}, len(jobs))
	for _, job := range jobs {
		if strings.TrimSpace(job.Name) == "" {
			return nil, errors.New("maintenance job name is required")
		}
		if _, ok := seen[job.Name]; ok {
			return nil, fmt.Errorf("maintenance job %q is registered more than once", job.Name)
		}
		if job.Interval <= 0 {
			return nil, fmt.Errorf("maintenance job %q interval must be positive", job.Name)
		}
		if job.Timeout <= 0 {
			return nil, fmt.Errorf("maintenance job %q timeout must be positive", job.Name)
		}
		if job.Run == nil {
			return nil, fmt.Errorf("maintenance job %q Run function is required", job.Name)
		}

		seen[job.Name] = struct{}{}
	}

	return &Coordinator{
		jobs:   append([]Job(nil), jobs...),
		logger: logger.With("component", "maintenance"),
		clock:  clock.RealClock{},
	}, nil
}

type scheduledJob struct {
	job     Job
	nextRun time.Time
}

func (c *Coordinator) Run(ctx context.Context) {
	now := c.clock.Now()
	schedule := make([]scheduledJob, 0, len(c.jobs))
	for _, job := range c.jobs {
		nextRun := now.Add(job.Interval)
		if job.RunOnStart {
			nextRun = now
		}
		schedule = append(schedule, scheduledJob{job: job, nextRun: nextRun})
	}

	c.runDue(ctx, schedule)
	for ctx.Err() == nil {
		timer := c.clock.NewTimer(c.nextDelay(schedule))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C():
		}

		c.runDue(ctx, schedule)
	}
}

func (c *Coordinator) runDue(ctx context.Context, schedule []scheduledJob) {
	for index := range schedule {
		if ctx.Err() != nil {
			return
		}
		if schedule[index].nextRun.After(c.clock.Now()) {
			continue
		}

		c.runJob(ctx, schedule[index].job)
		schedule[index].nextRun = c.clock.Now().Add(schedule[index].job.Interval)
	}
}

func (c *Coordinator) runJob(ctx context.Context, job Job) {
	startedAt := c.clock.Now()
	jobContext, cancel := context.WithTimeout(ctx, job.Timeout)
	err := job.Run(jobContext)
	if err == nil {
		err = jobContext.Err()
	}
	cancel()

	duration := c.clock.Now().Sub(startedAt)
	if err != nil {
		if ctx.Err() == nil {
			c.logger.Error(
				"run maintenance job",
				"job", job.Name,
				"duration", duration,
				"error", err,
			)
		}
		return
	}

	c.logger.Debug("completed maintenance job", "job", job.Name, "duration", duration)
}

func (c *Coordinator) nextDelay(schedule []scheduledJob) time.Duration {
	nextRun := schedule[0].nextRun
	for _, item := range schedule[1:] {
		if item.nextRun.Before(nextRun) {
			nextRun = item.nextRun
		}
	}

	delay := nextRun.Sub(c.clock.Now())
	if delay < 0 {
		return 0
	}

	return delay
}
