package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/integration"
)

func setupIntegrationConfigRepo(t *testing.T) *SqliteIntegrationConfigRepository {
	t.Helper()
	pool, err := db.OpenSQLite(filepath.Join(t.TempDir(), "integrations.db"))
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.RunMigrations(context.Background(), pool.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	return NewSqliteIntegrationConfigRepository(pool)
}

func TestIntegrationConfigRepository_SavesDraftAndSecretAtomically(t *testing.T) {
	repo := setupIntegrationConfigRepo(t)

	record, err := repo.Save(context.Background(), integration.ConfigSave{
		Integration:      "tmdb",
		ExpectedRevision: 1,
		AdminConfig:      []byte(`{"castLimit":30}`),
		SecretAction:     integration.SecretReplace,
		EncryptedSecret:  []byte("sealed-key"),
		State:            integration.StateConnected,
	})
	if err != nil {
		t.Fatalf("save config: %v", err)
	}
	if record.Revision != 2 || string(record.AdminConfig) != `{"castLimit":30}` {
		t.Fatalf("saved record = %+v", record)
	}
	if got := string(record.EncryptedSecret); got != "sealed-key" {
		t.Fatalf("encrypted secret = %q", got)
	}
	if record.State != integration.StateConnected {
		t.Fatalf("state = %q, want connected", record.State)
	}
}

func TestIntegrationConfigRepository_SeedsTMDBConfiguration(t *testing.T) {
	repo := setupIntegrationConfigRepo(t)

	record, err := repo.Get(context.Background(), "tmdb")
	if err != nil {
		t.Fatalf("get TMDB config: %v", err)
	}
	if record.Revision != 1 {
		t.Fatalf("revision = %d, want 1", record.Revision)
	}
	if got := string(record.AdminConfig); got != "{}" {
		t.Fatalf("admin config = %q, want {}", got)
	}
	if len(record.EncryptedSecret) != 0 {
		t.Fatal("fresh config should not contain an encrypted secret")
	}
}

func TestIntegrationConfigRepository_UpdatesScheduleAndSuccessfulRefreshWithoutBumpingRevision(t *testing.T) {
	repo := setupIntegrationConfigRepo(t)
	checkedAt := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	nextAt := checkedAt.Add(time.Hour)
	succeededAt := checkedAt.Add(5 * time.Minute)
	connectionTestedAt := checkedAt.Add(10 * time.Minute)

	if err := repo.UpdateSchedule(context.Background(), "tmdb", checkedAt, &nextAt); err != nil {
		t.Fatalf("update schedule: %v", err)
	}
	if err := repo.UpdateSuccessfulRun(context.Background(), "tmdb", succeededAt); err != nil {
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
	laterCheck := succeededAt.Add(time.Minute)
	if err := repo.UpdateLastChecked(context.Background(), "tmdb", laterCheck); err != nil {
		t.Fatalf("update last checked: %v", err)
	}
	rescheduledAt := nextAt.Add(time.Hour)
	if err := repo.UpdateNextCheck(context.Background(), "tmdb", &rescheduledAt); err != nil {
		t.Fatalf("update next check: %v", err)
	}
	record, err := repo.Get(context.Background(), "tmdb")
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if record.Revision != 1 {
		t.Fatalf("revision = %d, want unchanged", record.Revision)
	}
	if record.LastCheckedAt == nil || !record.LastCheckedAt.Equal(laterCheck) {
		t.Fatalf("last checked = %v", record.LastCheckedAt)
	}
	if record.NextCheckAt == nil || !record.NextCheckAt.Equal(rescheduledAt) {
		t.Fatalf("next check = %v", record.NextCheckAt)
	}
	if record.LastSuccessfulRunAt == nil || !record.LastSuccessfulRunAt.Equal(succeededAt) {
		t.Fatalf("last successful run = %v", record.LastSuccessfulRunAt)
	}
	if record.LastConnectionTestedAt == nil || !record.LastConnectionTestedAt.Equal(connectionTestedAt) {
		t.Fatalf("last connection test = %v", record.LastConnectionTestedAt)
	}
}

func TestIntegrationConfigRepository_ConnectionTestDoesNotReplaceLibraryScan(t *testing.T) {
	repo := setupIntegrationConfigRepo(t)
	scanAt := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	connectionTestedAt := scanAt.Add(30 * time.Minute)
	if err := repo.UpdateLastChecked(context.Background(), "tmdb", scanAt); err != nil {
		t.Fatalf("record library scan: %v", err)
	}
	if err := repo.UpdateConnectionTest(
		context.Background(),
		"tmdb",
		integration.StateCouldNotVerify,
		"TMDB could not be reached.",
		connectionTestedAt,
	); err != nil {
		t.Fatalf("record connection test: %v", err)
	}

	record, err := repo.Get(context.Background(), "tmdb")
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if record.LastCheckedAt == nil || !record.LastCheckedAt.Equal(scanAt) {
		t.Fatalf("last library scan = %v, want %v", record.LastCheckedAt, scanAt)
	}
	if record.LastConnectionTestedAt == nil || !record.LastConnectionTestedAt.Equal(connectionTestedAt) {
		t.Fatalf("last connection test = %v, want %v", record.LastConnectionTestedAt, connectionTestedAt)
	}
}
