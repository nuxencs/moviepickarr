package tmdb_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/integration"
	"moviepickarr/internal/integration/tmdb"
	"moviepickarr/internal/repository"
)

func setupConfigService(t *testing.T, environment tmdb.EnvironmentConfig) (*tmdb.Service, *repository.SqliteIntegrationConfigRepository) {
	t.Helper()
	pool, err := db.OpenSQLite(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.RunMigrations(context.Background(), pool.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	repo := repository.NewSqliteIntegrationConfigRepository(pool)
	return tmdb.NewService(repo, nil, environment, nil, nil), repo
}

func TestService_GetReturnsEffectiveConfigurationAndRevision(t *testing.T) {
	service, repo := setupConfigService(t, tmdb.EnvironmentConfig{CastLimit: new(24)})
	_, err := repo.Save(context.Background(), integration.ConfigSave{
		Integration:      "tmdb",
		ExpectedRevision: 1,
		AdminConfig:      []byte(`{"castLimit":30}`),
		State:            integration.StateDisabled,
	})
	if err != nil {
		t.Fatalf("seed Admin config: %v", err)
	}

	view, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if view.Revision != 2 {
		t.Fatalf("revision = %d, want 2", view.Revision)
	}
	if view.Config.CastLimit.Value != 24 || view.Config.CastLimit.Source != integration.SourceEnvironment {
		t.Fatalf("cast limit = %+v", view.Config.CastLimit)
	}
	if !view.Config.CastLimit.HasAdminFallback {
		t.Fatal("expected dormant Admin fallback")
	}
}

func TestService_GetReportsEnabledEnvironmentCredentialAsUnverified(t *testing.T) {
	service, _ := setupConfigService(t, tmdb.EnvironmentConfig{APIKey: "environment-key"})

	view, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if view.State != integration.StateCouldNotVerify {
		t.Fatalf("state = %q, want %q", view.State, integration.StateCouldNotVerify)
	}
	if view.Reason == "" {
		t.Fatal("expected unverified connection reason")
	}
}

func TestService_GetKeepsEnvironmentCredentialDisabledWhenConfiguredOff(t *testing.T) {
	service, _ := setupConfigService(t, tmdb.EnvironmentConfig{
		Enabled: new(false),
		APIKey:  "environment-key",
	})

	view, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if view.State != integration.StateDisabled || view.Reason != "" {
		t.Fatalf("state = %q, reason = %q, want disabled without reason", view.State, view.Reason)
	}
}

func TestService_GetReturnsSeparateScanAndConnectionTimestamps(t *testing.T) {
	service, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	scanAt := time.Date(2026, time.August, 4, 15, 4, 5, 0, time.UTC)
	connectionTestedAt := scanAt.Add(10 * time.Minute)
	if err := repo.UpdateLastChecked(context.Background(), "tmdb", scanAt); err != nil {
		t.Fatalf("seed scan: %v", err)
	}
	if err := repo.UpdateConnectionTest(
		context.Background(),
		"tmdb",
		integration.StateCouldNotVerify,
		"temporary failure",
		connectionTestedAt,
	); err != nil {
		t.Fatalf("seed connection test: %v", err)
	}

	view, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if view.LastCheckedAt == nil || !view.LastCheckedAt.Equal(scanAt) {
		t.Fatalf("last library scan = %v, want %v", view.LastCheckedAt, scanAt)
	}
	if view.LastConnectionTestedAt == nil || !view.LastConnectionTestedAt.Equal(connectionTestedAt) {
		t.Fatalf("last connection test = %v, want %v", view.LastConnectionTestedAt, connectionTestedAt)
	}
}

func TestService_AcquireLoadsTheEnvironmentRuntime(t *testing.T) {
	service, _ := setupConfigService(t, tmdb.EnvironmentConfig{
		APIKey:    "environment-key",
		CastLimit: new(24),
	})

	snapshot, err := service.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire runtime: %v", err)
	}
	if snapshot.Revision != 1 || snapshot.Config.APIKey != "environment-key" || snapshot.Config.CastLimit != 24 {
		t.Fatalf("runtime snapshot = %+v", snapshot)
	}
}

func TestService_AcquireDecryptsTheAdminRuntime(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	secrets := integration.NewSecretStore(fixedKey(make([]byte, 32)))
	sealed, err := secrets.Encrypt("stored-key")
	if err != nil {
		t.Fatalf("encrypt stored key: %v", err)
	}
	_, err = repo.Save(context.Background(), integration.ConfigSave{
		Integration:      "tmdb",
		ExpectedRevision: 1,
		AdminConfig:      []byte(`{"castLimit":30}`),
		SecretAction:     integration.SecretReplace,
		EncryptedSecret:  sealed,
		State:            integration.StateConnected,
	})
	if err != nil {
		t.Fatalf("seed Admin runtime: %v", err)
	}
	service := tmdb.NewService(repo, secrets, tmdb.EnvironmentConfig{}, nil, nil)

	snapshot, err := service.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire runtime: %v", err)
	}
	if snapshot.Revision != 2 || snapshot.Config.APIKey != "stored-key" || snapshot.Config.CastLimit != 30 {
		t.Fatalf("runtime snapshot = %+v", snapshot)
	}
}

