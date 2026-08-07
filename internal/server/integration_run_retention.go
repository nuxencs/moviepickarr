package server

import (
	"context"
	"sync"
	"time"
)

const integrationRunRetentionInterval = 24 * time.Hour

type integrationRunRetentionPruner interface {
	Prune(context.Context, time.Time) (int64, error)
}

type integrationRunRetentionTicker interface {
	C() <-chan time.Time
	Stop()
}

type systemIntegrationRunRetentionTicker struct {
	*time.Ticker
}

func (t systemIntegrationRunRetentionTicker) C() <-chan time.Time { return t.Ticker.C }

type integrationRunRetention struct {
	cancel   context.CancelFunc
	pruner   integrationRunRetentionPruner
	ticker   integrationRunRetentionTicker
	now      func() time.Time
	onError  func(error)
	onPruned func(int64)

	closeOnce sync.Once
	wg        sync.WaitGroup
}

func newIntegrationRunRetention(
	parent context.Context,
	pruner integrationRunRetentionPruner,
	tickerFactory func(time.Duration) integrationRunRetentionTicker,
	now func() time.Time,
	onError func(error),
	onPruned func(int64),
) *integrationRunRetention {
	if parent == nil {
		parent = context.Background()
	}
	if tickerFactory == nil {
		tickerFactory = func(interval time.Duration) integrationRunRetentionTicker {
			return systemIntegrationRunRetentionTicker{Ticker: time.NewTicker(interval)}
		}
	}
	if now == nil {
		now = time.Now
	}
	ctx, cancel := context.WithCancel(parent)
	retention := &integrationRunRetention{
		cancel:   cancel,
		pruner:   pruner,
		ticker:   tickerFactory(integrationRunRetentionInterval),
		now:      now,
		onError:  onError,
		onPruned: onPruned,
	}
	retention.wg.Add(1)
	go retention.run(ctx)
	return retention
}

func (r *integrationRunRetention) run(ctx context.Context) {
	defer r.wg.Done()
	defer r.ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.ticker.C():
			removed, err := r.pruner.Prune(ctx, r.now().UTC())
			if err != nil {
				if ctx.Err() == nil && r.onError != nil {
					r.onError(err)
				}
				continue
			}
			if removed > 0 && r.onPruned != nil {
				r.onPruned(removed)
			}
		}
	}
}

func (r *integrationRunRetention) Close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(r.cancel)
	r.wg.Wait()
}
