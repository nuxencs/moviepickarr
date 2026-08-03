package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"moviepickarr/internal/auth"

	"github.com/gofiber/fiber/v2"
)

// adminSession seeds an admin member with a local login and returns its id and a
// live session cookie, the actor every invite issuance test needs.
func (e *authTestEnv) adminSession(t *testing.T) (int, string) {
	t.Helper()
	id := e.seedMember(t, "Admin", "admin")
	e.seedLocalLogin(t, id, "admin", "correct horse battery")
	return id, e.login(t, "admin", "correct horse battery")
}

// createMember issues POST /members as the admin and returns the new member id
// and the claim token parsed out of the returned claim URL.
func (e *authTestEnv) createMember(t *testing.T, adminCookie, name string) (int, string) {
	t.Helper()
	resp := e.request(t, http.MethodPost, "/api/v1/members", adminCookie, map[string]string{"name": name})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("create member status = %d, want 201", resp.StatusCode)
	}
	var body createMemberResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode create member: %v", err)
	}
	return body.ID, tokenFromClaimURL(t, body.ClaimURL)
}

// tokenFromClaimURL pulls the raw token out of a "/claim/<token>" URL.
func tokenFromClaimURL(t *testing.T, url string) string {
	t.Helper()
	const prefix = "/claim/"
	if !strings.HasPrefix(url, prefix) {
		t.Fatalf("claim URL %q missing %q prefix", url, prefix)
	}
	token := strings.TrimPrefix(url, prefix)
	if token == "" {
		t.Fatal("claim URL carried an empty token")
	}
	return token
}

func decodeClaim(t *testing.T, resp *http.Response) claimResponse {
	t.Helper()
	var cr claimResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	return cr
}

// TestCreateMember_IssuesClaimAndPlaceholderClaimSetsUp walks the whole primary
// path: admin creates a placeholder + gets a claim URL, the claim validates as a
// placeholder, the password claim sets username+password and mints a session,
// and the invite then reads as already-used.
func TestCreateMember_IssuesClaimAndPlaceholderClaimSetsUp(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)

	memberID, token := e.createMember(t, adminCookie, "Newbie")

	// Claim validates as a fresh placeholder.
	valid := e.request(t, http.MethodGet, "/api/v1/auth/claim/"+token, "", nil)
	if valid.StatusCode != fiber.StatusOK {
		t.Fatalf("claim validate status = %d, want 200", valid.StatusCode)
	}
	cc := decodeClaim(t, valid)
	if cc.DisplayName != "Newbie" || cc.Mode != "placeholder" || !cc.Options.Password {
		t.Fatalf("claim = %+v, want Newbie/placeholder/password", cc)
	}

	// Password claim sets the first credential and mints a session (204 + cookie).
	claim := e.request(t, http.MethodPost, "/api/v1/auth/claim/"+token+"/password", "",
		map[string]string{"username": "newbie", "password": "a good long password"})
	if claim.StatusCode != fiber.StatusNoContent {
		t.Fatalf("password claim status = %d, want 204", claim.StatusCode)
	}
	cookie := sessionCookieValue(claim)
	if cookie == "" {
		t.Fatal("password claim set no session cookie")
	}

	// The minted session hydrates to the now-credentialed member.
	me := e.request(t, http.MethodGet, "/api/v1/auth/me", cookie, nil)
	if me.StatusCode != fiber.StatusOK {
		t.Fatalf("me status = %d, want 200", me.StatusCode)
	}
	var identity meResponse
	if err := json.NewDecoder(me.Body).Decode(&identity); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if identity.ID != memberID || identity.Username == nil || *identity.Username != "newbie" || !identity.HasLocalLogin {
		t.Fatalf("me = %+v, want id=%d username=newbie hasLocalLogin=true", identity, memberID)
	}

	// The consumed invite now reads as the distinct already-set-up state.
	used := e.request(t, http.MethodGet, "/api/v1/auth/claim/"+token, "", nil)
	if used.StatusCode != fiber.StatusGone {
		t.Fatalf("used claim status = %d, want 410", used.StatusCode)
	}
	if code := problemCode(t, used); code != "invite_used" {
		t.Fatalf("used claim code = %q, want invite_used", code)
	}
}

