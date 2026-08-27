package server

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"moviepickarr/internal/domain"
	integrationradarr "moviepickarr/internal/integration/radarr"
	"moviepickarr/internal/repository"

	"github.com/gofiber/fiber/v2"
)

func TestRadarrAdminRoutesAreRegistered(t *testing.T) {
	t.Parallel()
	app := fiber.New()
	registerV1Routes(app.Group("/api/v1"), &handler{})

	registered := make(map[string]bool)
	for _, route := range app.GetRoutes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, route := range radarrRouteContract() {
		if !registered[route] {
			t.Errorf("Radarr frontend route %q is not registered", route)
		}
	}
}

func TestRadarrAdminRoutesRejectMembersBeforeUsingService(t *testing.T) {
	t.Parallel()
	app := fiber.New()
	h := &handler{}
	if h.radarr != nil {
		t.Fatal("zero handler has a Radarr service")
	}
	mountTestV1(app, h)

	for _, route := range radarrRouteContract() {
		method, path, _ := strings.Cut(route, " ")
		path = strings.ReplaceAll(path, ":id", "1")
		path = strings.ReplaceAll(path, ":resultId", "opaque-result")
		request := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		response := doAs(t, app, request, 7, "member")
		status := response.StatusCode
		code := problemCode(t, response)
		if err := response.Body.Close(); err != nil {
			t.Fatalf("%s close response: %v", route, err)
		}
		if status != fiber.StatusForbidden || code != "admin_required" {
			t.Fatalf("%s status = %d, want admin_required", route, status)
		}
	}
}

func TestRadarrAttentionAndAcquisitionListStayConcealedUntilReveal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h, app, users, movies := setupEditMovieTest(t)
	admin, err := users.Create(ctx, "Admin")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	movie, err := movies.Add(ctx, "Arrival", "pool", admin.ID)
	if err != nil {
		t.Fatalf("add movie: %v", err)
	}
	tmdbID := 329865
	if err := movies.SetExternalIDs(ctx, movie.ID, &tmdbID, nil); err != nil {
		t.Fatalf("set movie identity: %v", err)
	}
	if _, err := h.movieService.DrawRandom(ctx, "drawer"); err != nil {
		t.Fatalf("draw movie: %v", err)
	}

	assertRadarrAttention(t, app, 0)
	response := doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/integrations/radarr/acquisitions", nil), admin.ID, "admin")
	defer response.Body.Close()
	var concealed []radarrAcquisitionResponse
	if err := json.UnmarshalRead(response.Body, &concealed); err != nil {
		t.Fatalf("decode concealed list: %v", err)
	}
	if len(concealed) != 0 {
		t.Fatalf("concealed acquisitions = %+v, want none", concealed)
	}

	if _, flipped, err := h.movieService.RevealCurrentDrawContext(ctx); err != nil || !flipped {
		t.Fatalf("reveal draw: flipped=%v err=%v", flipped, err)
	}
	assertRadarrAttention(t, app, 1)

	response = doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/integrations/radarr/acquisitions", nil), admin.ID, "admin")
	defer response.Body.Close()
	var revealed []radarrAcquisitionResponse
	if err := json.UnmarshalRead(response.Body, &revealed); err != nil {
		t.Fatalf("decode revealed list: %v", err)
	}
	if len(revealed) != 1 || revealed[0].Title != "Arrival" || revealed[0].Status != "needs_preset" {
		t.Fatalf("revealed acquisitions = %+v", revealed)
	}

	response = doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/integrations", nil), admin.ID, "admin")
	defer response.Body.Close()
	var summaries []integrationSummaryResponse
	if err := json.UnmarshalRead(response.Body, &summaries); err != nil {
		t.Fatalf("decode integration summaries: %v", err)
	}
	if len(summaries) != 2 || summaries[1].ID != "radarr" || summaries[1].AttentionCount == nil || *summaries[1].AttentionCount != 1 || len(summaries[1].Operations) != 0 {
		t.Fatalf("Radarr summary = %+v", summaries)
	}
}

