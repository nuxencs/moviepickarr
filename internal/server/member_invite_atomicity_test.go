package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"moviepickarr/internal/auth"

	"github.com/gofiber/fiber/v2"
)

func TestHandleCreateUser_ReturnsClaimSecretOnlyInDirectResponse(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	client, _ := e.h.broker.Subscribe()
	defer e.h.broker.Unsubscribe(client)

	resp := e.request(t, http.MethodPost, "/api/v1/members", adminCookie, map[string]string{"name": "Private link"})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	var body createMemberResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	rawToken := tokenFromClaimURL(t, body.ClaimURL)

	var storedHash string
	if err := e.pool.Read.QueryRowContext(context.Background(),
		"SELECT token_hash FROM invites WHERE user_id = ?", body.ID,
	).Scan(&storedHash); err != nil {
		t.Fatalf("read stored invite: %v", err)
	}
	if storedHash != auth.HashToken(rawToken) || storedHash == rawToken {
		t.Fatalf("stored token = %q, want only hash of response token", storedHash)
	}

	select {
	case broadcast := <-client:
		if broadcast.Type != "user:created" {
			t.Fatalf("broadcast type = %q, want user:created", broadcast.Type)
		}
		payload, err := json.Marshal(broadcast.Data)
		if err != nil {
			t.Fatalf("marshal broadcast: %v", err)
		}
		if bytes.Contains(payload, []byte("claimUrl")) || bytes.Contains(payload, []byte(rawToken)) {
			t.Fatalf("broadcast leaked one-time claim secret: %s", payload)
		}
	default:
		t.Fatal("create emitted no user:created broadcast")
	}
}

func TestHandleCreateUser_InviteFailureRollsBackMemberAndNextUp(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	if _, err := e.pool.Write.ExecContext(context.Background(), `
		CREATE TRIGGER fail_http_member_invite
		AFTER INSERT ON invites
		BEGIN
			INSERT INTO sessions (
				public_id, token_hash, user_id, expires_at, last_seen_at
			) VALUES (NULL, 'forced-http-failure', NEW.user_id, 1, 1);
		END
	`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	resp := e.request(t, http.MethodPost, "/api/v1/members", adminCookie, map[string]string{"name": "Rolled back"})
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("create through invite failure = %d, want 500", resp.StatusCode)
	}

	var members, invites int
	if err := e.pool.Read.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM users WHERE name = 'Rolled back'",
	).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.Read.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM invites",
	).Scan(&invites); err != nil {
		t.Fatal(err)
	}
	var nextUp sql.NullInt64
	if err := e.pool.Read.QueryRowContext(context.Background(),
		"SELECT user_id FROM next_up WHERE id = 1",
	).Scan(&nextUp); err != nil {
		t.Fatal(err)
	}
	if members != 0 || invites != 0 || nextUp.Valid {
		t.Fatalf("failed HTTP create left members=%d invites=%d nextUp=%v", members, invites, nextUp)
	}
}

func TestHandleRestoreUser_DeliversClaimURLWithoutPostCommitRead(t *testing.T) {
	ctx := context.Background()
	h, app, users, _, pool := setupEditMovieTestWithDB(t)
	admin, err := users.Create(ctx, "Admin")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	returning, err := users.Create(ctx, "Returning without read pool")
	if err != nil {
		t.Fatalf("create returning member: %v", err)
	}
	if _, err := pool.Write.ExecContext(ctx,
		"UPDATE users SET archived_at = unixepoch() WHERE id = ?", returning.ID,
	); err != nil {
		t.Fatalf("archive returning member: %v", err)
	}
	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	// Lifecycle writes and their response projection use the writer transaction.
	// Closing readers proves the committed raw token is not stranded behind a
	// fallible post-commit roster lookup.
	if err := pool.Read.Close(); err != nil {
		t.Fatalf("close read pool: %v", err)
	}
	resp := doAs(t, app,
		jsonReq(http.MethodPost, fmt.Sprintf("/api/v1/members/%d/restore", returning.ID), ``),
		admin.ID,
		"admin",
	)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("restore with unavailable read pool = %d, want 200", resp.StatusCode)
	}
	var body createMemberResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode restore response: %v", err)
	}
	if body.ID != returning.ID {
		t.Fatalf("restored member id = %d, want %d", body.ID, returning.ID)
	}
	rawToken := tokenFromClaimURL(t, body.ClaimURL)
	var storedHash string
	if err := pool.Write.QueryRowContext(ctx,
		"SELECT token_hash FROM invites WHERE user_id = ?", returning.ID,
	).Scan(&storedHash); err != nil {
		t.Fatalf("read restored invite on writer: %v", err)
	}
	if storedHash != auth.HashToken(rawToken) {
		t.Fatalf("restored invite hash = %q, want response token hash", storedHash)
	}

	select {
	case broadcast := <-client:
		payload, err := json.Marshal(broadcast.Data)
		if err != nil {
			t.Fatalf("marshal restore broadcast: %v", err)
		}
		if bytes.Contains(payload, []byte("claimUrl")) || bytes.Contains(payload, []byte(rawToken)) {
			t.Fatalf("restore broadcast leaked one-time claim secret: %s", payload)
		}
	default:
		t.Fatal("restore emitted no user:created broadcast")
	}
}
