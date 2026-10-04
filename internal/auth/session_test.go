package auth

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"

	"moviepickarr/internal/domain"
)

// fakeSessionRepo is an in-memory SessionRepo for clock-driven window tests.
type fakeSessionRepo struct {
	rows      map[string]*domain.AuthSession // keyed by token_hash
	nextID    int64
	touchedID int64
	touchedTo time.Time
	touches   int
}

func newFakeSessionRepo() *fakeSessionRepo {
	return &fakeSessionRepo{rows: map[string]*domain.AuthSession{}}
}

func (f *fakeSessionRepo) Create(_ context.Context, s domain.Session) error {
	f.nextID++
	f.rows[s.TokenHash] = &domain.AuthSession{Session: s, Role: "member"}
	f.rows[s.TokenHash].ID = f.nextID
	return nil
}

func (f *fakeSessionRepo) FindByTokenHash(_ context.Context, tokenHash string) (*domain.AuthSession, error) {
	as, ok := f.rows[tokenHash]
	if !ok {
		return nil, sql.ErrNoRows
	}
	clone := *as
	return &clone, nil
}

func (f *fakeSessionRepo) TouchLastSeen(_ context.Context, id int64, lastSeen time.Time) error {
	f.touches++
	f.touchedID = id
	f.touchedTo = lastSeen
	for _, as := range f.rows {
		if as.ID == id {
			as.LastSeenAt = lastSeen
		}
	}
	return nil
}

func (f *fakeSessionRepo) DeleteByTokenHash(_ context.Context, tokenHash string) error {
	delete(f.rows, tokenHash)
	return nil
}

func (f *fakeSessionRepo) DeleteByUserID(_ context.Context, userID int) (int64, error) {
	var n int64
	for h, as := range f.rows {
		if as.UserID == userID {
			delete(f.rows, h)
			n++
		}
	}
	return n, nil
}

func (f *fakeSessionRepo) DeleteOthersByUserID(_ context.Context, userID int, keep string) (int64, error) {
	var n int64
	for h, as := range f.rows {
		if as.UserID == userID && h != keep {
			delete(f.rows, h)
			n++
		}
	}
	return n, nil
}

func (f *fakeSessionRepo) DeleteByPublicIDForUser(_ context.Context, publicID string, userID int) (string, error) {
	for h, as := range f.rows {
		if as.PublicID == publicID && as.UserID == userID {
			delete(f.rows, h)
			return h, nil
		}
	}
	return "", nil
}

func (f *fakeSessionRepo) ListLiveByUserID(_ context.Context, userID int, now, idleCutoff time.Time) ([]domain.Session, error) {
	var live []domain.Session
	for _, as := range f.rows {
		if as.UserID == userID && as.ExpiresAt.After(now) && as.LastSeenAt.After(idleCutoff) {
			live = append(live, as.Session)
		}
	}
	// Match the real store's last-activity order, not map order.
	slices.SortFunc(live, func(a, b domain.Session) int {
		if c := b.LastSeenAt.Compare(a.LastSeenAt); c != 0 {
			return c
		}
		return cmp.Compare(b.ID, a.ID)
	})
	return live, nil
}