func TestService_AcquireRestoresPersistedAuthenticationSuspension(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	secrets := integration.NewSecretStore(fixedKey(make([]byte, 32)))
	sealed, err := secrets.Encrypt("rejected-key")
	if err != nil {
		t.Fatalf("encrypt stored key: %v", err)
	}
	_, err = repo.Save(context.Background(), integration.ConfigSave{
		Integration:      "tmdb",
		ExpectedRevision: 1,
		AdminConfig:      []byte(`{}`),
		SecretAction:     integration.SecretReplace,
		EncryptedSecret:  sealed,
		State:            integration.StateError,
		StateReason:      "API key rejected",
	})
	if err != nil {
		t.Fatalf("seed rejected runtime: %v", err)
	}
	service := tmdb.NewService(repo, secrets, tmdb.EnvironmentConfig{}, nil, nil)

	if _, err := service.Acquire(context.Background()); !errors.Is(err, tmdb.ErrAPIKeyRejected) {
		t.Fatalf("acquire restored runtime error = %v, want API key rejected", err)
	}
}

func TestService_AcquireTreatsEnvironmentCredentialAsReplacementAfterRestart(t *testing.T) {
	service, repo := setupConfigService(t, tmdb.EnvironmentConfig{APIKey: "replacement-key"})
	if err := repo.UpdateState(
		context.Background(),
		"tmdb",
		integration.StateError,
		"API key rejected",
	); err != nil {
		t.Fatalf("seed rejected environment credential: %v", err)
	}

	snapshot, err := service.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire replacement environment credential: %v", err)
	}
	if snapshot.Config.APIKey != "replacement-key" {
		t.Fatalf("runtime API key = %q, want replacement", snapshot.Config.APIKey)
	}
	view, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("get replacement state: %v", err)
	}
	if view.State != integration.StateCouldNotVerify || view.Reason == "" {
		t.Fatalf("replacement state = %q, reason = %q, want could not verify", view.State, view.Reason)
	}
}

type countingSecrets struct {
	decryptCalls atomic.Int32
}

func (secrets *countingSecrets) Encrypt(value string) ([]byte, error) {
	return []byte("sealed:" + value), nil
}

func (secrets *countingSecrets) Decrypt([]byte) (string, error) {
	secrets.decryptCalls.Add(1)
	return "stored-key", nil
}

func TestService_ReusesTheDecryptedCredentialForOneRevision(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	_, err := repo.Save(context.Background(), integration.ConfigSave{
		Integration:      "tmdb",
		ExpectedRevision: 1,
		AdminConfig:      []byte(`{}`),
		SecretAction:     integration.SecretReplace,
		EncryptedSecret:  []byte("ciphertext"),
		State:            integration.StateConnected,
	})
	if err != nil {
		t.Fatalf("seed Admin runtime: %v", err)
	}
	secrets := &countingSecrets{}
	service := tmdb.NewService(repo, secrets, tmdb.EnvironmentConfig{}, nil, nil)

	if _, err := service.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire runtime: %v", err)
	}
	if _, err := service.Get(context.Background()); err != nil {
		t.Fatalf("first settings read: %v", err)
	}
	view, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("second settings read: %v", err)
	}
	if _, err := service.Save(context.Background(), tmdb.SaveDraft{
		Revision: view.Revision,
		Admin:    tmdb.AdminConfig{CastLimit: new(30)},
	}); err != nil {
		t.Fatalf("save non-secret setting: %v", err)
	}
	if _, err := service.Acquire(context.Background()); err != nil {
		t.Fatalf("reacquire runtime: %v", err)
	}
	if got := secrets.decryptCalls.Load(); got != 1 {
		t.Fatalf("decrypt calls = %d, want 1 for one revision", got)
	}
}

type fixedKey []byte

func (key fixedKey) Key() ([]byte, error) { return key, nil }

