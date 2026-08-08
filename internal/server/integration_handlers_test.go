package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"moviepickarr/internal/integration"
	integrationtmdb "moviepickarr/internal/integration/tmdb"
	"moviepickarr/internal/repository"

	"github.com/gofiber/fiber/v2"
)

func TestHandleGetTMDBIntegration_ReturnsTypedWriteOnlySettings(t *testing.T) {
	t.Parallel()
	_, app, _, _ := setupEditMovieTest(t)

	response := doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/integrations/tmdb", nil), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	var body struct {
		Revision int64  `json:"revision"`
		State    string `json:"state"`
		Settings struct {
			Enabled struct {
				Value       bool   `json:"value"`
				Source      string `json:"source"`
				Environment string `json:"environment"`
			} `json:"enabled"`
			APIKey struct {
				Configured  bool   `json:"configured"`
				Source      string `json:"source"`
				Environment string `json:"environment"`
			} `json:"apiKey"`
			CastLimit struct {
				Value   int    `json:"value"`
				Default int    `json:"default"`
				Source  string `json:"source"`
			} `json:"castLimit"`
		} `json:"settings"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Revision != 1 || body.State != "disabled" {
		t.Fatalf("identity/state = revision %d, state %q", body.Revision, body.State)
	}
	if body.Settings.Enabled.Environment != "TMDB_ENABLED" || body.Settings.APIKey.Environment != "TMDB_API_KEY" {
		t.Fatalf("environment labels = enabled %q, key %q", body.Settings.Enabled.Environment, body.Settings.APIKey.Environment)
	}
	if body.Settings.APIKey.Configured || body.Settings.APIKey.Source != "default" {
		t.Fatalf("secret metadata = %+v", body.Settings.APIKey)
	}
	if body.Settings.CastLimit.Value != 15 || body.Settings.CastLimit.Default != 15 || body.Settings.CastLimit.Source != "default" {
		t.Fatalf("cast limit = %+v", body.Settings.CastLimit)
	}
}

func TestHandleGetTMDBIntegration_RequiresAdmin(t *testing.T) {
	t.Parallel()
	_, app, _, _ := setupEditMovieTest(t)

	response := doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/integrations/tmdb", nil), 1, "member")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusForbidden || problemCode(t, response) != "admin_required" {
		t.Fatalf("status = %d, want admin_required", response.StatusCode)
	}
}

func TestHandleGetTMDBIntegration_ReturnsCurrentOrLatestRun(t *testing.T) {
	t.Parallel()
	_, app, _, _, pool := setupEditMovieTestWithDB(t)
	ledger := repository.NewSqliteIntegrationRunRepository(pool)
	started, err := ledger.Start(context.Background(), integration.RunStart{
		Integration: "tmdb", Operation: integration.RunOperationRefreshStale,
		Trigger: integration.RunTriggerManual, ConfigRevision: 1,
		StartedAt: time.Now().UTC(), Total: 4,
	})
	if err != nil {
		t.Fatalf("start run: %v", err)
	}

	response := doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/integrations/tmdb", nil), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	var body struct {
		LatestRun *struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"latestRun"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.LatestRun == nil || body.LatestRun.ID != started.ID || body.LatestRun.Status != "running" {
		t.Fatalf("latest run = %+v", body.LatestRun)
	}
}

func TestHandleGetTMDBIntegration_PrefersActiveLibraryRunOverNewerMovieRun(t *testing.T) {
	t.Parallel()
	_, app, _, _, pool := setupEditMovieTestWithDB(t)
	ledger := repository.NewSqliteIntegrationRunRepository(pool)
	startedAt := time.Now().UTC().Add(-time.Minute)
	libraryRun, err := ledger.Start(context.Background(), integration.RunStart{
		Integration: "tmdb", Operation: integration.RunOperationRefreshStale,
		Trigger: integration.RunTriggerManual, ConfigRevision: 1,
		StartedAt: startedAt, Total: 4,
	})
	if err != nil {
		t.Fatalf("start library run: %v", err)
	}
	if _, err := ledger.Start(context.Background(), integration.RunStart{
		Integration: "tmdb", Operation: integration.RunOperationEnrichMovie,
		Trigger: integration.RunTriggerMovieAdded, ConfigRevision: 1,
		StartedAt: startedAt.Add(time.Second), Total: 1,
	}); err != nil {
		t.Fatalf("start single-movie run: %v", err)
	}

	response := doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/integrations/tmdb", nil), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	var body struct {
		LatestRun *struct {
			ID int64 `json:"id"`
		} `json:"latestRun"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.LatestRun == nil || body.LatestRun.ID != libraryRun.ID {
		t.Fatalf("latest run = %+v, want active library run %d", body.LatestRun, libraryRun.ID)
	}
}

