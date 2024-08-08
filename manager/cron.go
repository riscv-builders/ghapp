package manager

import (
	"context"
	"log/slog"
	"time"
)

type Cron struct {
	Name     string
	Func     func(ctx context.Context) error
	Interval time.Duration
	Timeout  time.Duration
	Throttle int
}

func (m *Coor) Register() {
	m.cronMap = append(m.cronMap,
		Cron{
			Name:     "find_available_jobs",
			Func:     m.findAvailableJob,
			Interval: 10 * time.Second,
			Timeout:  10 * time.Second,
			Throttle: 5,
		},
		Cron{
			Name:     "do_scheduled_tasks",
			Func:     m.doScheduledTasks,
			Interval: 10 * time.Second,
			Timeout:  10 * time.Second,
			Throttle: 5,
		},
		Cron{
			Name:     "do_found_builder",
			Func:     m.doFoundBuilder,
			Interval: 10 * time.Second,
			Timeout:  10 * time.Second,
			Throttle: 5,
		},
		Cron{
			Name:     "do_builder_ready",
			Func:     m.doBuilderReady,
			Interval: 10 * time.Second,
			Timeout:  10 * time.Second,
			Throttle: 5,
		},
		Cron{
			Name:     "do_in_progress",
			Func:     m.doInProgress,
			Interval: 10 * time.Second,
			Timeout:  10 * time.Second,
			Throttle: 5,
		},
	)
}

func (m *Coor) RunCron(ctx context.Context) {
	for _, c := range m.cronMap {
		go func(pctx context.Context, c Cron) {
			slog.Info("started cron", "name", c.Name, "interval", c.Interval, "timeout", c.Timeout)

			for {
				ctx, cancel := context.WithTimeout(pctx, c.Timeout)
				start := time.Now()
				err := c.Func(ctx)
				if err != nil {
					slog.Error("cron error", "name", c.Name, "err", err)
				}
				cancel()

				if time.Since(start) > c.Interval {
					continue
				}
				time.Sleep(c.Interval - time.Since(start))
			}
		}(ctx, c)
	}
}