func TestService_GetMarksAnUnreadableCredentialUnavailable(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	originalSecrets := integration.NewSecretStore(fixedKey(make([]byte, 32)))
	sealed, err := originalSecrets.Encrypt("stored-api-key")
	if err != nil {
		t.Fatalf("encrypt stored key: %v", err)
	}
	_, err = repo.Save(context.Background(), integration.ConfigSave{
		Integration:      "tmdb",
		ExpectedRevision: 1,
		AdminConfig:      []byte(`{}`),
		SecretAction:     integration.SecretReplace,
		EncryptedSecret:  sealed,
		State:            integration.StateConnected,
	})
	if err != nil {
		t.Fatalf("seed stored key: %v", err)
	}
	wrongKey := make([]byte, 32)
	wrongKey[0] = 1
	service := tmdb.NewService(repo, integration.NewSecretStore(fixedKey(wrongKey)), tmdb.EnvironmentConfig{}, nil, nil)

	view, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if view.State != integration.StateCredentialUnavailable {
		t.Fatalf("state = %q, want credential_unavailable", view.State)
	}
	if !view.Config.APIKey.Configured || view.Config.APIKey.Source != integration.SourceAdmin {
		t.Fatalf("secret metadata = %+v", view.Config.APIKey)
	}
	if _, err := service.Acquire(context.Background()); !errors.Is(err, integration.ErrCredentialUnavailable) {
		t.Fatalf("acquire unreadable credential error = %v, want credential unavailable", err)
	}
}

func TestService_SavePersistsOneValidatedDraft(t *testing.T) {
	service, _ := setupConfigService(t, tmdb.EnvironmentConfig{})

	view, err := service.Save(context.Background(), tmdb.SaveDraft{
		Revision: 1,
		Admin: tmdb.AdminConfig{
			CastLimit: new(30),
		},
	})
	if err != nil {
		t.Fatalf("save config: %v", err)
	}
	if view.Revision != 2 {
		t.Fatalf("revision = %d, want 2", view.Revision)
	}
	if view.Config.CastLimit.Value != 30 || view.Config.CastLimit.Source != integration.SourceAdmin {
		t.Fatalf("cast limit = %+v", view.Config.CastLimit)
	}
}

func TestService_SaveRequiresConfirmationForAggressiveValues(t *testing.T) {
	service, _ := setupConfigService(t, tmdb.EnvironmentConfig{})
	fast := 100 * time.Millisecond

	_, err := service.Save(context.Background(), tmdb.SaveDraft{
		Revision: 1,
		Admin:    tmdb.AdminConfig{MinInterval: &fast},
	})
	var warning *tmdb.WarningConfirmationError
	if !errors.As(err, &warning) {
		t.Fatalf("error = %v, want warning confirmation", err)
	}
	if len(warning.Warnings) != 1 || warning.Warnings[0].Field != "minInterval" {
		t.Fatalf("warnings = %+v", warning.Warnings)
	}

	view, getErr := service.Get(context.Background())
	if getErr != nil {
		t.Fatalf("get unchanged config: %v", getErr)
	}
	if view.Revision != 1 || view.Config.MinInterval.Value != 250*time.Millisecond {
		t.Fatalf("config changed despite missing confirmation: %+v", view)
	}
}

func TestService_SaveRetainsDormantFallbackUnlessRemovalIsStaged(t *testing.T) {
	service, repo := setupConfigService(t, tmdb.EnvironmentConfig{CastLimit: new(24)})
	_, err := repo.Save(context.Background(), integration.ConfigSave{
		Integration:      "tmdb",
		ExpectedRevision: 1,
		AdminConfig:      []byte(`{"castLimit":30}`),
		State:            integration.StateDisabled,
	})
	if err != nil {
		t.Fatalf("seed Admin fallback: %v", err)
	}

	view, err := service.Save(context.Background(), tmdb.SaveDraft{
		Revision: 2,
		Admin:    tmdb.AdminConfig{},
	})
	if err != nil {
		t.Fatalf("save unrelated draft: %v", err)
	}
	if !view.Config.CastLimit.HasAdminFallback {
		t.Fatal("dormant fallback was discarded without a removal action")
	}
}

type recordingTester struct {
	config tmdb.RuntimeConfig
	err    error
}

func (tester *recordingTester) TestConnection(_ context.Context, config tmdb.RuntimeConfig) error {
	tester.config = config
	return tester.err
}