// TestClaim_UsedInviteCannotBeRedeemedTwice proves single-use: a second password
// claim on a consumed invite is refused with the already-used state.
func TestClaim_UsedInviteCannotBeRedeemedTwice(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	_, token := e.createMember(t, adminCookie, "Once")

	first := e.request(t, http.MethodPost, "/api/v1/auth/claim/"+token+"/password", "",
		map[string]string{"username": "once", "password": "a good long password"})
	if first.StatusCode != fiber.StatusNoContent {
		t.Fatalf("first claim status = %d, want 204", first.StatusCode)
	}

	second := e.request(t, http.MethodPost, "/api/v1/auth/claim/"+token+"/password", "",
		map[string]string{"username": "again", "password": "another good password"})
	if second.StatusCode != fiber.StatusGone {
		t.Fatalf("second claim status = %d, want 410", second.StatusCode)
	}
}

// TestReissueInvite_RevokesTheOldLink proves regenerate: the old claim URL dies
// (no-longer-valid) and the fresh one works, enforcing one-valid-invite-per-member.
func TestReissueInvite_RevokesTheOldLink(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)

	memberID, oldToken := e.createMember(t, adminCookie, "Regen")

	resp := e.request(t, http.MethodPost, "/api/v1/members/"+strconv.Itoa(memberID)+"/invite", adminCookie, nil)
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("reissue status = %d, want 201", resp.StatusCode)
	}
	var reissued inviteResponse
	if err := json.NewDecoder(resp.Body).Decode(&reissued); err != nil {
		t.Fatalf("decode reissue: %v", err)
	}
	newToken := tokenFromClaimURL(t, reissued.ClaimURL)
	if newToken == oldToken {
		t.Fatal("reissue returned the same token")
	}

	old := e.request(t, http.MethodGet, "/api/v1/auth/claim/"+oldToken, "", nil)
	if old.StatusCode != fiber.StatusNotFound || problemCode(t, old) != "invite_invalid" {
		t.Fatalf("old claim status = %d code = %q, want 404 invite_invalid", old.StatusCode, problemCode(t, old))
	}
	fresh := e.request(t, http.MethodGet, "/api/v1/auth/claim/"+newToken, "", nil)
	if fresh.StatusCode != fiber.StatusOK {
		t.Fatalf("fresh claim status = %d, want 200", fresh.StatusCode)
	}
}

// TestRevokeInvite_InvalidatesThenIsNotFound proves revoke: the link dies, and a
// second revoke with nothing valid is an honest 404.
func TestRevokeInvite_InvalidatesThenIsNotFound(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	memberID, token := e.createMember(t, adminCookie, "Cancel")

	revoke := e.request(t, http.MethodDelete, "/api/v1/members/"+strconv.Itoa(memberID)+"/invite", adminCookie, nil)
	if revoke.StatusCode != fiber.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204", revoke.StatusCode)
	}

	dead := e.request(t, http.MethodGet, "/api/v1/auth/claim/"+token, "", nil)
	if dead.StatusCode != fiber.StatusNotFound || problemCode(t, dead) != "invite_invalid" {
		t.Fatalf("revoked claim status = %d code = %q, want 404 invite_invalid", dead.StatusCode, problemCode(t, dead))
	}

	again := e.request(t, http.MethodDelete, "/api/v1/members/"+strconv.Itoa(memberID)+"/invite", adminCookie, nil)
	if again.StatusCode != fiber.StatusNotFound {
		t.Fatalf("revoke-again status = %d, want 404", again.StatusCode)
	}
}