func (f *fakeSessionRepo) DeleteExpired(_ context.Context, now, idleCutoff time.Time) (int64, error) {
	var n int64
	for h, as := range f.rows {
		if !as.ExpiresAt.After(now) || !as.LastSeenAt.After(idleCutoff) {
			delete(f.rows, h)
			n++
		}
	}
	return n, nil
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func mintFor(t *testing.T, m *SessionManager, userID int) string {
	t.Helper()
	raw, _, err := m.Mint(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return raw
}

func TestMint_StoresHashNotRawAndSetsAbsoluteCap(t *testing.T) {
	repo := newFakeSessionRepo()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clk := &fakeClock{t: base}
	m := NewSessionManager(repo, WithClock(clk.now))

	raw, expiresAt, err := m.Mint(context.Background(), 7, nil)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if raw == "" {
		t.Fatal("mint returned empty raw token")
	}
	if _, ok := repo.rows[raw]; ok {
		t.Fatal("raw token must never be a stored key")
	}
	stored, ok := repo.rows[HashToken(raw)]
	if !ok {
		t.Fatal("token hash not stored")
	}
	if stored.UserID != 7 {
		t.Fatalf("stored user id = %d, want 7", stored.UserID)
	}
	if stored.PublicID == "" {
		t.Fatal("stored public id is empty")
	}
	if want := base.Add(SessionAbsoluteTTL); !expiresAt.Equal(want) {
		t.Fatalf("expiresAt = %v, want %v", expiresAt, want)
	}
	if !stored.ExpiresAt.Equal(base.Add(SessionAbsoluteTTL)) {
		t.Fatalf("stored expires_at = %v, want %v", stored.ExpiresAt, base.Add(SessionAbsoluteTTL))
	}
	if !stored.LastSeenAt.Equal(base) {
		t.Fatalf("stored last_seen_at = %v, want %v", stored.LastSeenAt, base)
	}
}

func TestAuthenticate_ValidSessionCarriesRole(t *testing.T) {
	repo := newFakeSessionRepo()
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	m := NewSessionManager(repo, WithClock(clk.now))
	raw := mintFor(t, m, 3)

	as, err := m.Authenticate(context.Background(), raw)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if as.UserID != 3 {
		t.Fatalf("user id = %d, want 3", as.UserID)
	}
	if as.Role != "member" {
		t.Fatalf("role = %q, want member", as.Role)
	}
}

func TestAuthenticate_EmptyAndUnknownTokenRejected(t *testing.T) {
	repo := newFakeSessionRepo()
	m := NewSessionManager(repo)

	if _, err := m.Authenticate(context.Background(), ""); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("empty token err = %v, want ErrSessionInvalid", err)
	}
	if _, err := m.Authenticate(context.Background(), "nope"); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("unknown token err = %v, want ErrSessionInvalid", err)
	}
}

func TestAuthenticate_AbsoluteExpiryBoundary(t *testing.T) {
	repo := newFakeSessionRepo()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := &fakeClock{t: base}
	m := NewSessionManager(repo, WithClock(clk.now))
	raw := mintFor(t, m, 1)

	// Keep last_seen fresh so only the absolute cap can reject.
	clk.t = base.Add(SessionAbsoluteTTL - time.Second)
	repo.rows[HashToken(raw)].LastSeenAt = clk.t
	if _, err := m.Authenticate(context.Background(), raw); err != nil {
		t.Fatalf("just before absolute cap: %v", err)
	}

	clk.t = base.Add(SessionAbsoluteTTL)
	repo.rows[HashToken(raw)].LastSeenAt = clk.t
	if _, err := m.Authenticate(context.Background(), raw); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("at absolute cap err = %v, want ErrSessionInvalid", err)
	}
}

func TestAuthenticate_IdleExpiryBoundary(t *testing.T) {
	repo := newFakeSessionRepo()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := &fakeClock{t: base}
	m := NewSessionManager(repo, WithClock(clk.now))
	raw := mintFor(t, m, 1)

	clk.t = base.Add(SessionIdleTTL - time.Second)
	if _, err := m.Authenticate(context.Background(), raw); err != nil {
		t.Fatalf("just before idle window: %v", err)
	}

	// Re-mint: the check above slid last_seen forward.
	repo2 := newFakeSessionRepo()
	clk2 := &fakeClock{t: base}
	m2 := NewSessionManager(repo2, WithClock(clk2.now))
	raw2 := mintFor(t, m2, 1)
	clk2.t = base.Add(SessionIdleTTL)
	if _, err := m2.Authenticate(context.Background(), raw2); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("at idle window err = %v, want ErrSessionInvalid", err)
	}
}