func TestService_SaveChecksAndEncryptsANewAPIKey(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	secrets := integration.NewSecretStore(fixedKey(make([]byte, 32)))
	tester := &recordingTester{}
	service := tmdb.NewService(repo, secrets, tmdb.EnvironmentConfig{}, tester, nil)

	view, err := service.Save(context.Background(), tmdb.SaveDraft{
		Revision: 1,
		APIKey:   "new-api-key",
	})
	if err != nil {
		t.Fatalf("save API key: %v", err)
	}
	if tester.config.APIKey != "new-api-key" {
		t.Fatalf("connection test used API key %q", tester.config.APIKey)
	}
	if view.State != integration.StateConnected {
		t.Fatalf("state = %q, want connected", view.State)
	}
	if view.LastCheckedAt != nil {
		t.Fatalf("saving a checked API key replaced library scan time with %v", view.LastCheckedAt)
	}
	if view.LastConnectionTestedAt == nil {
		t.Fatal("saving a checked API key did not record its connection test")
	}
	if !view.Config.APIKey.Configured || view.Config.APIKey.Source != integration.SourceAdmin {
		t.Fatalf("secret metadata = %+v", view.Config.APIKey)
	}
	record, err := repo.Get(context.Background(), "tmdb")
	if err != nil {
		t.Fatalf("read stored config: %v", err)
	}
	if string(record.EncryptedSecret) == "new-api-key" {
		t.Fatal("API key was stored in plaintext")
	}
	plaintext, err := secrets.Decrypt(record.EncryptedSecret)
	if err != nil || plaintext != "new-api-key" {
		t.Fatalf("decrypt stored API key = %q, %v", plaintext, err)
	}
}

func TestService_SaveReplacesTheRuntimeAndExposesEffects(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	secrets := integration.NewSecretStore(fixedKey(make([]byte, 32)))
	service := tmdb.NewService(repo, secrets, tmdb.EnvironmentConfig{}, &recordingTester{}, nil)

	view, err := service.Save(context.Background(), tmdb.SaveDraft{
		Revision: 1,
		APIKey:   "first-valid-key",
	})
	if err != nil {
		t.Fatalf("save first valid key: %v", err)
	}
	if !view.Effects.RefreshStale {
		t.Fatalf("save effects = %+v, want stale refresh", view.Effects)
	}
	snapshot, err := service.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire saved runtime: %v", err)
	}
	if snapshot.Revision != view.Revision || snapshot.Config.APIKey != "first-valid-key" {
		t.Fatalf("saved runtime = %+v", snapshot)
	}
}

func TestService_SaveReschedulesFromTheReplacementTime(t *testing.T) {
	service, _ := setupConfigService(t, tmdb.EnvironmentConfig{APIKey: "environment-key"})
	if _, err := service.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire initial runtime: %v", err)
	}
	interval := 2 * time.Hour
	before := time.Now().UTC()

	view, err := service.Save(context.Background(), tmdb.SaveDraft{
		Revision: 1,
		Admin:    tmdb.AdminConfig{RefreshInterval: &interval},
	})
	if err != nil {
		t.Fatalf("save refresh interval: %v", err)
	}
	after := time.Now().UTC()
	if !view.Effects.Reschedule {
		t.Fatalf("save effects = %+v, want reschedule", view.Effects)
	}
	if view.Effects.NextScheduledCheck.Before(before.Add(interval)) ||
		view.Effects.NextScheduledCheck.After(after.Add(interval)) {
		t.Fatalf("next check = %v, want replacement time plus %v", view.Effects.NextScheduledCheck, interval)
	}
}

func TestService_AuthenticationRejectionSuspendsWorkAndPersistsTheCause(t *testing.T) {
	service, _ := setupConfigService(t, tmdb.EnvironmentConfig{APIKey: "rejected-key"})
	snapshot, err := service.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire runtime: %v", err)
	}

	if applied, err := service.AuthenticationRejected(context.Background(), snapshot); err != nil {
		t.Fatalf("reject authentication: %v", err)
	} else if !applied {
		t.Fatal("authentication rejection was not applied")
	}
	if _, err := service.Acquire(context.Background()); !errors.Is(err, tmdb.ErrAPIKeyRejected) {
		t.Fatalf("acquire after rejection error = %v, want API key rejected", err)
	}
	view, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("get rejected state: %v", err)
	}
	if view.State != integration.StateError || view.Reason != "API key rejected" {
		t.Fatalf("rejected state = %q, reason %q", view.State, view.Reason)
	}
}