// TestClaimReset_RevokesExistingSessions proves the reset branch: a credentialed
// member's invite validates as a reset, the password-only claim mints a fresh
// session, and every prior session is revoked.
func TestClaimReset_RevokesExistingSessions(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)

	bobID := e.seedMember(t, "Bob", "member")
	e.seedLocalLogin(t, bobID, "bob", "the old password")
	oldCookie := e.login(t, "bob", "the old password")

	// Admin issues an invite for the already-credentialed Bob → a reset.
	issue := e.request(t, http.MethodPost, "/api/v1/members/"+strconv.Itoa(bobID)+"/invite", adminCookie, nil)
	if issue.StatusCode != fiber.StatusCreated {
		t.Fatalf("issue status = %d, want 201", issue.StatusCode)
	}
	var inv inviteResponse
	if err := json.NewDecoder(issue.Body).Decode(&inv); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	token := tokenFromClaimURL(t, inv.ClaimURL)

	valid := e.request(t, http.MethodGet, "/api/v1/auth/claim/"+token, "", nil)
	if cc := decodeClaim(t, valid); cc.Mode != "reset" || cc.DisplayName != "Bob" {
		t.Fatalf("claim = %+v, want Bob/reset", cc)
	}

	// Password-only reset: no username in the body.
	reset := e.request(t, http.MethodPost, "/api/v1/auth/claim/"+token+"/password", "",
		map[string]string{"password": "a brand new password"})
	if reset.StatusCode != fiber.StatusNoContent {
		t.Fatalf("reset claim status = %d, want 204", reset.StatusCode)
	}
	newCookie := sessionCookieValue(reset)
	if newCookie == "" {
		t.Fatal("reset claim set no session cookie")
	}

	// The old session is gone; the minted one works.
	if old := e.request(t, http.MethodGet, "/api/v1/auth/me", oldCookie, nil); old.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("old session after reset = %d, want 401", old.StatusCode)
	}
	if fresh := e.request(t, http.MethodGet, "/api/v1/auth/me", newCookie, nil); fresh.StatusCode != fiber.StatusOK {
		t.Fatalf("new session after reset = %d, want 200", fresh.StatusCode)
	}

	// The old password no longer logs in; the new one does.
	badLogin := e.request(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"username": "bob", "password": "the old password"})
	if badLogin.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("old password login = %d, want 401", badLogin.StatusCode)
	}
	_ = e.login(t, "bob", "a brand new password")
}

// TestClaim_ExpiredCollapsesToInvalid proves the time-derived validity: past the
// TTL an unused, unrevoked invite still reads as no-longer-valid.
func TestClaim_ExpiredCollapsesToInvalid(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	_, token := e.createMember(t, adminCookie, "Slowpoke")

	e.clk.t = e.clk.t.Add(auth.InviteTTL + 1)

	resp := e.request(t, http.MethodGet, "/api/v1/auth/claim/"+token, "", nil)
	if resp.StatusCode != fiber.StatusNotFound || problemCode(t, resp) != "invite_invalid" {
		t.Fatalf("expired claim status = %d code = %q, want 404 invite_invalid", resp.StatusCode, problemCode(t, resp))
	}
}

func TestClaim_UnknownTokenIsInvalid(t *testing.T) {
	e := setupAuthApp(t)
	resp := e.request(t, http.MethodGet, "/api/v1/auth/claim/does-not-exist", "", nil)
	if resp.StatusCode != fiber.StatusNotFound || problemCode(t, resp) != "invite_invalid" {
		t.Fatalf("unknown claim status = %d code = %q, want 404 invite_invalid", resp.StatusCode, problemCode(t, resp))
	}
}