func TestAuthenticate_SlidesOnlyWhenStale(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	repo := newFakeSessionRepo()
	clk := &fakeClock{t: base}
	m := NewSessionManager(repo, WithClock(clk.now))
	raw := mintFor(t, m, 1)
	clk.advance(30 * time.Minute)
	if _, err := m.Authenticate(context.Background(), raw); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if repo.touches != 0 {
		t.Fatalf("touches = %d, want 0 (under threshold)", repo.touches)
	}

	repo2 := newFakeSessionRepo()
	clk2 := &fakeClock{t: base}
	m2 := NewSessionManager(repo2, WithClock(clk2.now))
	raw2 := mintFor(t, m2, 1)
	clk2.advance(2 * time.Hour)
	if _, err := m2.Authenticate(context.Background(), raw2); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if repo2.touches != 1 {
		t.Fatalf("touches = %d, want 1 (over threshold)", repo2.touches)
	}
	if !repo2.touchedTo.Equal(base.Add(2 * time.Hour)) {
		t.Fatalf("slid to %v, want %v", repo2.touchedTo, base.Add(2*time.Hour))
	}
}

// The SSE heartbeat relies on this, so a long-held stream cannot keep an idle
// session alive.
func TestRevalidate_DropsRevokedWithoutSliding(t *testing.T) {
	repo := newFakeSessionRepo()
	clk := &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	m := NewSessionManager(repo, WithClock(clk.now))
	raw := mintFor(t, m, 5)

	clk.advance(2 * time.Hour)
	if err := m.Revalidate(context.Background(), raw); err != nil {
		t.Fatalf("revalidate live session: %v", err)
	}
	if repo.touches != 0 {
		t.Fatalf("Revalidate slid the idle window (touches = %d, want 0)", repo.touches)
	}

	if err := m.Revalidate(context.Background(), ""); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("empty token: got %v, want ErrSessionInvalid", err)
	}

	if err := m.RevokeCurrent(context.Background(), raw); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := m.Revalidate(context.Background(), raw); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked token: got %v, want ErrSessionInvalid", err)
	}
}

func TestRevalidate_RejectsIdleExpired(t *testing.T) {
	repo := newFakeSessionRepo()
	clk := &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	m := NewSessionManager(repo, WithClock(clk.now))
	raw := mintFor(t, m, 5)

	clk.advance(SessionIdleTTL + time.Hour)
	if err := m.Revalidate(context.Background(), raw); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("idle-expired: got %v, want ErrSessionInvalid", err)
	}
}

func TestRevoke_DelegatesWithHashedTokens(t *testing.T) {
	repo := newFakeSessionRepo()
	m := NewSessionManager(repo)
	keep := mintFor(t, m, 1)
	other := mintFor(t, m, 1)
	stranger := mintFor(t, m, 2)

	if err := m.RevokeOthers(context.Background(), 1, keep); err != nil {
		t.Fatalf("revoke others: %v", err)
	}
	if _, ok := repo.rows[HashToken(keep)]; !ok {
		t.Fatal("revoke-others dropped the kept session")
	}
	if _, ok := repo.rows[HashToken(other)]; ok {
		t.Fatal("revoke-others kept the other session")
	}
	if _, ok := repo.rows[HashToken(stranger)]; !ok {
		t.Fatal("revoke-others touched another member's session")
	}

	if err := m.RevokeCurrent(context.Background(), keep); err != nil {
		t.Fatalf("revoke current: %v", err)
	}
	if _, ok := repo.rows[HashToken(keep)]; ok {
		t.Fatal("revoke-current left the session")
	}

	if err := m.RevokeAll(context.Background(), 2); err != nil {
		t.Fatalf("revoke all: %v", err)
	}
	if _, ok := repo.rows[HashToken(stranger)]; ok {
		t.Fatal("revoke-all left a session")
	}
}