func TestService_SuccessfulSavedKeyTestResumesWorkAndPersistsConnected(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	secrets := integration.NewSecretStore(fixedKey(make([]byte, 32)))
	tester := &recordingTester{}
	service := tmdb.NewService(repo, secrets, tmdb.EnvironmentConfig{}, tester, nil)
	view, err := service.Save(context.Background(), tmdb.SaveDraft{Revision: 1, APIKey: "saved-key"})
	if err != nil {
		t.Fatalf("save key: %v", err)
	}
	snapshot, err := service.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire runtime: %v", err)
	}
	if applied, err := service.AuthenticationRejected(context.Background(), snapshot); err != nil {
		t.Fatalf("reject authentication: %v", err)
	} else if !applied {
		t.Fatal("authentication rejection was not applied")
	}

	result, err := service.TestConnection(context.Background(), tmdb.SaveDraft{Revision: view.Revision})
	if err != nil {
		t.Fatalf("test saved key: %v", err)
	}
	if result.State != integration.StateConnected {
		t.Fatalf("connection state = %q, want connected", result.State)
	}
	if !result.RuntimeResumed || result.RuntimeRevision != view.Revision {
		t.Fatalf("connection runtime result = %+v, want resumed revision %d", result, view.Revision)
	}
	if _, err := service.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire resumed runtime: %v", err)
	}
	connected, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("get connected state: %v", err)
	}
	if connected.Revision != view.Revision || connected.State != integration.StateConnected || connected.Reason != "" {
		t.Fatalf("connected view = %+v", connected)
	}
	if connected.LastConnectionTestedAt == nil || connected.LastConnectionTestedAt.Unix() != result.CheckedAt.Unix() {
		t.Fatalf("last connection test = %v, result checked at %v", connected.LastConnectionTestedAt, result.CheckedAt)
	}
}

func TestService_SuccessfulDraftKeyTestDoesNotResumeTheSavedCredential(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	secrets := integration.NewSecretStore(fixedKey(make([]byte, 32)))
	service := tmdb.NewService(repo, secrets, tmdb.EnvironmentConfig{}, &recordingTester{}, nil)
	view, err := service.Save(context.Background(), tmdb.SaveDraft{Revision: 1, APIKey: "saved-rejected-key"})
	if err != nil {
		t.Fatalf("save key: %v", err)
	}
	snapshot, err := service.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire runtime: %v", err)
	}
	if applied, err := service.AuthenticationRejected(context.Background(), snapshot); err != nil {
		t.Fatalf("reject authentication: %v", err)
	} else if !applied {
		t.Fatal("authentication rejection was not applied")
	}

	result, err := service.TestConnection(context.Background(), tmdb.SaveDraft{
		Revision: view.Revision,
		APIKey:   "working-draft-key",
	})
	if err != nil {
		t.Fatalf("test draft key: %v", err)
	}
	if result.State != integration.StateConnected {
		t.Fatalf("draft test state = %q, want connected", result.State)
	}
	if _, err := service.Acquire(context.Background()); !errors.Is(err, tmdb.ErrAPIKeyRejected) {
		t.Fatalf("saved runtime error = %v, want API key rejected", err)
	}
	unchanged, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("get unchanged state: %v", err)
	}
	if unchanged.Revision != view.Revision || unchanged.State != integration.StateError {
		t.Fatalf("saved state changed after draft test: %+v", unchanged)
	}
}

func TestService_RejectedDraftKeyTestDoesNotSuspendTheSavedCredential(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	secrets := integration.NewSecretStore(fixedKey(make([]byte, 32)))
	tester := &recordingTester{}
	service := tmdb.NewService(repo, secrets, tmdb.EnvironmentConfig{}, tester, nil)
	view, err := service.Save(context.Background(), tmdb.SaveDraft{Revision: 1, APIKey: "working-saved-key"})
	if err != nil {
		t.Fatalf("save working key: %v", err)
	}
	tester.err = tmdb.ErrAuthentication

	result, err := service.TestConnection(context.Background(), tmdb.SaveDraft{
		Revision: view.Revision,
		APIKey:   "rejected-draft-key",
	})
	if err != nil {
		t.Fatalf("test rejected draft key: %v", err)
	}
	if result.State != integration.StateError || result.RuntimeRevision != 0 {
		t.Fatalf("draft rejection result = %+v, want no saved runtime revision", result)
	}
	current, err := service.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire saved runtime after draft rejection: %v", err)
	}
	if current.Config.APIKey != "working-saved-key" {
		t.Fatalf("saved runtime key = %q, want unchanged", current.Config.APIKey)
	}
}