// TestClaimPassword_RejectsBadInput checks the shared validation is enforced at
// the claim edge: a placeholder needs a username, and the password bound holds.
func TestClaimPassword_RejectsBadInput(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)

	_, tokenA := e.createMember(t, adminCookie, "NoUser")
	noUser := e.request(t, http.MethodPost, "/api/v1/auth/claim/"+tokenA+"/password", "",
		map[string]string{"password": "a good long password"})
	if noUser.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("placeholder without username = %d, want 400", noUser.StatusCode)
	}

	_, tokenB := e.createMember(t, adminCookie, "ShortPw")
	shortPw := e.request(t, http.MethodPost, "/api/v1/auth/claim/"+tokenB+"/password", "",
		map[string]string{"username": "shorty", "password": "short"})
	if shortPw.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("short password = %d, want 400", shortPw.StatusCode)
	}

	// A rejected claim leaves the invite usable.
	stillValid := e.request(t, http.MethodGet, "/api/v1/auth/claim/"+tokenB, "", nil)
	if stillValid.StatusCode != fiber.StatusOK {
		t.Fatalf("claim after rejected attempt = %d, want 200 (still usable)", stillValid.StatusCode)
	}
}

// TestInviteRoutes_RequireAdmin proves the issuance surface is admin-gated.
func TestInviteRoutes_RequireAdmin(t *testing.T) {
	e := setupAuthApp(t)
	memberID := e.seedMember(t, "Plain", "member")
	e.seedLocalLogin(t, memberID, "plain", "the right password")
	memberCookie := e.login(t, "plain", "the right password")

	target := e.seedMember(t, "Target", "member")

	reissue := e.request(t, http.MethodPost, "/api/v1/members/"+strconv.Itoa(target)+"/invite", memberCookie, nil)
	if reissue.StatusCode != fiber.StatusForbidden || problemCode(t, reissue) != "admin_required" {
		t.Fatalf("member reissue = %d code = %q, want 403 admin_required", reissue.StatusCode, problemCode(t, reissue))
	}
	revoke := e.request(t, http.MethodDelete, "/api/v1/members/"+strconv.Itoa(target)+"/invite", memberCookie, nil)
	if revoke.StatusCode != fiber.StatusForbidden || problemCode(t, revoke) != "admin_required" {
		t.Fatalf("member revoke = %d code = %q, want 403 admin_required", revoke.StatusCode, problemCode(t, revoke))
	}
	create := e.request(t, http.MethodPost, "/api/v1/members", memberCookie, map[string]string{"name": "Nope"})
	if create.StatusCode != fiber.StatusForbidden || problemCode(t, create) != "admin_required" {
		t.Fatalf("member create = %d code = %q, want 403 admin_required", create.StatusCode, problemCode(t, create))
	}
}

func TestReissueInvite_MissingMemberIsNotFound(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	resp := e.request(t, http.MethodPost, "/api/v1/members/99999/invite", adminCookie, nil)
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("reissue missing member = %d, want 404", resp.StatusCode)
	}
}

func TestArchivedMemberInviteIsInvalidAndCannotBeReissued(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	memberID, token := e.createMember(t, adminCookie, "Archived")
	if _, err := e.pool.Write.ExecContext(context.Background(),
		"UPDATE users SET archived_at = unixepoch() WHERE id = ?", memberID); err != nil {
		t.Fatalf("archive member without cleanup: %v", err)
	}

	validate := e.request(t, http.MethodGet, "/api/v1/auth/claim/"+token, "", nil)
	if validate.StatusCode != fiber.StatusNotFound {
		t.Fatalf("archived claim validation = %d, want 404", validate.StatusCode)
	}

	reissue := e.request(t, http.MethodPost, "/api/v1/members/"+strconv.Itoa(memberID)+"/invite", adminCookie, nil)
	if reissue.StatusCode != fiber.StatusNotFound {
		t.Fatalf("archived invite reissue = %d, want 404", reissue.StatusCode)
	}
}

