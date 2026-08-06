package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeRetentionTicker struct {
	ticks chan time.Time
}

func (t *fakeRetentionTicker) C() <-chan time.Time { return t.ticks }
func (t *fakeRetentionTicker) Stop()               {}

type recordingRetentionPruner struct {
	mu     sync.Mutex
	nows   []time.Time
	err    error
	pruned int64
	called chan struct{}
}

func (p *recordingRetentionPruner) Prune(_ context.Context, now time.Time) (int64, error) {
	p.mu.Lock()
	p.nows = append(p.nows, now)
	p.mu.Unlock()
	select {
	case p.called <- struct{}{}:
	default:
	}
	return p.pruned, p.err
}

func TestIntegrationRunRetention_PrunesDailyWhileProcessStaysRunning(t *testing.T) {
	t.Parallel()
	ticker := &fakeRetentionTicker{ticks: make(chan time.Time, 1)}
	pruner := &recordingRetentionPruner{pruned: 7, called: make(chan struct{}, 1)}
	now := time.Date(2026, time.August, 5, 2, 0, 0, 0, time.UTC)
	results := make(chan int64, 1)
	retention := newIntegrationRunRetention(
		context.Background(),
		pruner,
		func(time.Duration) integrationRunRetentionTicker { return ticker },
		func() time.Time { return now },
		nil,
		func(removed int64) { results <- removed },
	)
	t.Cleanup(retention.Close)

	ticker.ticks <- now
	select {
	case <-pruner.called:
	case <-time.After(time.Second):
		t.Fatal("daily retention did not prune")
	}
	if removed := <-results; removed != 7 {
		t.Fatalf("removed = %d, want 7", removed)
	}
	pruner.mu.Lock()
	defer pruner.mu.Unlock()
	if len(pruner.nows) != 1 || !pruner.nows[0].Equal(now) {
		t.Fatalf("prune times = %v, want %v", pruner.nows, now)
	}
}

func TestIntegrationRunRetention_ReportsPruneFailure(t *testing.T) {
	t.Parallel()
	ticker := &fakeRetentionTicker{ticks: make(chan time.Time, 1)}
	wantErr := errors.New("retention failed")
	pruner := &recordingRetentionPruner{err: wantErr, called: make(chan struct{}, 1)}
	errorsSeen := make(chan error, 1)
	retention := newIntegrationRunRetention(
		context.Background(),
		pruner,
		func(time.Duration) integrationRunRetentionTicker { return ticker },
		time.Now,
		func(err error) { errorsSeen <- err },
		nil,
	)
	t.Cleanup(retention.Close)

	ticker.ticks <- time.Now()
	select {
	case err := <-errorsSeen:
		if !errors.Is(err, wantErr) {
			t.Fatalf("reported error = %v, want %v", err, wantErr)
		}
	case <-time.After(time.Second):
		t.Fatal("retention error was not reported")
	}
}