func TestService_RejectedSavedKeyTestSuspendsWork(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	secrets := integration.NewSecretStore(fixedKey(make([]byte, 32)))
	tester := &recordingTester{}
	service := tmdb.NewService(repo, secrets, tmdb.EnvironmentConfig{}, tester, nil)
	view, err := service.Save(context.Background(), tmdb.SaveDraft{Revision: 1, APIKey: "saved-key"})
	if err != nil {
		t.Fatalf("save key: %v", err)
	}
	tester.err = tmdb.ErrAuthentication

	result, err := service.TestConnection(context.Background(), tmdb.SaveDraft{Revision: view.Revision})
	if err != nil {
		t.Fatalf("test saved key: %v", err)
	}
	if result.State != integration.StateError || result.Reason != "API key rejected" {
		t.Fatalf("rejected test result = %+v", result)
	}
	if result.RuntimeRevision != view.Revision {
		t.Fatalf("rejected runtime revision = %d, want %d", result.RuntimeRevision, view.Revision)
	}
	if _, err := service.Acquire(context.Background()); !errors.Is(err, tmdb.ErrAPIKeyRejected) {
		t.Fatalf("acquire after rejected test error = %v, want API key rejected", err)
	}
	failed, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("get rejected state: %v", err)
	}
	if failed.State != integration.StateError || failed.Reason != "API key rejected" {
		t.Fatalf("saved rejection state = %+v", failed)
	}
}

func TestService_UnreachableSavedKeyTestPersistsCouldNotVerify(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	secrets := integration.NewSecretStore(fixedKey(make([]byte, 32)))
	tester := &recordingTester{}
	service := tmdb.NewService(repo, secrets, tmdb.EnvironmentConfig{}, tester, nil)
	view, err := service.Save(context.Background(), tmdb.SaveDraft{Revision: 1, APIKey: "saved-key"})
	if err != nil {
		t.Fatalf("save key: %v", err)
	}
	tester.err = errors.New("network unavailable")

	result, err := service.TestConnection(context.Background(), tmdb.SaveDraft{Revision: view.Revision})
	if err != nil {
		t.Fatalf("test saved key: %v", err)
	}
	if result.State != integration.StateCouldNotVerify {
		t.Fatalf("test state = %q, want could not verify", result.State)
	}
	persisted, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("get tested state: %v", err)
	}
	if persisted.State != integration.StateCouldNotVerify || persisted.Reason == "" {
		t.Fatalf("persisted state = %+v, want could not verify", persisted)
	}
	if persisted.LastConnectionTestedAt == nil || persisted.LastConnectionTestedAt.Unix() != result.CheckedAt.Unix() {
		t.Fatalf("last connection test = %v, result checked at %v", persisted.LastConnectionTestedAt, result.CheckedAt)
	}
}

func TestService_UnrelatedSaveKeepsTheRejectedRuntimeSuspended(t *testing.T) {
	service, _ := setupConfigService(t, tmdb.EnvironmentConfig{APIKey: "rejected-key"})
	snapshot, err := service.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire runtime: %v", err)
	}
	if applied, err := service.AuthenticationRejected(context.Background(), snapshot); err != nil {
		t.Fatalf("reject authentication: %v", err)
	} else if !applied {
		t.Fatal("authentication rejection was not applied")
	}

	view, err := service.Save(context.Background(), tmdb.SaveDraft{
		Revision: 1,
		Admin:    tmdb.AdminConfig{CastLimit: new(30)},
	})
	if err != nil {
		t.Fatalf("save unrelated setting: %v", err)
	}
	if view.State != integration.StateError || view.Reason != "API key rejected" {
		t.Fatalf("state after unrelated save = %+v", view)
	}
	if _, err := service.Acquire(context.Background()); !errors.Is(err, tmdb.ErrAPIKeyRejected) {
		t.Fatalf("acquire after unrelated save error = %v, want API key rejected", err)
	}
}

type blockingConfigGetStore struct {
	integration.ConfigStore
	blockNext atomic.Bool
	loaded    chan struct{}
	release   chan struct{}
}

func (s *blockingConfigGetStore) Get(ctx context.Context, name string) (integration.ConfigRecord, error) {
	record, err := s.ConfigStore.Get(ctx, name)
	if err == nil && s.blockNext.CompareAndSwap(true, false) {
		close(s.loaded)
		select {
		case <-ctx.Done():
			return integration.ConfigRecord{}, ctx.Err()
		case <-s.release:
		}
	}
	return record, err
}

