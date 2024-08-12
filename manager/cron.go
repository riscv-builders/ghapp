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
}

func (m *Coor) Register() {
	m.cronMap = append(m.cronMap,
		Cron{
			Name:     "find_available_jobs",
			Func:     m.findAvailableJob,
			Interval: time.Second,
			Timeout:  10 * time.Second,
		},
		Cron{
			Name:     "do_scheduled_tasks",
			Func:     m.doPendingTask,
			Interval: time.Second,
			Timeout:  10 * time.Second,
		},
		Cron{
			Name:     "do_found_builder",
			Func:     m.doFoundBuilder,
			Interval: time.Second,
			Timeout:  10 * time.Minute,
		},
		Cron{
			Name:     "do_builder_ready",
			Func:     m.doBuilderReady,
			Interval: time.Second,
			Timeout:  10 * time.Second,
		},
		Cron{
			Name:     "do_in_progress",
			Func:     m.doInProgress,
			Interval: time.Second,
			Timeout:  10 * time.Second,
		},
	)
}

func (m *Coor) RunCron(pctx context.Context) {
	for {
		for _, c := range m.cronMap {
			slog.Info("started cron", "name", c.Name, "interval", c.Interval, "timeout", c.Timeout)
			ctx, cancel := context.WithTimeout(pctx, c.Timeout)
			start := time.Now()
			err := c.Func(ctx)
			if err != nil {
				slog.Error("cron error", "name", c.Name, "err", err)
			}
			cancel()

			slog.Debug("cron exec", "name", c.Name, "time", time.Since(start))
			if c.Interval < time.Since(start) {
				continue
			}
			time.Sleep(c.Interval - time.Since(start))
		}
	}
}