func TestRadarrSetupListsArchivedRecordsWithoutSecrets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, app, _, _, pool := setupEditMovieTestWithDB(t)
	repo := repository.NewSqliteRadarrRepository(pool)
	now := time.Now().UTC()
	instance, err := repo.CreateInstance(ctx, repository.RadarrInstanceSave{
		Name: "1080p", BaseURL: "https://radarr.example.test", EncryptedAPIKey: []byte("sealed-secret"),
		State: radarrInstanceConnected, CheckedAt: now,
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	preset, err := repo.CreatePreset(ctx, repository.RadarrPresetSave{
		Name: "Living room", InstanceID: instance.ID, RootFolderID: 2,
		RootFolderPath: "/movies", QualityProfileID: 4, QualityProfileName: "HD",
		Tags:                []repository.RadarrTagSnapshot{{ID: 9, Label: "shared"}},
		MinimumAvailability: "released", AcquisitionMode: "manual", Valid: true, ValidatedAt: now,
	})
	if err != nil {
		t.Fatalf("create preset: %v", err)
	}
	webhook, err := repo.CreateWebhookDestination(ctx, repository.RadarrWebhookDestinationSave{
		Name: "Discord", Kind: "discord", EncryptedURL: []byte("sealed-webhook"),
		ReasonFilters: []string{"release_required"}, Enabled: false,
	})
	if err != nil {
		t.Fatalf("create webhook: %v", err)
	}
	if err := repo.ArchivePreset(ctx, preset.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("archive preset: %v", err)
	}
	if err := repo.ArchiveInstance(ctx, instance.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("archive instance: %v", err)
	}
	if err := repo.ArchiveWebhookDestination(ctx, webhook.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("archive webhook: %v", err)
	}

	for _, endpoint := range []string{"instances", "presets", "webhooks"} {
		response := doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/integrations/radarr/"+endpoint, nil), 1, "admin")
		body, readErr := ioReadAll(response)
		closeErr := response.Body.Close()
		if readErr != nil {
			t.Fatalf("read %s: %v", endpoint, readErr)
		}
		if closeErr != nil {
			t.Fatalf("close %s response: %v", endpoint, closeErr)
		}
		if response.StatusCode != fiber.StatusOK || !strings.Contains(string(body), `"archivedAt"`) {
			t.Fatalf("%s response = %d %s", endpoint, response.StatusCode, body)
		}
		if strings.Contains(string(body), "sealed-secret") || strings.Contains(string(body), "sealed-webhook") {
			t.Fatalf("%s response leaked an encrypted secret: %s", endpoint, body)
		}
	}
}

func TestRadarrRemoveInstanceReportsHardDeleteForUnusedSetup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, app, _, _, pool := setupEditMovieTestWithDB(t)
	repo := repository.NewSqliteRadarrRepository(pool)
	now := time.Now().UTC()
	instance, err := repo.CreateInstance(ctx, repository.RadarrInstanceSave{
		Name: "Temporary", BaseURL: "https://radarr.example.test",
		EncryptedAPIKey: []byte("sealed-secret"), State: radarrInstanceConnected,
		CheckedAt: now,
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	preset, err := repo.CreatePreset(ctx, repository.RadarrPresetSave{
		Name: "Temporary preset", InstanceID: instance.ID, RootFolderID: 2,
		RootFolderPath: "/movies", QualityProfileID: 4, QualityProfileName: "HD",
		Tags:                []repository.RadarrTagSnapshot{},
		MinimumAvailability: "released", AcquisitionMode: "manual", Valid: true,
		ValidatedAt: now,
	})
	if err != nil {
		t.Fatalf("create preset: %v", err)
	}

	response := doAs(t, app, httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/integrations/radarr/instances/"+strconv.FormatInt(instance.ID, 10),
		nil,
	), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("remove unused instance status = %d, want %d", response.StatusCode, fiber.StatusOK)
	}
	var result radarrRemoveResponse
	if err := json.UnmarshalRead(response.Body, &result); err != nil {
		t.Fatalf("decode removal response: %v", err)
	}
	if result.Outcome != repository.RadarrOutcomeDeleted {
		t.Fatalf("removal outcome = %q, want %q", result.Outcome, repository.RadarrOutcomeDeleted)
	}
	if _, err := repo.GetInstance(ctx, instance.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get deleted instance = %v, want not found", err)
	}
	if _, err := repo.GetPreset(ctx, preset.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get deleted preset = %v, want not found", err)
	}
}

func TestRadarrRemovePresetReportsHardDeleteForUnusedSetup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, app, _, _, pool := setupEditMovieTestWithDB(t)
	repo := repository.NewSqliteRadarrRepository(pool)
	now := time.Now().UTC()
	instance, err := repo.CreateInstance(ctx, repository.RadarrInstanceSave{
		Name: "Preset host", BaseURL: "https://radarr.example.test",
		EncryptedAPIKey: []byte("sealed-secret"), State: radarrInstanceConnected,
		CheckedAt: now,
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	preset, err := repo.CreatePreset(ctx, repository.RadarrPresetSave{
		Name: "Temporary preset", InstanceID: instance.ID, RootFolderID: 2,
		RootFolderPath: "/movies", QualityProfileID: 4, QualityProfileName: "HD",
		Tags:                []repository.RadarrTagSnapshot{},
		MinimumAvailability: "released", AcquisitionMode: "manual", Valid: true,
		ValidatedAt: now,
	})
	if err != nil {
		t.Fatalf("create preset: %v", err)
	}

	response := doAs(t, app, httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/integrations/radarr/presets/"+strconv.FormatInt(preset.ID, 10),
		nil,
	), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("remove unused preset status = %d, want %d", response.StatusCode, fiber.StatusOK)
	}
	var result radarrRemoveResponse
	if err := json.UnmarshalRead(response.Body, &result); err != nil {
		t.Fatalf("decode removal response: %v", err)
	}
	if result.Outcome != repository.RadarrOutcomeDeleted {
		t.Fatalf("removal outcome = %q, want %q", result.Outcome, repository.RadarrOutcomeDeleted)
	}
	if _, err := repo.GetPreset(ctx, preset.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get deleted preset = %v, want not found", err)
	}
}

func TestRadarrRemovePresetHardDeletesArchivedUnusedSetup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, app, _, _, pool := setupEditMovieTestWithDB(t)
	repo := repository.NewSqliteRadarrRepository(pool)
	now := time.Now().UTC()
	instance, err := repo.CreateInstance(ctx, repository.RadarrInstanceSave{
		Name: "Legacy preset host", BaseURL: "https://radarr.example.test",
		EncryptedAPIKey: []byte("sealed-secret"), State: radarrInstanceConnected,
		CheckedAt: now,
	})
	if err != nil {
		t.Fatalf("create legacy instance: %v", err)
	}
	preset, err := repo.CreatePreset(ctx, repository.RadarrPresetSave{
		Name: "Legacy preset", InstanceID: instance.ID, RootFolderID: 2,
		RootFolderPath: "/movies", QualityProfileID: 4, QualityProfileName: "HD",
		Tags:                []repository.RadarrTagSnapshot{},
		MinimumAvailability: "released", AcquisitionMode: "manual", Valid: true,
		ValidatedAt: now,
	})
	if err != nil {
		t.Fatalf("create legacy preset: %v", err)
	}
	if err := repo.ArchivePreset(ctx, preset.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("archive legacy preset: %v", err)
	}

	response := doAs(t, app, httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/integrations/radarr/presets/"+strconv.FormatInt(preset.ID, 10),
		nil,
	), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("remove archived preset status = %d, want %d", response.StatusCode, fiber.StatusOK)
	}
	var result radarrRemoveResponse
	if err := json.UnmarshalRead(response.Body, &result); err != nil {
		t.Fatalf("decode archived removal response: %v", err)
	}
	if result.Outcome != repository.RadarrOutcomeDeleted {
		t.Fatalf("archived removal outcome = %q, want %q", result.Outcome, repository.RadarrOutcomeDeleted)
	}
	if _, err := repo.GetPreset(ctx, preset.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get deleted archived preset = %v, want not found", err)
	}
}

