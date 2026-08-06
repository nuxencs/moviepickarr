package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"moviepickarr/internal/integration"
	integrationtmdb "moviepickarr/internal/integration/tmdb"

	"github.com/gofiber/fiber/v2"
)

type actionRunCandidates struct {
	subjects []tmdbRunSubject
}

func (s actionRunCandidates) RefreshStale(context.Context, time.Time, int) ([]tmdbRunSubject, error) {
	return s.subjects, nil
}

func (s actionRunCandidates) ReEnrichAll(context.Context) ([]tmdbRunSubject, error) {
	return s.subjects, nil
}

type actionRunConfig struct{}

func (actionRunConfig) Acquire(context.Context) (integrationtmdb.RuntimeSnapshot, error) {
	return integrationtmdb.RuntimeSnapshot{
		Revision: 5,
		Config: integrationtmdb.RuntimeConfig{
			Enabled: true, APIKey: "configured", TTL: 24 * time.Hour, BatchLimit: 10,
		},
	}, nil
}

type blockingActionEnricher struct {
	started chan struct{}
	release chan struct{}
}

func (e *blockingActionEnricher) EnrichOne(
	_ context.Context,
	_ integrationtmdb.RuntimeSnapshot,
	_ int,
) (enrichResult, error) {
	select {
	case e.started <- struct{}{}:
	default:
	}
	<-e.release
	return enrichResult{}, nil
}

type actionRunLedger struct {
	integration.RunLedger
	finished chan integration.RunFinish
}

func (l *actionRunLedger) Start(_ context.Context, start integration.RunStart) (*integration.Run, error) {
	return &integration.Run{
		ID: 77, Integration: start.Integration, Operation: start.Operation,
		Trigger: start.Trigger, InitiatedBy: start.InitiatedBy,
		ConfigRevision: start.ConfigRevision, Status: integration.RunStatusRunning,
		StartedAt: start.StartedAt,
		Progress:  integration.RunProgress{Total: start.Total, Remaining: start.Total},
	}, nil
}

func (l *actionRunLedger) Update(context.Context, int64, integration.RunProgress) error { return nil }

func (l *actionRunLedger) Finish(_ context.Context, _ int64, finish integration.RunFinish) (*integration.Run, error) {
	l.finished <- finish
	return &integration.Run{ID: 77, Status: finish.Status}, nil
}

func TestHandleTMDBRunActions_StartsAndCooperativelyCancels(t *testing.T) {
	t.Parallel()
	ledger := &actionRunLedger{finished: make(chan integration.RunFinish, 1)}
	enricher := &blockingActionEnricher{started: make(chan struct{}, 1), release: make(chan struct{})}
	controller := newTMDBRunController(
		context.Background(),
		actionRunCandidates{subjects: []tmdbRunSubject{{MovieID: 1}, {MovieID: 2}}},
		enricher,
		actionRunConfig{},
		ledger,
		nil,
		nil,
	)
	t.Cleanup(controller.Close)
	h := &handler{tmdbRuns: controller}
	app := fiber.New()
	mountTestV1(app, h)

	start := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/integrations/tmdb/runs", `{"operation":"refresh_stale"}`), 9, "admin")
	defer start.Body.Close()
	if start.StatusCode != fiber.StatusAccepted {
		t.Fatalf("start status = %d, want 202", start.StatusCode)
	}
	var started integrationRunResponse
	if err := json.NewDecoder(start.Body).Decode(&started); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	if started.ID != 77 || started.InitiatedBy == nil || *started.InitiatedBy != 9 {
		t.Fatalf("started run = %+v", started)
	}
	select {
	case <-enricher.started:
	case <-time.After(time.Second):
		t.Fatal("run did not start work")
	}

	cancel := doAs(t, app, jsonReq(http.MethodDelete, "/api/v1/integration-runs/77", ""), 9, "admin")
	defer cancel.Body.Close()
	if cancel.StatusCode != fiber.StatusAccepted {
		t.Fatalf("cancel status = %d, want 202", cancel.StatusCode)
	}
	close(enricher.release)
	select {
	case finish := <-ledger.finished:
		if finish.Status != integration.RunStatusCancelled || finish.Progress.Processed != 1 || finish.Progress.Remaining != 1 {
			t.Fatalf("cancelled finish = %+v", finish)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled run did not finish")
	}
}

func TestHandleStartTMDBRun_RequiresConfirmationForReEnrichAll(t *testing.T) {
	t.Parallel()
	h := &handler{}
	app := fiber.New()
	mountTestV1(app, h)

	response := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/integrations/tmdb/runs", `{"operation":"re_enrich_all"}`), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusConflict || problemCode(t, response) != "confirmation_required" {
		t.Fatalf("status = %d, want confirmation_required", response.StatusCode)
	}
}