func TestService_AuthenticationRejectionDuringUnrelatedSaveStaysAuthoritative(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	if err := repo.UpdateState(context.Background(), "tmdb", integration.StateConnected, ""); err != nil {
		t.Fatalf("seed connected state: %v", err)
	}
	store := &blockingConfigGetStore{
		ConfigStore: repo,
		loaded:      make(chan struct{}),
		release:     make(chan struct{}),
	}
	service := tmdb.NewService(store, nil, tmdb.EnvironmentConfig{APIKey: "saved-key"}, nil, nil)
	snapshot, err := service.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire runtime: %v", err)
	}

	store.blockNext.Store(true)
	saved := make(chan tmdb.ConfigView, 1)
	saveErr := make(chan error, 1)
	go func() {
		view, err := service.Save(context.Background(), tmdb.SaveDraft{
			Revision: 1,
			Admin:    tmdb.AdminConfig{CastLimit: new(30)},
		})
		saved <- view
		saveErr <- err
	}()
	<-store.loaded
	if applied, err := service.AuthenticationRejected(context.Background(), snapshot); err != nil {
		t.Fatalf("reject authentication during save: %v", err)
	} else if !applied {
		t.Fatal("authentication rejection during save was not applied")
	}
	close(store.release)
	if err := <-saveErr; err != nil {
		t.Fatalf("save unrelated setting: %v", err)
	}
	view := <-saved
	if view.State != integration.StateError || view.Reason != "API key rejected" {
		t.Fatalf("saved view = %+v, want rejected state", view)
	}
	if _, err := service.Acquire(context.Background()); !errors.Is(err, tmdb.ErrAPIKeyRejected) {
		t.Fatalf("acquire after concurrent rejection error = %v, want API key rejected", err)
	}
}

func TestService_SuccessfulKeyReplacementResumesTheRuntime(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	secrets := integration.NewSecretStore(fixedKey(make([]byte, 32)))
	service := tmdb.NewService(repo, secrets, tmdb.EnvironmentConfig{}, &recordingTester{}, nil)
	view, err := service.Save(context.Background(), tmdb.SaveDraft{Revision: 1, APIKey: "rejected-key"})
	if err != nil {
		t.Fatalf("save original key: %v", err)
	}
	snapshot, err := service.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire runtime: %v", err)
	}
	if applied, err := service.AuthenticationRejected(context.Background(), snapshot); err != nil {
		t.Fatalf("reject authentication: %v", err)
	} else if !applied {
		t.Fatal("authentication rejection was not applied")
	}

	view, err = service.Save(context.Background(), tmdb.SaveDraft{
		Revision: view.Revision,
		APIKey:   "replacement-key",
	})
	if err != nil {
		t.Fatalf("save replacement key: %v", err)
	}
	if view.State != integration.StateConnected || view.Reason != "" {
		t.Fatalf("replacement state = %+v", view)
	}
	resumed, err := service.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire replacement runtime: %v", err)
	}
	if resumed.Revision != view.Revision || resumed.Config.APIKey != "replacement-key" {
		t.Fatalf("replacement runtime = %+v", resumed)
	}
}

func TestService_SuccessfulSaveOfRejectedKeyResumesTheRuntime(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	secrets := integration.NewSecretStore(fixedKey(make([]byte, 32)))
	service := tmdb.NewService(repo, secrets, tmdb.EnvironmentConfig{}, &recordingTester{}, nil)
	view, err := service.Save(context.Background(), tmdb.SaveDraft{Revision: 1, APIKey: "same-key"})
	if err != nil {
		t.Fatalf("save original key: %v", err)
	}
	snapshot, err := service.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire runtime: %v", err)
	}
	if applied, err := service.AuthenticationRejected(context.Background(), snapshot); err != nil {
		t.Fatalf("reject authentication: %v", err)
	} else if !applied {
		t.Fatal("authentication rejection was not applied")
	}

	view, err = service.Save(context.Background(), tmdb.SaveDraft{
		Revision: view.Revision,
		APIKey:   "same-key",
	})
	if err != nil {
		t.Fatalf("save verified key again: %v", err)
	}
	if view.State != integration.StateConnected || view.Reason != "" {
		t.Fatalf("saved state = %+v, want connected", view)
	}
	resumed, err := service.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire verified runtime: %v", err)
	}
	if resumed.Revision != view.Revision || resumed.Config.APIKey != "same-key" {
		t.Fatalf("verified runtime = %+v", resumed)
	}
}