func setupUsedRadarrRemovalHTTPTest(
	t *testing.T,
) (*fiber.App, *repository.SqliteRadarrRepository, repository.RadarrInstance, repository.RadarrPreset) {
	t.Helper()
	ctx := context.Background()
	_, app, users, movies, pool := setupEditMovieTestWithDB(t)
	repo := repository.NewSqliteRadarrRepository(pool)
	now := time.Now().UTC().Truncate(time.Second)
	actor, err := users.Create(ctx, "Radarr removal Admin")
	if err != nil {
		t.Fatalf("create removal actor: %v", err)
	}
	instance, err := repo.CreateInstance(ctx, repository.RadarrInstanceSave{
		Name: "Used instance", BaseURL: "https://radarr.example.test",
		EncryptedAPIKey: []byte("sealed-secret"), State: radarrInstanceConnected,
		CheckedAt: now,
	})
	if err != nil {
		t.Fatalf("create used instance: %v", err)
	}
	preset, err := repo.CreatePreset(ctx, repository.RadarrPresetSave{
		Name: "Used preset", InstanceID: instance.ID, RootFolderID: 2,
		RootFolderPath: "/movies", QualityProfileID: 4, QualityProfileName: "HD",
		Tags:                []repository.RadarrTagSnapshot{},
		MinimumAvailability: "released", AcquisitionMode: "manual", Valid: true,
		ValidatedAt: now,
	})
	if err != nil {
		t.Fatalf("create used preset: %v", err)
	}
	movie, err := movies.Add(ctx, "Arrival", "pool", actor.ID)
	if err != nil {
		t.Fatalf("add removal movie: %v", err)
	}
	if err := movies.StartDraw(ctx, movie.ID, now, now.Add(16*time.Second), "drawer"); err != nil {
		t.Fatalf("start removal draw: %v", err)
	}
	if err := movies.RevealDraw(ctx, movie.ID, now.Add(17*time.Second)); err != nil {
		t.Fatalf("reveal removal draw: %v", err)
	}
	acquisitions, err := repo.ListAcquisitions(ctx, "")
	if err != nil {
		t.Fatalf("list removal acquisitions: %v", err)
	}
	if len(acquisitions) != 1 {
		t.Fatalf("removal acquisitions = %d, want 1", len(acquisitions))
	}
	if _, err := repo.SelectAcquisitionPreset(
		ctx,
		acquisitions[0].ID,
		preset.ID,
		actor.ID,
		now.Add(18*time.Second),
	); err != nil {
		t.Fatalf("select removal preset: %v", err)
	}
	return app, repo, instance, preset
}