// TestSelfServeLocalLogin_SetsFirstCredential proves the completeness path: an
// authed member with no local login sets one, and a second attempt is a 409.
func TestSelfServeLocalLogin_SetsFirstCredential(t *testing.T) {
	e := setupAuthApp(t)

	// A member with a session but no local login (the OIDC-first shape): mint a
	// session straight from the manager, no credential involved.
	memberID := e.seedMember(t, "Ess Es Oh", "member")
	rawToken, _, err := e.h.sessions.Mint(context.Background(), memberID, nil, nil)
	if err != nil {
		t.Fatalf("mint session: %v", err)
	}

	set := e.request(t, http.MethodPost, "/api/v1/auth/local-login", rawToken,
		map[string]string{"username": "sso_user", "password": "a good long password"})
	if set.StatusCode != fiber.StatusNoContent {
		t.Fatalf("self-serve local-login = %d, want 204", set.StatusCode)
	}

	me := e.request(t, http.MethodGet, "/api/v1/auth/me", rawToken, nil)
	var identity meResponse
	if err := json.NewDecoder(me.Body).Decode(&identity); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if identity.Username == nil || *identity.Username != "sso_user" || !identity.HasLocalLogin {
		t.Fatalf("me = %+v, want username=sso_user hasLocalLogin=true", identity)
	}

	// The credential now exists: a second attempt is a conflict, not a change.
	dup := e.request(t, http.MethodPost, "/api/v1/auth/local-login", rawToken,
		map[string]string{"username": "other", "password": "another good password"})
	if dup.StatusCode != fiber.StatusConflict {
		t.Fatalf("second self-serve local-login = %d, want 409", dup.StatusCode)
	}
}

func TestSelfServeLocalLogin_RequiresSession(t *testing.T) {
	e := setupAuthApp(t)
	resp := e.request(t, http.MethodPost, "/api/v1/auth/local-login", "",
		map[string]string{"username": "nobody", "password": "a good long password"})
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("unauth self-serve local-login = %d, want 401", resp.StatusCode)
	}
}

// invitesOverview reads GET /invites as the given actor, asserting 200.
func (e *authTestEnv) invitesOverview(t *testing.T, cookie string) []inviteOverviewResponse {
	t.Helper()
	resp := e.request(t, http.MethodGet, "/api/v1/invites", cookie, nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("invites overview status = %d, want 200", resp.StatusCode)
	}
	var rows []inviteOverviewResponse
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode invites overview: %v", err)
	}
	return rows
}

// TestInvitesOverview_OneRowPerMemberSplitOpenFromExpired is the surface's whole
// contract in one walk: a member re-invited after their first link lapsed
// appears once (the newest invite, open), a member never re-invited appears as
// expired, and open leads expired. Both rows name the issuing admin.
func TestInvitesOverview_OneRowPerMemberSplitOpenFromExpired(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)

	benID, _ := e.createMember(t, adminCookie, "Ben")
	cleoID, _ := e.createMember(t, adminCookie, "Cleo")

	// Past the 7-day TTL: both first links have lapsed, and neither was revoked
	// (Issue only revokes valid invites), so both rows are still on the table.
	e.clk.t = e.clk.t.Add(auth.InviteTTL + time.Hour)

	// Cleo is re-invited; Ben is left waiting on a dead link.
	reissue := e.request(t, http.MethodPost, "/api/v1/members/"+strconv.Itoa(cleoID)+"/invite", adminCookie, nil)
	if reissue.StatusCode != fiber.StatusCreated {
		t.Fatalf("reissue status = %d, want 201", reissue.StatusCode)
	}

	rows := e.invitesOverview(t, adminCookie)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want 2 (one per member)", rows)
	}
	if rows[0].MemberID != cleoID || rows[0].Status != auth.InviteOpen {
		t.Fatalf("first row = %+v, want Cleo open", rows[0])
	}
	if rows[1].MemberID != benID || rows[1].Status != auth.InviteExpired {
		t.Fatalf("second row = %+v, want Ben expired", rows[1])
	}
	if rows[0].MemberName != "Cleo" || rows[1].MemberName != "Ben" {
		t.Fatalf("names = %q/%q, want Cleo/Ben", rows[0].MemberName, rows[1].MemberName)
	}
	for _, row := range rows {
		if row.IssuedBy != "Admin" {
			t.Fatalf("row %+v issuedBy = %q, want Admin", row, row.IssuedBy)
		}
		if row.ExpiresAt == "" || row.IssuedAt == "" {
			t.Fatalf("row %+v is missing a timestamp", row)
		}
	}
}

