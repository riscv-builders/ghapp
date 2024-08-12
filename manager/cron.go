package manager

import (
	"context"
	"time"
)

type Cron struct {
	Name     string
	Func     func(ctx context.Context) error
	Interval time.Duration
	Timeout  time.Duration
}

func (m *Coor) Register() {
	m.cronMap = append(m.cronMap)
}

func (m *Coor) RunCron(pctx context.Context) {
	for {
		<-pctx.Done()
	}
}