func TestHandleGetTMDBIntegration_ReturnsStatusTimestamps(t *testing.T) {
	t.Parallel()
	_, app, _, _, pool := setupEditMovieTestWithDB(t)
	repo := repository.NewSqliteIntegrationConfigRepository(pool)
	checkedAt := time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC)
	connectionTestedAt := checkedAt.Add(30 * time.Minute)
	nextAt := checkedAt.Add(time.Hour)
	successAt := checkedAt.Add(-time.Hour)
	if err := repo.UpdateSchedule(context.Background(), "tmdb", checkedAt, &nextAt); err != nil {
		t.Fatalf("update schedule: %v", err)
	}
	if err := repo.UpdateSuccessfulRun(context.Background(), "tmdb", successAt); err != nil {
		t.Fatalf("update successful run: %v", err)
	}
	if err := repo.UpdateConnectionTest(
		context.Background(),
		"tmdb",
		integration.StateConnected,
		"",
		connectionTestedAt,
	); err != nil {
		t.Fatalf("update connection test: %v", err)
	}

	response := doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/integrations/tmdb", nil), 1, "admin")
	defer response.Body.Close()
	var body struct {
		LastCheckedAt          string `json:"lastCheckedAt"`
		LastConnectionTestedAt string `json:"lastConnectionTestedAt"`
		NextCheckAt            string `json:"nextCheckAt"`
		LastSuccessfulRunAt    string `json:"lastSuccessfulRunAt"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.LastCheckedAt != checkedAt.Format(time.RFC3339) ||
		body.LastConnectionTestedAt != connectionTestedAt.Format(time.RFC3339) ||
		body.NextCheckAt != nextAt.Format(time.RFC3339) ||
		body.LastSuccessfulRunAt != successAt.Format(time.RFC3339) {
		t.Fatalf("timestamps = %+v", body)
	}
}

func TestHandleListIntegrations_UsesConnectionCheckAsLatestActivity(t *testing.T) {
	t.Parallel()
	_, app, _, _, pool := setupEditMovieTestWithDB(t)
	repo := repository.NewSqliteIntegrationConfigRepository(pool)
	checkedAt := time.Date(2026, time.August, 4, 19, 0, 0, 0, time.UTC)
	if err := repo.UpdateConnectionTest(context.Background(), "tmdb", integration.StateCouldNotVerify, "temporary failure", checkedAt); err != nil {
		t.Fatalf("update integration connection test: %v", err)
	}

	response := doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/integrations", nil), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	var body []struct {
		ID             string `json:"id"`
		LatestActivity string `json:"latestActivity"`
		AttentionCount int    `json:"attentionCount"`
		Operations     []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"operations"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 2 || body[0].ID != "tmdb" || body[0].LatestActivity != checkedAt.Format(time.RFC3339) {
		t.Fatalf("latest activity = %+v, want %s", body, checkedAt.Format(time.RFC3339))
	}
	if len(body[0].Operations) != 3 || body[0].Operations[0].ID != "refresh_stale" || body[0].Operations[0].Name != "Refresh stale" {
		t.Fatalf("operations = %+v, want typed TMDB operations", body[0].Operations)
	}
	if body[1].ID != "radarr" || len(body[1].Operations) != 0 || body[1].AttentionCount != 0 {
		t.Fatalf("Radarr summary = %+v, want no Runs operations and no attention", body[1])
	}
}

func TestHandleSaveTMDBIntegration_PersistsOneTypedDraft(t *testing.T) {
	t.Parallel()
	_, app, _, _ := setupEditMovieTest(t)

	response := doAs(t, app, jsonReq(http.MethodPut, "/api/v1/integrations/tmdb", `{
		"revision": 1,
		"settings": {
			"enabled": true,
			"castLimit": 30,
			"refreshIntervalMs": 7200000,
			"ttlMs": 604800000,
			"minIntervalMs": 500,
			"maxRetries": 3,
			"backoffMs": 750,
			"batchLimit": 100
		}
	}`), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	var body tmdbIntegrationResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Revision != 2 || body.Settings.CastLimit.Value != 30 {
		t.Fatalf("saved response = %+v", body)
	}
}

func TestHandleSaveTMDBIntegration_ReportsValidationAndStaleRevisions(t *testing.T) {
	t.Parallel()
	_, app, _, _ := setupEditMovieTest(t)

	invalid := doAs(t, app, jsonReq(http.MethodPut, "/api/v1/integrations/tmdb", `{
		"revision": 1,
		"settings": {"castLimit": -1, "batchLimit": 0}
	}`), 1, "admin")
	defer invalid.Body.Close()
	if invalid.StatusCode != fiber.StatusUnprocessableEntity || problemCode(t, invalid) != "validation_failed" {
		t.Fatalf("invalid status = %d, want validation_failed", invalid.StatusCode)
	}

	stale := doAs(t, app, jsonReq(http.MethodPut, "/api/v1/integrations/tmdb", `{
		"revision": 99,
		"settings": {"castLimit": 20}
	}`), 1, "admin")
	defer stale.Body.Close()
	if stale.StatusCode != fiber.StatusConflict || problemCode(t, stale) != "stale_revision" {
		t.Fatalf("stale status = %d, want stale_revision", stale.StatusCode)
	}
}

func TestHandleSaveTMDBIntegration_RequiresWarningConfirmation(t *testing.T) {
	t.Parallel()
	_, app, _, _ := setupEditMovieTest(t)

	response := doAs(t, app, jsonReq(http.MethodPut, "/api/v1/integrations/tmdb", `{
		"revision": 1,
		"settings": {"minIntervalMs": 100}
	}`), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusConflict || problemCode(t, response) != "confirmation_required" {
		t.Fatalf("status = %d, want confirmation_required", response.StatusCode)
	}
}

type integrationTestSecrets struct{}

func (integrationTestSecrets) Encrypt(value string) ([]byte, error) {
	return []byte("sealed:" + value), nil
}

func (integrationTestSecrets) Decrypt(value []byte) (string, error) {
	return string(value[len("sealed:"):]), nil
}

type integrationConnectionTester struct {
	config integrationtmdb.RuntimeConfig
	err    error
}

func (tester *integrationConnectionTester) TestConnection(_ context.Context, config integrationtmdb.RuntimeConfig) error {
	tester.config = config
	return tester.err
}

func TestHandleTestTMDBConnection_UsesDraftWithoutSavingOrReturningSecret(t *testing.T) {
	t.Parallel()
	h, app, _, _, pool := setupEditMovieTestWithDB(t)
	tester := &integrationConnectionTester{}
	h.tmdbIntegration = integrationtmdb.NewService(
		repository.NewSqliteIntegrationConfigRepository(pool),
		integrationTestSecrets{},
		integrationtmdb.EnvironmentConfig{},
		tester,
		nil,
	)

	response := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/integrations/tmdb/test", `{
		"revision": 1,
		"apiKey": "draft-secret",
		"settings": {"castLimit": 21}
	}`), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["state"] != string(integration.StateConnected) || tester.config.APIKey != "draft-secret" || tester.config.CastLimit != 21 {
		t.Fatalf("test response/config = body %#v, config %+v", body, tester.config)
	}
	if _, leaked := body["apiKey"]; leaked {
		t.Fatal("connection response returned the API key")
	}

	view, err := h.tmdbIntegration.Get(context.Background())
	if err != nil {
		t.Fatalf("read unchanged config: %v", err)
	}
	if view.Revision != 1 || view.Config.APIKey.Configured {
		t.Fatalf("test connection persisted draft: %+v", view)
	}
}

func TestHandleSaveTMDBIntegration_NewKeyIsWriteOnlyOverHTTP(t *testing.T) {
	t.Parallel()
	h, app, _, _, pool := setupEditMovieTestWithDB(t)
	configRepo := repository.NewSqliteIntegrationConfigRepository(pool)
	h.tmdbIntegration = integrationtmdb.NewService(
		configRepo,
		integrationTestSecrets{},
		integrationtmdb.EnvironmentConfig{},
		&integrationConnectionTester{},
		nil,
	)
	h.tmdbRuns = nil
	h.tmdbScheduler = newTMDBRunScheduler(
		context.Background(), h.tmdbIntegration, nil, configRepo, nil, nil,
	)
	ledger := repository.NewSqliteIntegrationRunRepository(pool)
	startedAt := time.Date(2026, time.August, 4, 16, 0, 0, 0, time.UTC)
	run, err := ledger.Start(context.Background(), integration.RunStart{
		Integration: "tmdb", Operation: integration.RunOperationEnrichMovie,
		Trigger: integration.RunTriggerMovieAdded, ConfigRevision: 1,
		StartedAt: startedAt, Total: 1,
	})
	if err != nil {
		t.Fatalf("seed latest run: %v", err)
	}
	if _, err := ledger.Finish(context.Background(), run.ID, integration.RunFinish{
		Status: integration.RunStatusCompleted, FinishedAt: startedAt.Add(time.Second),
		Progress: integration.RunProgress{Total: 1, Processed: 1, Succeeded: 1},
	}); err != nil {
		t.Fatalf("finish latest run: %v", err)
	}

	response := doAs(t, app, jsonReq(http.MethodPut, "/api/v1/integrations/tmdb", `{
		"revision": 1,
		"apiKey": "never-return-this-secret",
		"settings": {"enabled": true}
	}`), 1, "admin")
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, payload)
	}
	if strings.Contains(string(payload), "never-return-this-secret") {
		t.Fatalf("secret leaked in response: %s", payload)
	}
	var body struct {
		NextCheckAt string                  `json:"nextCheckAt"`
		LatestRun   *integrationRunResponse `json:"latestRun"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode save response: %v", err)
	}
	if body.NextCheckAt == "" {
		t.Fatal("save response did not include the rescheduled next check")
	}
	if body.LatestRun == nil || body.LatestRun.ID != run.ID {
		t.Fatalf("save response latest run = %+v, want run %d", body.LatestRun, run.ID)
	}
}