// TestInvitesOverview_DropsMembersWhoCanLogIn is the self-clearing rule at the
// HTTP seam: claiming the invite is what a member does, and the row goes with
// it. No admin action is involved.
func TestInvitesOverview_DropsMembersWhoCanLogIn(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	_, token := e.createMember(t, adminCookie, "Ben")

	if rows := e.invitesOverview(t, adminCookie); len(rows) != 1 {
		t.Fatalf("rows before claim = %+v, want 1", rows)
	}

	claim := e.request(t, http.MethodPost, "/api/v1/auth/claim/"+token+"/password", "",
		map[string]string{"username": "ben", "password": "a good long password"})
	if claim.StatusCode != fiber.StatusNoContent {
		t.Fatalf("password claim status = %d, want 204", claim.StatusCode)
	}

	if rows := e.invitesOverview(t, adminCookie); len(rows) != 0 {
		t.Fatalf("rows after claim = %+v, want none", rows)
	}
}

// TestDismissInvite_ClearsTheRowThenIs404 covers Dismiss end to end: it reaches
// a lapsed invite (which the member-scoped revoke cannot), takes it off the
// surface, and refuses a repeat rather than reporting a second success.
func TestDismissInvite_ClearsTheRowThenIs404(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	memberID, _ := e.createMember(t, adminCookie, "Ben")
	e.clk.t = e.clk.t.Add(auth.InviteTTL + time.Hour)

	// The member-scoped revoke has nothing valid left to cancel, which is exactly
	// why the expired row needs its own id.
	stale := e.request(t, http.MethodDelete, "/api/v1/members/"+strconv.Itoa(memberID)+"/invite", adminCookie, nil)
	if stale.StatusCode != fiber.StatusNotFound {
		t.Fatalf("member-scoped revoke of a lapsed invite = %d, want 404", stale.StatusCode)
	}

	rows := e.invitesOverview(t, adminCookie)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want 1", rows)
	}
	path := "/api/v1/invites/" + strconv.FormatInt(rows[0].ID, 10)

	dismiss := e.request(t, http.MethodDelete, path, adminCookie, nil)
	if dismiss.StatusCode != fiber.StatusNoContent {
		t.Fatalf("dismiss status = %d, want 204", dismiss.StatusCode)
	}
	if left := e.invitesOverview(t, adminCookie); len(left) != 0 {
		t.Fatalf("rows after dismiss = %+v, want none", left)
	}

	again := e.request(t, http.MethodDelete, path, adminCookie, nil)
	if again.StatusCode != fiber.StatusNotFound {
		t.Fatalf("second dismiss status = %d, want 404", again.StatusCode)
	}
}

// TestInvitesOverview_AdminOnly: the overview names every member still waiting
// to set up a login and who invited them. Both routes are admin-gated.
func TestInvitesOverview_AdminOnly(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	memberID, _ := e.createMember(t, adminCookie, "Ben")
	e.seedLocalLogin(t, memberID, "ben", "a good long password")
	memberCookie := e.login(t, "ben", "a good long password")

	list := e.request(t, http.MethodGet, "/api/v1/invites", memberCookie, nil)
	if list.StatusCode != fiber.StatusForbidden {
		t.Fatalf("member invites list = %d, want 403", list.StatusCode)
	}
	dismiss := e.request(t, http.MethodDelete, "/api/v1/invites/1", memberCookie, nil)
	if dismiss.StatusCode != fiber.StatusForbidden {
		t.Fatalf("member dismiss = %d, want 403", dismiss.StatusCode)
	}
}