func TestRadarrRemovePresetReportsArchivedOutcomeForUsedSetup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	app, repo, _, preset := setupUsedRadarrRemovalHTTPTest(t)

	response := doAs(t, app, httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/integrations/radarr/presets/"+strconv.FormatInt(preset.ID, 10),
		nil,
	), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("remove used preset status = %d, want %d", response.StatusCode, fiber.StatusOK)
	}
	var result radarrRemoveResponse
	if err := json.UnmarshalRead(response.Body, &result); err != nil {
		t.Fatalf("decode removal response: %v", err)
	}
	if result.Outcome != repository.RadarrOutcomeArchived {
		t.Fatalf("removal outcome = %q, want %q", result.Outcome, repository.RadarrOutcomeArchived)
	}
	stored, err := repo.GetPreset(ctx, preset.ID)
	if err != nil {
		t.Fatalf("get archived preset: %v", err)
	}
	if stored.ArchivedAt == nil {
		t.Fatal("used preset remained active")
	}
}

func TestRadarrRemoveInstanceReportsConflictForUnresolvedTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	app, repo, instance, _ := setupUsedRadarrRemovalHTTPTest(t)

	response := doAs(t, app, httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/integrations/radarr/instances/"+strconv.FormatInt(instance.ID, 10),
		nil,
	), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusConflict {
		t.Fatalf("remove unresolved instance status = %d, want %d", response.StatusCode, fiber.StatusConflict)
	}
	if code := problemCode(t, response); code != "conflict" {
		t.Fatalf("remove unresolved instance problem = %q, want conflict", code)
	}
	stored, err := repo.GetInstance(ctx, instance.ID)
	if err != nil {
		t.Fatalf("get retained instance: %v", err)
	}
	if stored.ArchivedAt != nil {
		t.Fatalf("refused instance archived_at = %v, want active", stored.ArchivedAt)
	}
}

