package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"moviepickarr/internal/integration"
	"moviepickarr/internal/repository"

	"github.com/gofiber/fiber/v2"
)

type blockingFirstRadarrWebhookSender struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   []string
}

func (s *blockingFirstRadarrWebhookSender) Send(_ context.Context, endpoint string, _ any) error {
	s.mu.Lock()
	s.calls = append(s.calls, endpoint)
	first := len(s.calls) == 1
	if first {
		close(s.started)
	}
	s.mu.Unlock()
	if first {
		<-s.release
	}
	return nil
}

func TestValidateRadarrReasonsRejectsUnknownValues(t *testing.T) {
	t.Parallel()
	normalized, err := validateRadarrReasons([]string{" release_required ", "release_required"})
	if err != nil || len(normalized) != 1 || normalized[0] != "release_required" {
		t.Fatalf("normalize valid reasons = %v, %v", normalized, err)
	}

	for _, reasons := range [][]string{
		{"not_actionable"},
		{"release_required", "not_actionable"},
	} {
		_, err := validateRadarrReasons(reasons)
		var fieldErr *radarrFieldError
		if !errors.As(err, &fieldErr) || fieldErr.Field != "reasons" {
			t.Fatalf("validate reasons %v = %v, want reasons field error", reasons, err)
		}
	}
}