func TestHandleSaveTMDBIntegration_AttributesFirstKeyRefreshToAdmin(t *testing.T) {
	t.Parallel()
	h, app, _, _, pool := setupEditMovieTestWithDB(t)
	configRepo := repository.NewSqliteIntegrationConfigRepository(pool)
	service := integrationtmdb.NewService(
		configRepo,
		integrationTestSecrets{},
		integrationtmdb.EnvironmentConfig{},
		&integrationConnectionTester{},
		nil,
	)
	ledger := &recordingTMDBRunLedger{}
	controller := newTMDBRunController(
		context.Background(),
		&recordingTMDBRunCandidates{refresh: []tmdbRunSubject{{MovieID: 603, Label: "The Matrix"}}},
		&recordingTMDBRunEnricher{},
		service,
		ledger,
		nil,
		nil,
	)
	t.Cleanup(controller.Close)
	h.tmdbIntegration = service
	h.tmdbRuns = controller
	h.tmdbScheduler = &recordingTMDBScheduler{}
	h.posterWall = nil

	response := doAs(t, app, jsonReq(http.MethodPut, "/api/v1/integrations/tmdb", `{
		"revision": 1,
		"apiKey": "first-key",
		"settings": {"enabled": true}
	}`), 27, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, body = %s", response.StatusCode, payload)
	}
	if ledger.start.InitiatedBy == nil || *ledger.start.InitiatedBy != 27 {
		t.Fatalf("configuration run initiated by = %v, want admin 27", ledger.start.InitiatedBy)
	}
}

func TestHandleSaveAndTestTMDBIntegration_RequireAdmin(t *testing.T) {
	t.Parallel()
	_, app, _, _ := setupEditMovieTest(t)
	for _, endpoint := range []struct {
		method string
		path   string
	}{
		{http.MethodPut, "/api/v1/integrations/tmdb"},
		{http.MethodPost, "/api/v1/integrations/tmdb/test"},
		{http.MethodPost, "/api/v1/integrations/tmdb/runs"},
	} {
		response := doAs(t, app, jsonReq(endpoint.method, endpoint.path, `{}`), 1, "member")
		if response.StatusCode != fiber.StatusForbidden || problemCode(t, response) != "admin_required" {
			t.Fatalf("%s %s status = %d, want admin_required", endpoint.method, endpoint.path, response.StatusCode)
		}
		_ = response.Body.Close()
	}
}