func TestSweep_UsesIdleCutoff(t *testing.T) {
	repo := newFakeSessionRepo()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := &fakeClock{t: base}
	m := NewSessionManager(repo, WithClock(clk.now))
	raw := mintFor(t, m, 1)

	if n, err := m.Sweep(context.Background()); err != nil || n != 0 {
		t.Fatalf("sweep fresh = (%d, %v), want (0, nil)", n, err)
	}

	clk.advance(SessionIdleTTL + time.Hour)
	if n, err := m.Sweep(context.Background()); err != nil || n != 1 {
		t.Fatalf("sweep idle-expired = (%d, %v), want (1, nil)", n, err)
	}
	if _, ok := repo.rows[HashToken(raw)]; ok {
		t.Fatal("idle-expired session survived the sweep")
	}
}

func TestList_LiveOnlyWithCurrentFlagged(t *testing.T) {
	repo := newFakeSessionRepo()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := &fakeClock{t: base}
	m := NewSessionManager(repo, WithClock(clk.now))
	ctx := context.Background()

	old := mintFor(t, m, 1)
	clk.advance(time.Hour)
	current := mintFor(t, m, 1)
	stranger := mintFor(t, m, 2)

	views, err := m.List(ctx, 1, current)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("list returned %d sessions, want 2", len(views))
	}
	if !views[0].Current {
		t.Error("newest session is the caller's, want current flagged")
	}
	if views[1].Current {
		t.Error("older session flagged current")
	}
	if views[0].TokenHash != HashToken(current) || views[1].TokenHash != HashToken(old) {
		t.Error("sessions not ordered by last activity, newest first")
	}
	for _, v := range views {
		if v.TokenHash == HashToken(stranger) {
			t.Fatal("list leaked another member's session")
		}
	}

	clk.advance(SessionIdleTTL + time.Hour)
	views, err = m.List(ctx, 1, current)
	if err != nil {
		t.Fatalf("list after idle: %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("list returned %d idle-expired sessions, want 0", len(views))
	}
}

func TestList_NoCurrentTokenFlagsNothing(t *testing.T) {
	repo := newFakeSessionRepo()
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	m := NewSessionManager(repo, WithClock(clk.now))
	mintFor(t, m, 1)

	views, err := m.List(context.Background(), 1, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(views) != 1 || views[0].Current {
		t.Fatalf("empty current token flagged a session as current")
	}
}

func TestRevokeByPublicID_ScopedToOwner(t *testing.T) {
	repo := newFakeSessionRepo()
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	m := NewSessionManager(repo, WithClock(clk.now))
	ctx := context.Background()

	current := mintFor(t, m, 1)
	other := mintFor(t, m, 1)
	stranger := mintFor(t, m, 2)

	views, err := m.List(ctx, 1, current)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var otherID, currentID string
	for _, v := range views {
		if v.TokenHash == HashToken(other) {
			otherID = v.PublicID
		}
		if v.Current {
			currentID = v.PublicID
		}
	}

	wasCurrent, err := m.RevokeByPublicID(ctx, 1, otherID, current)
	if err != nil {
		t.Fatalf("revoke own other session: %v", err)
	}
	if wasCurrent {
		t.Error("revoking another device reported the current one")
	}
	if _, ok := repo.rows[HashToken(other)]; ok {
		t.Error("revoked session survived")
	}
	if _, ok := repo.rows[HashToken(current)]; !ok {
		t.Error("revoking another device closed the current one")
	}

	// Another member's session is unreachable, not refused.
	strangerID := repo.rows[HashToken(stranger)].PublicID
	if _, err := m.RevokeByPublicID(ctx, 1, strangerID, current); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("revoking another member's session = %v, want ErrNotFound", err)
	}
	if _, ok := repo.rows[HashToken(stranger)]; !ok {
		t.Fatal("revoked another member's session")
	}

	if _, err := m.RevokeByPublicID(ctx, 1, otherID, current); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("revoking a gone session = %v, want ErrNotFound", err)
	}

	// wasCurrent tells the caller to clear the cookie.
	wasCurrent, err = m.RevokeByPublicID(ctx, 1, currentID, current)
	if err != nil {
		t.Fatalf("revoke current session: %v", err)
	}
	if !wasCurrent {
		t.Error("revoking the current session did not report it as current")
	}
}