func TestRadarrWebhookHTTPRejectsUnknownReasons(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		path    string
		reasons string
	}{
		{name: "create with unknown only", path: "/api/v1/integrations/radarr/webhooks", reasons: `["not_actionable"]`},
		{name: "create with mixed valid and unknown", path: "/api/v1/integrations/radarr/webhooks", reasons: `["release_required","not_actionable"]`},
		{name: "draft test with unknown only", path: "/api/v1/integrations/radarr/webhooks/test", reasons: `["not_actionable"]`},
		{name: "draft test with mixed valid and unknown", path: "/api/v1/integrations/radarr/webhooks/test", reasons: `["release_required","not_actionable"]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, app, _, _ := setupEditMovieTest(t)
			response := doAs(t, app, jsonReq(http.MethodPost, test.path, `{
				"name":"Operations",
				"format":"generic",
				"url":"https://hooks.example.test/moviepickarr",
				"enabled":false,
				"reasons":`+test.reasons+`
			}`), 1, "admin")
			defer response.Body.Close()
			if response.StatusCode != fiber.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d", response.StatusCode, fiber.StatusUnprocessableEntity)
			}
			var problem radarrProblemResponse
			if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if len(problem.Issues) != 1 || problem.Issues[0].Field != "reasons" {
				t.Fatalf("problem issues = %+v, want reasons field", problem.Issues)
			}
		})
	}
}

func TestSavedWebhookTestCannotVerifyAConcurrentEndpointReplacement(t *testing.T) {
	env := setupRadarrAcquisitionServiceTest(t, "manual", nil)
	requestStarted := make(chan struct{})
	releaseResponse := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseResponse) }) }
	defer release()

	endpoint := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseResponse
		response.WriteHeader(http.StatusNoContent)
	}))
	defer endpoint.Close()

	destination, err := env.service.saveWebhookDestination(env.ctx, nil, radarrWebhookDraft{
		Name: "Operations", Kind: "generic", URL: endpoint.URL,
		Reasons: []string{"release_required"},
	})
	if err != nil {
		t.Fatalf("save webhook destination: %v", err)
	}

	testResult := make(chan error, 1)
	go func() {
		_, testErr := env.service.testWebhookDestination(env.ctx, destination.ID)
		testResult <- testErr
	}()
	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("saved webhook test did not reach the original endpoint")
	}

	updated, err := env.service.saveWebhookDestination(env.ctx, &destination.ID, radarrWebhookDraft{
		Name: "Operations", Kind: "generic", URL: "https://replacement.example.test/hook",
		Reasons: []string{"release_required"}, Revision: destination.Revision,
	})
	if err != nil {
		t.Fatalf("replace webhook endpoint: %v", err)
	}
	if updated.Revision == destination.Revision || updated.VerifiedAt != nil {
		t.Fatalf("replacement destination = %+v, want a newer unverified revision", updated)
	}

	release()
	select {
	case err := <-testResult:
		if !errors.Is(err, integration.ErrStaleRevision) {
			t.Fatalf("stale saved webhook test = %v, want stale revision", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("saved webhook test did not finish")
	}

	current, err := env.repo.GetWebhookDestination(env.ctx, destination.ID)
	if err != nil {
		t.Fatalf("read replacement webhook destination: %v", err)
	}
	if current.Revision != updated.Revision || current.VerifiedAt != nil {
		t.Fatalf("destination after stale test = %+v, want replacement to remain unverified", current)
	}
}

func TestRadarrWebhookWorkerRunsStartupMaintenance(t *testing.T) {
	e := setupRadarrAcquisitionServiceTest(t, "manual", nil)
	received := make(chan struct{}, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
		select {
		case received <- struct{}{}:
		default:
		}
	}))
	defer endpoint.Close()

	verifiedAt := e.now
	destination, err := e.repo.CreateWebhookDestination(e.ctx, repository.RadarrWebhookDestinationSave{
		Name:          "Operations",
		Kind:          "generic",
		EncryptedURL:  []byte(endpoint.URL),
		ReasonFilters: []string{"identity_required"},
		Enabled:       true,
		VerifiedAt:    &verifiedAt,
	})
	if err != nil {
		t.Fatalf("create webhook destination: %v", err)
	}
	acquisition, err := e.repo.TransitionAcquisition(e.ctx, e.acquisitionID, repository.RadarrAcquisitionTransition{
		Status:       "action_needed",
		ActionReason: "identity_required",
		At:           e.now.Add(30 * time.Second),
	})
	if err != nil {
		t.Fatalf("create due delivery: %v", err)
	}
	var deliveryID int64
	if err := e.pool.Read.QueryRowContext(e.ctx, `
		SELECT id FROM radarr_webhook_deliveries
		WHERE destination_id = ? AND acquisition_id = ? AND action_version = ?
	`, destination.ID, e.acquisitionID, acquisition.ActionVersion).Scan(&deliveryID); err != nil {
		t.Fatalf("read due delivery: %v", err)
	}
	if _, err := e.pool.Write.ExecContext(e.ctx, `
		UPDATE radarr_webhook_deliveries
		SET status = 'sending', claim_version = 1, claim_expires_at = ?
		WHERE id = ?
	`, e.now.Add(30*time.Second).Unix(), deliveryID); err != nil {
		t.Fatalf("seed expired delivery claim: %v", err)
	}

	oldDeliveredAt := e.now.Add(-31 * 24 * time.Hour)
	oldResult, err := e.pool.Write.ExecContext(e.ctx, `
		INSERT INTO radarr_webhook_deliveries (
		    destination_id, destination_revision, acquisition_id,
		    reason, action_version, status, next_attempt_at, delivered_at
		) VALUES (?, ?, ?, 'identity_required', ?, 'delivered', ?, ?)
	`, destination.ID, destination.Revision, e.acquisitionID,
		acquisition.ActionVersion+100, oldDeliveredAt.Unix(), oldDeliveredAt.Unix())
	if err != nil {
		t.Fatalf("insert expired delivery: %v", err)
	}
	oldDeliveryID, err := oldResult.LastInsertId()
	if err != nil {
		t.Fatalf("read expired delivery id: %v", err)
	}

	workerErrors := make(chan error, 1)
	worker := newRadarrWebhookWorker(context.Background(), e.service, func(err error) {
		workerErrors <- err
	})
	defer worker.Close()

	select {
	case <-received:
	case err := <-workerErrors:
		t.Fatalf("startup maintenance failed: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("startup maintenance did not deliver the due webhook")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		var status string
		if err := e.pool.Read.QueryRowContext(e.ctx,
			`SELECT status FROM radarr_webhook_deliveries WHERE id = ?`, deliveryID,
		).Scan(&status); err != nil {
			t.Fatalf("read startup delivery status: %v", err)
		}
		var oldCount int
		if err := e.pool.Read.QueryRowContext(e.ctx,
			`SELECT COUNT(*) FROM radarr_webhook_deliveries WHERE id = ?`, oldDeliveryID,
		).Scan(&oldCount); err != nil {
			t.Fatalf("count expired delivery: %v", err)
		}
		if status == "delivered" && oldCount == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("startup state = status %q, expired count %d", status, oldCount)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRadarrWebhookBatchSkipsDeliveryMadeObsoleteWhileEarlierSendBlocks(t *testing.T) {
	e := setupRadarrAcquisitionServiceTest(t, "manual", nil)
	verifiedAt := e.now
	for _, destination := range []struct {
		name string
		url  string
	}{
		{name: "First", url: "https://hooks.example.test/first"},
		{name: "Second", url: "https://hooks.example.test/second"},
	} {
		if _, err := e.repo.CreateWebhookDestination(e.ctx, repository.RadarrWebhookDestinationSave{
			Name: destination.name, Kind: "generic", EncryptedURL: []byte(destination.url),
			ReasonFilters: []string{"identity_required"}, Enabled: true, VerifiedAt: &verifiedAt,
		}); err != nil {
			t.Fatalf("create %s webhook destination: %v", destination.name, err)
		}
	}
	if _, err := e.repo.TransitionAcquisition(e.ctx, e.acquisitionID, repository.RadarrAcquisitionTransition{
		Status: "action_needed", ActionReason: "identity_required", At: e.now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("create actionable delivery batch: %v", err)
	}
	due, err := e.repo.DueWebhookDeliveries(e.ctx, e.now.Add(time.Hour), 10)
	if err != nil || len(due) != 2 {
		t.Fatalf("due delivery batch = %+v, err=%v", due, err)
	}

	sender := &blockingFirstRadarrWebhookSender{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	type deliveryResult struct {
		processed int
		err       error
	}
	result := make(chan deliveryResult, 1)
	go func() {
		processed, err := e.service.deliverDueWebhooks(e.ctx, sender, 10)
		result <- deliveryResult{processed: processed, err: err}
	}()
	select {
	case <-sender.started:
	case <-time.After(3 * time.Second):
		close(sender.release)
		t.Fatal("first webhook send did not block")
	}
	if err := e.repo.ArchiveWebhookDestination(
		e.ctx, due[1].DestinationID, time.Now().UTC().Add(time.Minute),
	); err != nil {
		close(sender.release)
		t.Fatalf("archive second destination: %v", err)
	}
	close(sender.release)

	select {
	case delivered := <-result:
		if delivered.err != nil || delivered.processed != 1 {
			t.Fatalf("delivery result = processed %d, err=%v", delivered.processed, delivered.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("webhook delivery batch did not finish")
	}
	if len(sender.calls) != 1 || sender.calls[0] != string(due[0].EncryptedURL) {
		t.Fatalf("outbound webhook calls = %+v, want only first delivery", sender.calls)
	}
	var secondStatus string
	if err := e.pool.Read.QueryRowContext(e.ctx,
		`SELECT status FROM radarr_webhook_deliveries WHERE id = ?`, due[1].ID,
	).Scan(&secondStatus); err != nil {
		t.Fatalf("read obsolete delivery status: %v", err)
	}
	if secondStatus != "terminal_failed" {
		t.Fatalf("obsolete delivery status = %q, want terminal_failed", secondStatus)
	}
}

func TestConcurrentRadarrWebhookDispatchersSendOnce(t *testing.T) {
	e := setupRadarrAcquisitionServiceTest(t, "manual", nil)
	verifiedAt := e.now
	if _, err := e.repo.CreateWebhookDestination(e.ctx, repository.RadarrWebhookDestinationSave{
		Name: "Operations", Kind: "generic", EncryptedURL: []byte("https://hooks.example.test/ops"),
		ReasonFilters: []string{"identity_required"}, Enabled: true, VerifiedAt: &verifiedAt,
	}); err != nil {
		t.Fatalf("create webhook destination: %v", err)
	}
	if _, err := e.repo.TransitionAcquisition(e.ctx, e.acquisitionID, repository.RadarrAcquisitionTransition{
		Status: "action_needed", ActionReason: "identity_required", At: e.now.Add(30 * time.Second),
	}); err != nil {
		t.Fatalf("create due delivery: %v", err)
	}

	sender := &blockingFirstRadarrWebhookSender{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	type deliveryResult struct {
		processed int
		err       error
	}
	firstResult := make(chan deliveryResult, 1)
	go func() {
		processed, err := e.service.deliverDueWebhooks(e.ctx, sender, 10)
		firstResult <- deliveryResult{processed: processed, err: err}
	}()
	select {
	case <-sender.started:
	case <-time.After(3 * time.Second):
		close(sender.release)
		t.Fatal("first webhook dispatcher did not reach the sender")
	}

	processed, err := e.service.deliverDueWebhooks(e.ctx, sender, 10)
	if err != nil || processed != 0 {
		close(sender.release)
		t.Fatalf("concurrent dispatcher = processed %d, err=%v", processed, err)
	}
	close(sender.release)
	select {
	case result := <-firstResult:
		if result.err != nil || result.processed != 1 {
			t.Fatalf("claim holder = processed %d, err=%v", result.processed, result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("claim holder did not finish")
	}
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.calls) != 1 {
		t.Fatalf("webhook sends = %d, want 1", len(sender.calls))
	}
}