func TestService_ConcurrentReplacementOutranksOldAuthenticationFailure(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	secrets := integration.NewSecretStore(fixedKey(make([]byte, 32)))
	service := tmdb.NewService(repo, secrets, tmdb.EnvironmentConfig{}, &recordingTester{}, nil)
	view, err := service.Save(context.Background(), tmdb.SaveDraft{Revision: 1, APIKey: "key-0"})
	if err != nil {
		t.Fatalf("save initial key: %v", err)
	}

	for replacement := 1; replacement <= 20; replacement++ {
		oldWork, err := service.Acquire(context.Background())
		if err != nil {
			t.Fatalf("acquire revision %d: %v", view.Revision, err)
		}
		start := make(chan struct{})
		type saveOutcome struct {
			view tmdb.ConfigView
			err  error
		}
		saved := make(chan saveOutcome, 1)
		rejected := make(chan error, 1)
		var workers sync.WaitGroup
		workers.Add(2)
		go func() {
			defer workers.Done()
			<-start
			_, err := service.AuthenticationRejected(context.Background(), oldWork)
			rejected <- err
		}()
		go func(revision int64, apiKey string) {
			defer workers.Done()
			<-start
			next, err := service.Save(context.Background(), tmdb.SaveDraft{Revision: revision, APIKey: apiKey})
			saved <- saveOutcome{view: next, err: err}
		}(view.Revision, fmt.Sprintf("key-%d", replacement))
		close(start)
		workers.Wait()
		if err := <-rejected; err != nil {
			t.Fatalf("record old rejection: %v", err)
		}
		outcome := <-saved
		if outcome.err != nil {
			t.Fatalf("save replacement %d: %v", replacement, outcome.err)
		}
		view = outcome.view

		current, err := service.Acquire(context.Background())
		if err != nil {
			t.Fatalf("acquire replacement %d: %v", replacement, err)
		}
		wantKey := fmt.Sprintf("key-%d", replacement)
		if current.Revision != view.Revision || current.Config.APIKey != wantKey {
			t.Fatalf("replacement runtime = %+v, want revision %d key %q", current, view.Revision, wantKey)
		}
		currentView, err := service.Get(context.Background())
		if err != nil {
			t.Fatalf("get replacement state: %v", err)
		}
		if currentView.State != integration.StateConnected || currentView.Reason != "" {
			t.Fatalf("replacement state = %+v", currentView)
		}
	}
}

func TestService_TestConnectionUsesTheDraftWithoutSaving(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	tester := &recordingTester{}
	service := tmdb.NewService(repo, integration.NewSecretStore(fixedKey(make([]byte, 32))), tmdb.EnvironmentConfig{}, tester, nil)
	interval := 400 * time.Millisecond

	result, err := service.TestConnection(context.Background(), tmdb.SaveDraft{
		Revision: 1,
		Admin: tmdb.AdminConfig{
			MinInterval: &interval,
		},
		APIKey: "draft-api-key",
	})
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if result.State != integration.StateConnected {
		t.Fatalf("state = %q, want connected", result.State)
	}
	if tester.config.APIKey != "draft-api-key" || tester.config.MinInterval != interval {
		t.Fatalf("tested runtime = %+v", tester.config)
	}
	record, err := repo.Get(context.Background(), "tmdb")
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if record.Revision != 1 || len(record.EncryptedSecret) != 0 {
		t.Fatalf("test connection persisted the draft: %+v", record)
	}
}

func TestService_ClearAPIKeyDisablesTMDBWithoutDeletingOtherSettings(t *testing.T) {
	_, repo := setupConfigService(t, tmdb.EnvironmentConfig{})
	secrets := integration.NewSecretStore(fixedKey(make([]byte, 32)))
	service := tmdb.NewService(repo, secrets, tmdb.EnvironmentConfig{}, &recordingTester{}, nil)
	first, err := service.Save(context.Background(), tmdb.SaveDraft{
		Revision: 1,
		Admin:    tmdb.AdminConfig{CastLimit: new(30)},
		APIKey:   "stored-key",
	})
	if err != nil {
		t.Fatalf("seed API key: %v", err)
	}

	view, err := service.Save(context.Background(), tmdb.SaveDraft{
		Revision:    first.Revision,
		Admin:       tmdb.AdminConfig{CastLimit: new(30)},
		ClearAPIKey: true,
	})
	if err != nil {
		t.Fatalf("clear API key: %v", err)
	}
	if view.State != integration.StateDisabled || view.Config.APIKey.Configured {
		t.Fatalf("view after clear = %+v", view)
	}
	if view.Config.CastLimit.Value != 30 {
		t.Fatalf("cast limit changed while clearing key: %+v", view.Config.CastLimit)
	}
	record, err := repo.Get(context.Background(), "tmdb")
	if err != nil {
		t.Fatalf("read stored config: %v", err)
	}
	if len(record.EncryptedSecret) != 0 {
		t.Fatal("encrypted API key was not cleared")
	}
}
