package server

import (
	"net/http/httptest"
	"testing"
	"time"

	"moviepickarr/internal/integration"
	integrationtmdb "moviepickarr/internal/integration/tmdb"

	"github.com/gofiber/fiber/v2"
)

func TestWireTMDBRunEnriched_UsesTheRunnerBatchNotifier(t *testing.T) {
	t.Parallel()
	broker := newEventBroker()
	client, _ := broker.Subscribe()
	defer broker.Unsubscribe(client)
	runner, statsInvalidations := newBatchRunner(broker, time.Second, time.Second)
	controller := &tmdbRunController{}

	wireTMDBRunEnriched(controller, runner)
	controller.notifyEnriched(603)
	runner.flushBatch()

	if got := statsInvalidations.Load(); got != 1 {
		t.Fatalf("stats invalidations = %d, want one batched invalidation", got)
	}
	if got := countBatchEvents(client, 50*time.Millisecond); got != 1 {
		t.Fatalf("enriched-batch events = %d, want one", got)
	}
}

type recordingTMDBScheduler struct {
	starts         int
	reconfigures   int
	rejections     []int64
	closes         int
	reconfigureErr error
}

func TestApplyTMDBConnectionResult_ResumesOrPausesTheSchedule(t *testing.T) {
	t.Parallel()
	scheduler := &recordingTMDBScheduler{}
	h := &handler{tmdbScheduler: scheduler}
	app := fiber.New()
	app.Get("/connected-resumed", func(c *fiber.Ctx) error {
		h.applyTMDBConnectionResult(c, integrationtmdb.ConnectionResult{
			State:          integration.StateConnected,
			RuntimeResumed: true,
		})
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/connected-unchanged", func(c *fiber.Ctx) error {
		h.applyTMDBConnectionResult(c, integrationtmdb.ConnectionResult{State: integration.StateConnected})
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/rejected", func(c *fiber.Ctx) error {
		h.applyTMDBConnectionResult(c, integrationtmdb.ConnectionResult{
			State:           integration.StateError,
			Reason:          "API key rejected",
			RuntimeRevision: 7,
		})
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/temporary", func(c *fiber.Ctx) error {
		h.applyTMDBConnectionResult(c, integrationtmdb.ConnectionResult{State: integration.StateCouldNotVerify})
		return c.SendStatus(fiber.StatusNoContent)
	})

	for _, path := range []string{"/connected-resumed", "/connected-unchanged", "/rejected", "/temporary"} {
		response, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatalf("request %s: %v", path, err)
		}
		_ = response.Body.Close()
	}
	if scheduler.reconfigures != 1 || len(scheduler.rejections) != 1 || scheduler.rejections[0] != 7 {
		t.Fatalf(
			"scheduler calls = reconfigure:%d reject:%v, want one revision 7 rejection",
			scheduler.reconfigures,
			scheduler.rejections,
		)
	}
}

func (s *recordingTMDBScheduler) Start() error {
	s.starts++
	return nil
}

func (s *recordingTMDBScheduler) Reconfigure() error {
	s.reconfigures++
	return s.reconfigureErr
}

func (s *recordingTMDBScheduler) AuthenticationRejected(revision int64) error {
	s.rejections = append(s.rejections, revision)
	return nil
}

func (s *recordingTMDBScheduler) Close() {
	s.closes++
}

func TestApplyTMDBRuntimeEffects_ReconfiguresOnlyWhenScheduleChanged(t *testing.T) {
	t.Parallel()
	scheduler := &recordingTMDBScheduler{}
	h := &handler{tmdbScheduler: scheduler}
	app := fiber.New()
	app.Get("/unchanged", func(c *fiber.Ctx) error {
		h.applyTMDBRuntimeEffects(c, integrationtmdb.ConfigView{})
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/changed", func(c *fiber.Ctx) error {
		h.applyTMDBRuntimeEffects(c, integrationtmdb.ConfigView{
			Effects: integrationtmdb.RuntimeEffects{Reschedule: true},
		})
		return c.SendStatus(fiber.StatusNoContent)
	})

	for _, path := range []string{"/unchanged", "/changed"} {
		response, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatalf("request %s: %v", path, err)
		}
		_ = response.Body.Close()
	}
	if scheduler.reconfigures != 1 {
		t.Fatalf("scheduler reconfigures = %d, want 1", scheduler.reconfigures)
	}
}

func TestHandlerClose_ClosesSchedulerBeforeRunController(t *testing.T) {
	t.Parallel()
	scheduler := &recordingTMDBScheduler{}
	h := &handler{tmdbScheduler: scheduler}

	h.Close()
	h.Close()

	if scheduler.closes != 2 {
		t.Fatalf("scheduler closes = %d, want handler Close to remain safe", scheduler.closes)
	}
}