func TestRadarrWebhookHTTPWorkflowRequiresSavedVerificationBeforeEnable(t *testing.T) {
	t.Parallel()
	var deliveries atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		deliveries.Add(1)
		response.WriteHeader(http.StatusNoContent)
	}))
	defer endpoint.Close()

	_, app, _, _ := setupEditMovieTest(t)
	response := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/integrations/radarr/webhooks", `{
		"name":"Discord ops",
		"format":"generic",
		"url":"`+endpoint.URL+`",
		"enabled":true,
		"reasons":["release_required"]
	}`), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusCreated {
		t.Fatalf("create webhook status = %d", response.StatusCode)
	}
	var created radarrWebhookResponse
	if err := json.UnmarshalRead(response.Body, &created); err != nil {
		t.Fatalf("decode created webhook: %v", err)
	}
	if created.Enabled || created.Verified || created.ID <= 0 || len(created.Reasons) != 1 {
		t.Fatalf("created webhook = %+v", created)
	}

	response = doAs(t, app, httptest.NewRequest(
		http.MethodPost,
		"/api/v1/integrations/radarr/webhooks/"+strconv.FormatInt(created.ID, 10)+"/test",
		nil,
	), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("test webhook status = %d", response.StatusCode)
	}
	var verified radarrWebhookResponse
	if err := json.UnmarshalRead(response.Body, &verified); err != nil {
		t.Fatalf("decode tested webhook: %v", err)
	}
	if !verified.Verified || deliveries.Load() != 1 {
		t.Fatalf("tested webhook = %+v, deliveries = %d", verified, deliveries.Load())
	}

	response = doAs(t, app, jsonReq(
		http.MethodPut,
		"/api/v1/integrations/radarr/webhooks/"+strconv.FormatInt(created.ID, 10),
		`{"name":"Discord ops","format":"generic","enabled":true,"reasons":["release_required"],"revision":1}`,
	), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("enable webhook status = %d", response.StatusCode)
	}
	var enabled radarrWebhookResponse
	if err := json.UnmarshalRead(response.Body, &enabled); err != nil {
		t.Fatalf("decode enabled webhook: %v", err)
	}
	if !enabled.Enabled || !enabled.Verified {
		t.Fatalf("enabled webhook = %+v", enabled)
	}

	response = doAs(t, app, jsonReq(
		http.MethodPut,
		"/api/v1/integrations/radarr/webhooks/"+strconv.FormatInt(created.ID, 10),
		fmt.Sprintf(`{"name":"Discord ops","format":"discord","enabled":true,"reasons":["release_required"],"revision":%d}`, enabled.Revision),
	), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("change webhook format status = %d", response.StatusCode)
	}
	var changed radarrWebhookResponse
	if err := json.UnmarshalRead(response.Body, &changed); err != nil {
		t.Fatalf("decode changed webhook: %v", err)
	}
	if changed.Enabled || changed.Verified {
		t.Fatalf("format-changed webhook = %+v, want disabled and unverified", changed)
	}
}

func TestRadarrWebhookDraftTestReturnsActionableFailure(t *testing.T) {
	t.Parallel()
	endpoint := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusBadGateway)
	}))
	defer endpoint.Close()
	_, app, _, _ := setupEditMovieTest(t)
	response := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/integrations/radarr/webhooks/test", `{
		"name":"Broken",
		"format":"generic",
		"url":"`+endpoint.URL+`",
		"enabled":false,
		"reasons":["release_required"]
	}`), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusUnprocessableEntity || problemCode(t, response) != "webhook_test_failed" {
		t.Fatalf("draft test status = %d, want webhook_test_failed", response.StatusCode)
	}
}

func TestRadarrHTTPSetupAndManualAcquisitionWorkflow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h, app, users, movies := setupEditMovieTest(t)
	client := &radarrHTTPTestClient{
		catalog: integrationradarr.Catalog{
			RootFolders:     []integrationradarr.RootFolder{{ID: 2, Path: "/movies", Accessible: true}},
			QualityProfiles: []integrationradarr.QualityProfile{{ID: 4, Name: "HD"}},
			Tags:            []integrationradarr.Tag{{ID: 9, Label: "shared"}},
		},
		releases: []integrationradarr.Release{{
			ID: "opaque-release", Title: "Arrival.2016.1080p", Indexer: "Indexer",
			Quality: integrationradarr.Quality{Name: "Bluray-1080p"}, Approved: true,
		}},
	}
	h.radarr.newClient = func(baseURL, apiKey string) (integrationradarr.Client, error) {
		client.baseURL = baseURL
		client.apiKey = apiKey
		return client, nil
	}

	response := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/integrations/radarr/instances", `{
		"name":"Main","url":"https://radarr.example.test/","apiKey":"write-only-key"
	}`), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusCreated {
		t.Fatalf("create instance status = %d", response.StatusCode)
	}
	var instance radarrInstanceResponse
	if err := json.UnmarshalRead(response.Body, &instance); err != nil {
		t.Fatalf("decode instance: %v", err)
	}
	if instance.ID <= 0 || !instance.APIKeyConfigured || client.baseURL != "https://radarr.example.test" || client.apiKey != "write-only-key" {
		t.Fatalf("instance = %+v, client URL/key = %q/%q", instance, client.baseURL, client.apiKey)
	}

	response = doAs(t, app, httptest.NewRequest(
		http.MethodGet,
		"/api/v1/integrations/radarr/instances/"+strconv.FormatInt(instance.ID, 10)+"/options",
		nil,
	), 1, "admin")
	defer response.Body.Close()
	var options radarrInstanceOptionsResponse
	if err := json.UnmarshalRead(response.Body, &options); err != nil {
		t.Fatalf("decode options: %v", err)
	}
	if len(options.RootFolders) != 1 || len(options.QualityProfiles) != 1 || len(options.Tags) != 1 {
		t.Fatalf("instance options = %+v", options)
	}

	response = doAs(t, app, jsonReq(http.MethodPost, "/api/v1/integrations/radarr/presets", `{
		"name":"Living room","instanceId":`+strconv.FormatInt(instance.ID, 10)+`,
		"rootFolderPath":"/movies","qualityProfileId":4,"tagIds":[9],
		"minimumAvailability":"released","mode":"manual"
	}`), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusCreated {
		t.Fatalf("create preset status = %d", response.StatusCode)
	}
	var preset radarrPresetResponse
	if err := json.UnmarshalRead(response.Body, &preset); err != nil {
		t.Fatalf("decode preset: %v", err)
	}
	if preset.ID <= 0 || !preset.Valid || preset.QualityProfileName != "HD" || len(preset.Tags) != 1 {
		t.Fatalf("preset = %+v", preset)
	}

	admin, err := users.Create(ctx, "Admin")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	movie, err := movies.Add(ctx, "Arrival", "pool", admin.ID)
	if err != nil {
		t.Fatalf("add movie: %v", err)
	}
	tmdbID := 329865
	if err := movies.SetExternalIDs(ctx, movie.ID, &tmdbID, nil); err != nil {
		t.Fatalf("set movie identity: %v", err)
	}
	if _, err := h.movieService.DrawRandom(ctx, "drawer"); err != nil {
		t.Fatalf("draw: %v", err)
	}
	if _, flipped, err := h.movieService.RevealCurrentDrawContext(ctx); err != nil || !flipped {
		t.Fatalf("reveal: flipped=%v err=%v", flipped, err)
	}

	response = doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/integrations/radarr/acquisitions", nil), admin.ID, "admin")
	defer response.Body.Close()
	var acquisitions []radarrAcquisitionResponse
	if err := json.UnmarshalRead(response.Body, &acquisitions); err != nil {
		t.Fatalf("decode acquisitions: %v", err)
	}
	if len(acquisitions) != 1 {
		t.Fatalf("acquisitions = %+v", acquisitions)
	}
	acquisitionPath := "/api/v1/integrations/radarr/acquisitions/" + strconv.FormatInt(acquisitions[0].ID, 10)

	response = doAs(t, app, jsonReq(http.MethodPut, acquisitionPath+"/preset", `{"presetId":`+strconv.FormatInt(preset.ID, 10)+`}`), admin.ID, "admin")
	defer response.Body.Close()
	var preview radarrAcquisitionResponse
	if err := json.UnmarshalRead(response.Body, &preview); err != nil {
		t.Fatalf("decode target preview: %v", err)
	}
	if response.StatusCode != fiber.StatusOK || !preview.PreviewReady || preview.TargetLocked || preview.TargetPreviewExisting {
		t.Fatalf("target preview = status %d %+v", response.StatusCode, preview)
	}

	response = doAs(t, app, httptest.NewRequest(http.MethodPost, acquisitionPath+"/confirm", nil), admin.ID, "admin")
	defer response.Body.Close()
	var confirmed radarrAcquisitionResponse
	if err := json.UnmarshalRead(response.Body, &confirmed); err != nil {
		t.Fatalf("decode confirmed target: %v", err)
	}
	if response.StatusCode != fiber.StatusOK || !confirmed.TargetLocked || confirmed.Status != "needs_release" || confirmed.RadarrMovieID != 91 {
		t.Fatalf("confirmed target = status %d %+v", response.StatusCode, confirmed)
	}
	if client.addRequest.TMDBID != tmdbID || client.addRequest.Mode != integrationradarr.AcquisitionModeManual {
		t.Fatalf("Radarr add request = %+v", client.addRequest)
	}

	response = doAs(t, app, httptest.NewRequest(http.MethodPost, acquisitionPath+"/releases/search", nil), admin.ID, "admin")
	defer response.Body.Close()
	var releases []radarrReleaseResponse
	if err := json.UnmarshalRead(response.Body, &releases); err != nil {
		t.Fatalf("decode releases: %v", err)
	}
	if len(releases) != 1 || releases[0].ID != "opaque-release" || !releases[0].GrabAllowed {
		t.Fatalf("release results = %+v", releases)
	}

	response = doAs(t, app, jsonReq(http.MethodPost, acquisitionPath+"/releases/opaque-release/grab", `{"override":false}`), admin.ID, "admin")
	defer response.Body.Close()
	var grabbed radarrAcquisitionResponse
	if err := json.UnmarshalRead(response.Body, &grabbed); err != nil {
		t.Fatalf("decode grabbed acquisition: %v", err)
	}
	if response.StatusCode != fiber.StatusOK || grabbed.Status != "queued" || !grabbed.ActiveQueue || grabbed.ManualAttemptCount != 1 {
		t.Fatalf("grabbed acquisition = status %d %+v", response.StatusCode, grabbed)
	}
	if !client.grabbed || client.movie == nil || !client.movie.Monitored {
		t.Fatalf("release grab/monitor state = grabbed %v movie %+v", client.grabbed, client.movie)
	}
}

func TestRadarrInstanceEndpointChangeRequiresExplicitAPIKey(t *testing.T) {
	t.Parallel()
	h, app, _, _ := setupEditMovieTest(t)
	client := &radarrHTTPTestClient{}
	var clientCalls atomic.Int32
	h.radarr.newClient = func(baseURL, apiKey string) (integrationradarr.Client, error) {
		clientCalls.Add(1)
		client.baseURL = baseURL
		client.apiKey = apiKey
		return client, nil
	}

	response := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/integrations/radarr/instances", `{
		"name":"Main","url":"https://radarr.example.test","apiKey":"write-only-key"
	}`), 1, "admin")
	if response.StatusCode != fiber.StatusCreated {
		_ = response.Body.Close()
		t.Fatalf("create instance status = %d", response.StatusCode)
	}
	var instance radarrInstanceResponse
	if err := json.UnmarshalRead(response.Body, &instance); err != nil {
		_ = response.Body.Close()
		t.Fatalf("decode instance: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close create response: %v", err)
	}

	response = doAs(t, app, jsonReq(
		http.MethodPut,
		"/api/v1/integrations/radarr/instances/"+strconv.FormatInt(instance.ID, 10),
		fmt.Sprintf(`{
			"name":"Main","url":"http://capture.example.test","revision":%d
		}`, instance.Revision),
	), 1, "admin")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusUnprocessableEntity {
		t.Fatalf("endpoint change status = %d, want %d", response.StatusCode, fiber.StatusUnprocessableEntity)
	}
	var problem radarrProblemResponse
	if err := json.UnmarshalRead(response.Body, &problem); err != nil {
		t.Fatalf("decode endpoint change problem: %v", err)
	}
	if len(problem.Issues) != 1 || problem.Issues[0].Field != "apiKey" {
		t.Fatalf("endpoint change problem = %+v", problem)
	}
	if clientCalls.Load() != 1 || client.baseURL != "https://radarr.example.test" || client.apiKey != "write-only-key" {
		t.Fatalf("client calls = %d, URL/key = %q/%q", clientCalls.Load(), client.baseURL, client.apiKey)
	}
}

func TestRadarrAcquisitionDTOExposesPreviewAndLockedTargetState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	instanceID, presetID, profileID := int64(3), int64(5), 7
	response := toRadarrAcquisitionResponse(repository.RadarrAcquisition{
		ID: 17, MovieID: 23, MovieTitle: "Moon", MovieYear: 2009, Status: "waiting_for_radarr",
		TMDBID: new(17431), IdentitySource: "tmdb",
		PresetID: &presetID, PresetName: "4K", TargetInstanceID: &instanceID,
		TargetInstanceName: "Radarr 4K", TargetRootFolderPath: "/movies-4k",
		TargetQualityProfileID: &profileID, TargetQualityProfileName: "UHD",
		TargetMinimumAvailability: "released", TargetAcquisitionMode: "manual",
		TargetPreviewExisting: true, TargetPreviewedAt: &now,
		EffectiveConfiguration: repository.RadarrEffectiveConfiguration{
			RootFolderPath: "/existing", QualityProfileID: 8, QualityProfileName: "Existing",
			Monitored: true,
		},
		CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
	})
	if !response.PreviewReady || !response.TargetPreviewExisting || !response.Existing || response.TargetLocked || response.AdoptedExisting {
		t.Fatalf("preview DTO = %+v", response)
	}
	if response.Year != 2009 || response.Identity == nil || response.Identity.Year != 2009 {
		t.Fatalf("movie year DTO = %+v", response)
	}
	if response.EffectiveConfig == nil || response.EffectiveConfig.RootFolderPath != "/existing" {
		t.Fatalf("effective config = %+v", response.EffectiveConfig)
	}
}

func TestRadarrAcquisitionDTOProjectsWildcardCancellation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 25, 18, 0, 0, 0, time.UTC)
	wildcardID := int64(12)
	response := toRadarrAcquisitionResponse(repository.RadarrAcquisition{
		ID: 21, MovieID: 30, MovieTitle: "Guest Night", Status: "abandoned",
		Source: "wildcard", WildcardID: &wildcardID, CanceledAt: &now,
		CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
	})
	if response.Status != "canceled" || response.Source != "wildcard" || response.WildcardID != wildcardID {
		t.Fatalf("canceled wildcard DTO = %+v", response)
	}
	if response.Milestones.CanceledAt == "" {
		t.Fatal("canceled wildcard DTO omitted canceledAt")
	}
}

func TestRadarrReleaseDTOContainsOnlySanitizedSelectionFields(t *testing.T) {
	t.Parallel()
	seeders := 14
	response := toRadarrReleaseResponse(integrationradarr.Release{
		ID: "opaque-3", Title: "Moon.2009.1080p", Indexer: "Indexer",
		Size: 8_000_000_000, PublishedAt: time.Now().UTC().Add(-2 * time.Hour),
		Protocol: "torrent", Seeders: &seeders,
		Quality:           integrationradarr.Quality{Name: "Bluray-1080p"},
		CustomFormats:     []string{"Preferred group", "Original language"},
		CustomFormatScore: 1200, Approved: false, Rejected: true,
		RejectionReasons: []string{"below preferred score"},
	}, time.Now().UTC())
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal release: %v", err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"guid", "magnet", "downloadUrl", "infoUrl", "indexerId", "hash"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("release DTO contains forbidden field %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, `"id":"opaque-3"`) || !strings.Contains(text, `"rejected":true`) {
		t.Fatalf("release DTO = %s", text)
	}
	if !strings.Contains(text, `"customFormats":["Preferred group","Original language"]`) {
		t.Fatalf("release DTO custom formats = %s", text)
	}
}

func TestRadarrWorkerLifecycleStartsOnceAndCloses(t *testing.T) {
	t.Parallel()
	h, _, _, _ := setupEditMovieTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	h.startRadarrWorkers(ctx)
	firstAcquisitionWorker := h.radarrAcquisitions
	firstWebhookWorker := h.radarrWebhooks
	h.startRadarrWorkers(ctx)
	if firstAcquisitionWorker == nil || firstWebhookWorker == nil ||
		h.radarrAcquisitions != firstAcquisitionWorker || h.radarrWebhooks != firstWebhookWorker {
		t.Fatal("Radarr workers were not initialized exactly once")
	}
	cancel()
	h.Close()
}

func radarrRouteContract() []string {
	return []string{
		"GET /api/v1/integrations/radarr/attention",
		"GET /api/v1/integrations/radarr/acquisitions",
		"GET /api/v1/integrations/radarr/acquisitions/:id",
		"PUT /api/v1/integrations/radarr/acquisitions/:id/preset",
		"POST /api/v1/integrations/radarr/acquisitions/:id/confirm",
		"POST /api/v1/integrations/radarr/acquisitions/:id/identity-search",
		"PUT /api/v1/integrations/radarr/acquisitions/:id/identity",
		"POST /api/v1/integrations/radarr/acquisitions/:id/releases/search",
		"POST /api/v1/integrations/radarr/acquisitions/:id/releases/:resultId/grab",
		"POST /api/v1/integrations/radarr/acquisitions/:id/retry",
		"POST /api/v1/integrations/radarr/acquisitions/:id/abandon/review",
		"POST /api/v1/integrations/radarr/acquisitions/:id/abandon",
		"GET /api/v1/integrations/radarr/instances",
		"POST /api/v1/integrations/radarr/instances",
		"PUT /api/v1/integrations/radarr/instances/:id",
		"DELETE /api/v1/integrations/radarr/instances/:id",
		"GET /api/v1/integrations/radarr/instances/:id/options",
		"GET /api/v1/integrations/radarr/presets",
		"POST /api/v1/integrations/radarr/presets",
		"PUT /api/v1/integrations/radarr/presets/:id",
		"DELETE /api/v1/integrations/radarr/presets/:id",
		"GET /api/v1/integrations/radarr/webhooks",
		"POST /api/v1/integrations/radarr/webhooks",
		"PUT /api/v1/integrations/radarr/webhooks/:id",
		"DELETE /api/v1/integrations/radarr/webhooks/:id",
		"POST /api/v1/integrations/radarr/webhooks/:id/test",
		"POST /api/v1/integrations/radarr/webhooks/test",
	}
}

func assertRadarrAttention(t *testing.T, app *fiber.App, want int) {
	t.Helper()
	response := doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/integrations/radarr/attention", nil), 1, "admin")
	defer response.Body.Close()
	var body radarrAttentionResponse
	if err := json.UnmarshalRead(response.Body, &body); err != nil {
		t.Fatalf("decode attention: %v", err)
	}
	if response.StatusCode != fiber.StatusOK || body.Count != want {
		t.Fatalf("attention = status %d count %d, want %d", response.StatusCode, body.Count, want)
	}
}

func ioReadAll(response *http.Response) ([]byte, error) {
	return io.ReadAll(response.Body)
}

type radarrHTTPTestClient struct {
	baseURL    string
	apiKey     string
	catalog    integrationradarr.Catalog
	movie      *integrationradarr.Movie
	addRequest integrationradarr.AddMovieRequest
	releases   []integrationradarr.Release
	grabbed    bool
}

func (c *radarrHTTPTestClient) VerifyAndCatalog(context.Context) (integrationradarr.Catalog, error) {
	return c.catalog, nil
}

func (c *radarrHTTPTestClient) LookupMovie(context.Context, integrationradarr.ExactIdentity) (integrationradarr.MovieCandidate, error) {
	return integrationradarr.MovieCandidate{}, integrationradarr.ErrNotFound
}

func (c *radarrHTTPTestClient) SearchMovies(context.Context, integrationradarr.TitleQuery) ([]integrationradarr.MovieCandidate, error) {
	return []integrationradarr.MovieCandidate{}, nil
}

func (c *radarrHTTPTestClient) FindMovieByTMDB(_ context.Context, tmdbID int) (*integrationradarr.Movie, error) {
	if c.movie == nil || c.movie.TMDBID != tmdbID {
		return nil, nil
	}
	movie := *c.movie
	return &movie, nil
}

func (c *radarrHTTPTestClient) AddMovie(_ context.Context, request integrationradarr.AddMovieRequest) (integrationradarr.Movie, error) {
	c.addRequest = request
	c.movie = &integrationradarr.Movie{
		ID: 91, TMDBID: request.TMDBID, Title: request.Title,
		RootFolderPath: request.RootFolderPath, QualityProfileID: request.QualityProfileID,
		TagIDs: append([]int(nil), request.TagIDs...), MinimumAvailability: request.MinimumAvailability,
	}
	return *c.movie, nil
}

func (c *radarrHTTPTestClient) GetMovie(context.Context, int) (integrationradarr.Movie, error) {
	if c.movie == nil {
		return integrationradarr.Movie{}, integrationradarr.ErrNotFound
	}
	return *c.movie, nil
}

func (c *radarrHTTPTestClient) Queue(context.Context, int) ([]integrationradarr.QueueItem, error) {
	return []integrationradarr.QueueItem{}, nil
}

func (c *radarrHTTPTestClient) History(context.Context, int) ([]integrationradarr.HistoryItem, error) {
	return []integrationradarr.HistoryItem{}, nil
}

func (c *radarrHTTPTestClient) SearchReleases(context.Context, int) ([]integrationradarr.Release, error) {
	return append([]integrationradarr.Release(nil), c.releases...), nil
}

func (c *radarrHTTPTestClient) GrabRelease(_ context.Context, request integrationradarr.GrabReleaseRequest) error {
	if request.ResultID != "opaque-release" {
		return integrationradarr.ErrReleaseExpired
	}
	c.grabbed = true
	return nil
}

func (c *radarrHTTPTestClient) SetMonitored(_ context.Context, _ int, monitored bool) (integrationradarr.Movie, error) {
	if c.movie == nil {
		return integrationradarr.Movie{}, integrationradarr.ErrNotFound
	}
	c.movie.Monitored = monitored
	return *c.movie, nil
}

func (c *radarrHTTPTestClient) StartMoviesSearch(context.Context, int) (integrationradarr.Command, error) {
	return integrationradarr.Command{ID: 7, Status: "queued"}, nil
}

func (c *radarrHTTPTestClient) FindRecentMoviesSearchCommand(
	context.Context,
	int,
	time.Time,
) (*integrationradarr.Command, error) {
	return nil, nil
}

func (c *radarrHTTPTestClient) GetCommand(context.Context, int) (integrationradarr.Command, error) {
	return integrationradarr.Command{ID: 7, Status: "completed"}, nil
}
