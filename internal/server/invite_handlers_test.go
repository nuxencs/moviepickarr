package server

import (
	"context"
	"encoding/json/v2"
	"errors"
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
	if err := json.UnmarshalRead(resp.Body, &body); err != nil {
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
	if err := json.UnmarshalRead(resp.Body, &cr); err != nil {
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
	residualCookie, _, err := e.h.sessions.Mint(context.Background(), memberID, nil)
	if err != nil {
		t.Fatalf("mint residual placeholder session: %v", err)
	}

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
	if err := json.UnmarshalRead(me.Body, &identity); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if identity.ID != memberID || identity.Username == nil || *identity.Username != "newbie" || !identity.HasLocalLogin {
		t.Fatalf("me = %+v, want id=%d username=newbie hasLocalLogin=true", identity, memberID)
	}
	if old := e.request(t, http.MethodGet, "/api/v1/auth/me", residualCookie, nil); old.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("residual session after onboarding claim = %d, want 401", old.StatusCode)
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

// TestReplaceInvite_RevokesTheOldLink proves exact replacement: the old claim
// URL dies and the fresh one works.
func TestReplaceInvite_RevokesTheOldLink(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)

	_, oldToken := e.createMember(t, adminCookie, "Regen")
	rows := e.invitesOverview(t, adminCookie).Items
	if len(rows) != 1 {
		t.Fatalf("overview rows = %+v, want one", rows)
	}

	resp := e.request(t, http.MethodPost, "/api/v1/invites/"+rows[0].ID+"/replacement", adminCookie, nil)
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("replacement status = %d, want 201", resp.StatusCode)
	}
	var replacement inviteResponse
	if err := json.UnmarshalRead(resp.Body, &replacement); err != nil {
		t.Fatalf("decode replacement: %v", err)
	}
	newToken := tokenFromClaimURL(t, replacement.ClaimURL)
	if newToken == oldToken {
		t.Fatal("replacement returned the same token")
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

// TestRevokeInvite_InvalidatesThenConflicts proves revoke: the link dies, and a
// second action on the stale generation conflicts.
func TestRevokeInvite_InvalidatesThenConflicts(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	_, token := e.createMember(t, adminCookie, "Cancel")
	rows := e.invitesOverview(t, adminCookie).Items
	if len(rows) != 1 {
		t.Fatalf("overview rows = %+v, want one", rows)
	}
	path := "/api/v1/invites/" + rows[0].ID

	revoke := e.request(t, http.MethodDelete, path, adminCookie, nil)
	if revoke.StatusCode != fiber.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204", revoke.StatusCode)
	}

	dead := e.request(t, http.MethodGet, "/api/v1/auth/claim/"+token, "", nil)
	if dead.StatusCode != fiber.StatusNotFound || problemCode(t, dead) != "invite_invalid" {
		t.Fatalf("revoked claim status = %d code = %q, want 404 invite_invalid", dead.StatusCode, problemCode(t, dead))
	}

	again := e.request(t, http.MethodDelete, path, adminCookie, nil)
	if again.StatusCode != fiber.StatusConflict {
		t.Fatalf("revoke-again status = %d, want 409", again.StatusCode)
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

	// Admin explicitly issues a password-reset invite for credentialed Bob.
	issue := e.request(t, http.MethodPost, "/api/v1/members/"+strconv.Itoa(bobID)+"/invite", adminCookie,
		map[string]string{"purpose": "password_reset"})
	if issue.StatusCode != fiber.StatusCreated {
		t.Fatalf("issue status = %d, want 201", issue.StatusCode)
	}
	var inv inviteResponse
	if err := json.UnmarshalRead(issue.Body, &inv); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	token := tokenFromClaimURL(t, inv.ClaimURL)
	overview := e.invitesOverview(t, adminCookie)
	if len(overview.Items) != 1 {
		t.Fatalf("overview after reset issue = %+v, want one manageable invite", overview.Items)
	}
	if row := overview.Items[0]; row.MemberID != bobID || row.Status != auth.InviteOpen || row.ID == "" {
		t.Fatalf("reset overview row = %+v, want Bob/open/public handle", row)
	}

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
	if rows := e.invitesOverview(t, adminCookie).Items; len(rows) != 0 {
		t.Fatalf("overview after reset claim = %+v, want empty", rows)
	}
}

func TestCreateInvite_DefaultPurposeRejectsCredentialedMember(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	bobID := e.seedMember(t, "Bob", "member")
	e.seedLocalLogin(t, bobID, "bob", "a good long password")

	issue := e.request(
		t,
		http.MethodPost,
		"/api/v1/members/"+strconv.Itoa(bobID)+"/invite",
		adminCookie,
		nil,
	)
	code := problemCode(t, issue)
	if issue.StatusCode != fiber.StatusConflict || code != "conflict" {
		t.Fatalf(
			"default invite for credentialed member = %d code %q, want 409 conflict",
			issue.StatusCode,
			code,
		)
	}
	if rows := e.invitesOverview(t, adminCookie).Items; len(rows) != 0 {
		t.Fatalf("overview after rejected onboarding invite = %+v, want empty", rows)
	}
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

	createInvite := e.request(t, http.MethodPost, "/api/v1/members/"+strconv.Itoa(target)+"/invite", memberCookie, nil)
	if createInvite.StatusCode != fiber.StatusForbidden || problemCode(t, createInvite) != "admin_required" {
		t.Fatalf("member invite create = %d code = %q, want 403 admin_required", createInvite.StatusCode, problemCode(t, createInvite))
	}
	handle := "public-invite-handle-000000"
	revoke := e.request(t, http.MethodDelete, "/api/v1/invites/"+handle, memberCookie, nil)
	if revoke.StatusCode != fiber.StatusForbidden || problemCode(t, revoke) != "admin_required" {
		t.Fatalf("member revoke = %d code = %q, want 403 admin_required", revoke.StatusCode, problemCode(t, revoke))
	}
	replace := e.request(t, http.MethodPost, "/api/v1/invites/"+handle+"/replacement", memberCookie, nil)
	if replace.StatusCode != fiber.StatusForbidden || problemCode(t, replace) != "admin_required" {
		t.Fatalf("member replace = %d code = %q, want 403 admin_required", replace.StatusCode, problemCode(t, replace))
	}
	dismiss := e.request(t, http.MethodPost, "/api/v1/invites/"+handle+"/dismiss", memberCookie, nil)
	if dismiss.StatusCode != fiber.StatusForbidden || problemCode(t, dismiss) != "admin_required" {
		t.Fatalf("member dismiss = %d code = %q, want 403 admin_required", dismiss.StatusCode, problemCode(t, dismiss))
	}
	create := e.request(t, http.MethodPost, "/api/v1/members", memberCookie, map[string]string{"name": "Nope"})
	if create.StatusCode != fiber.StatusForbidden || problemCode(t, create) != "admin_required" {
		t.Fatalf("member create = %d code = %q, want 403 admin_required", create.StatusCode, problemCode(t, create))
	}
}

func TestCreateInvite_MissingMemberIsNotFound(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	resp := e.request(t, http.MethodPost, "/api/v1/members/99999/invite", adminCookie, nil)
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("create invite for missing member = %d, want 404", resp.StatusCode)
	}
}

func TestArchivedMemberInviteIsInvalidAndCannotBeCreated(t *testing.T) {
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

	issue := e.request(t, http.MethodPost, "/api/v1/members/"+strconv.Itoa(memberID)+"/invite", adminCookie, nil)
	if issue.StatusCode != fiber.StatusNotFound {
		t.Fatalf("archived invite create = %d, want 404", issue.StatusCode)
	}
}

// TestSelfServeLocalLogin_SetsFirstCredential proves the completeness path: an
// authed member with no local login sets one, and a second attempt is a 409.
func TestSelfServeLocalLogin_SetsFirstCredential(t *testing.T) {
	e := setupAuthApp(t)

	// A member with a session but no local login (the OIDC-first shape): mint a
	// session straight from the manager, no credential involved.
	memberID := e.seedMember(t, "Ess Es Oh", "member")
	rawToken, _, err := e.h.sessions.Mint(context.Background(), memberID, nil)
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
	if err := json.UnmarshalRead(me.Body, &identity); err != nil {
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

func TestAdminCredentialCreationRetiresCurrentInvite(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	memberID, token := e.createMember(t, adminCookie, "Ben")

	set := e.request(
		t,
		http.MethodPut,
		"/api/v1/members/"+strconv.Itoa(memberID)+"/local-login",
		adminCookie,
		map[string]string{"username": "ben", "password": "a good long password"},
	)
	if set.StatusCode != fiber.StatusNoContent {
		t.Fatalf("set local login = %d, want 204", set.StatusCode)
	}
	validate := e.request(t, http.MethodGet, "/api/v1/auth/claim/"+token, "", nil)
	if validate.StatusCode != fiber.StatusNotFound {
		t.Fatalf("old invite after credential creation = %d, want 404", validate.StatusCode)
	}
	if rows := e.invitesOverview(t, adminCookie).Items; len(rows) != 0 {
		t.Fatalf("overview after credential creation = %+v, want empty", rows)
	}
}

// invitesOverview reads GET /invites as the given actor, asserting 200.
func (e *authTestEnv) invitesOverview(t *testing.T, cookie string) invitesOverviewResponse {
	t.Helper()
	resp := e.request(t, http.MethodGet, "/api/v1/invites", cookie, nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("invites overview status = %d, want 200", resp.StatusCode)
	}
	var overview invitesOverviewResponse
	if err := json.UnmarshalRead(resp.Body, &overview); err != nil {
		t.Fatalf("decode invites overview: %v", err)
	}
	return overview
}

// TestInvitesOverview_OneRowPerMemberSplitOpenFromExpired is the surface's whole
// contract in one walk: an expired generation replaced by its exact handle
// appears once as open, an untouched generation remains expired, and open leads
// expired. Both rows name the issuing admin.
func TestInvitesOverview_OneRowPerMemberSplitOpenFromExpired(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)

	benID, _ := e.createMember(t, adminCookie, "Ben")
	cleoID, _ := e.createMember(t, adminCookie, "Cleo")

	// Past the 7-day TTL: both first links have lapsed.
	e.clk.t = e.clk.t.Add(auth.InviteTTL + time.Hour)

	before := e.invitesOverview(t, adminCookie)
	var cleoInvite string
	for _, row := range before.Items {
		if row.MemberID == cleoID {
			cleoInvite = row.ID
		}
	}
	if cleoInvite == "" {
		t.Fatal("Cleo's expired invite was missing")
	}
	// Replace Cleo's exact expired generation; Ben stays on the dead link.
	replacement := e.request(t, http.MethodPost, "/api/v1/invites/"+cleoInvite+"/replacement", adminCookie, nil)
	if replacement.StatusCode != fiber.StatusCreated {
		t.Fatalf("replacement status = %d, want 201", replacement.StatusCode)
	}

	overview := e.invitesOverview(t, adminCookie)
	rows := overview.Items
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want 2 (one per member)", rows)
	}
	if overview.ServerNow != formatTime(&e.clk.t) {
		t.Fatalf("serverNow = %q, want %q", overview.ServerNow, formatTime(&e.clk.t))
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

func TestInvitesOverview_SubsecondClockMatchesWireClassification(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	_, _ = e.createMember(t, adminCookie, "Ben")
	expiresAt := e.clk.t.Add(auth.InviteTTL)

	tests := []struct {
		name       string
		now        time.Time
		wantStatus string
	}{
		{name: "just before expiry", now: expiresAt.Add(-100 * time.Millisecond), wantStatus: auth.InviteOpen},
		{name: "just after expiry", now: expiresAt.Add(100 * time.Millisecond), wantStatus: auth.InviteExpired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e.clk.t = tt.now
			overview := e.invitesOverview(t, adminCookie)
			if len(overview.Items) != 1 {
				t.Fatalf("items = %+v, want one", overview.Items)
			}
			row := overview.Items[0]
			if row.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", row.Status, tt.wantStatus)
			}

			wireNow, err := time.Parse(time.RFC3339, overview.ServerNow)
			if err != nil {
				t.Fatalf("parse serverNow %q: %v", overview.ServerNow, err)
			}
			wireExpiry, err := time.Parse(time.RFC3339, row.ExpiresAt)
			if err != nil {
				t.Fatalf("parse expiresAt %q: %v", row.ExpiresAt, err)
			}
			wireStatus := auth.InviteExpired
			if wireNow.Before(wireExpiry) {
				wireStatus = auth.InviteOpen
			}
			if row.Status != wireStatus {
				t.Fatalf(
					"status %q disagrees with serialized serverNow/expiresAt (%s/%s => %q)",
					row.Status,
					overview.ServerNow,
					row.ExpiresAt,
					wireStatus,
				)
			}
			if overview.ServerNow != formatTime(&tt.now) {
				t.Fatalf("serverNow = %q, want whole-second %q", overview.ServerNow, formatTime(&tt.now))
			}
		})
	}
}

// TestInvitesOverview_DropsMembersWhoCanLogIn is the self-clearing rule at the
// HTTP seam: claiming the invite is what a member does, and the row goes with
// it. No admin action is involved.
func TestInvitesOverview_DropsMembersWhoCanLogIn(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	_, token := e.createMember(t, adminCookie, "Ben")

	if rows := e.invitesOverview(t, adminCookie).Items; len(rows) != 1 {
		t.Fatalf("rows before claim = %+v, want 1", rows)
	}

	claim := e.request(t, http.MethodPost, "/api/v1/auth/claim/"+token+"/password", "",
		map[string]string{"username": "ben", "password": "a good long password"})
	if claim.StatusCode != fiber.StatusNoContent {
		t.Fatalf("password claim status = %d, want 204", claim.StatusCode)
	}

	if rows := e.invitesOverview(t, adminCookie).Items; len(rows) != 0 {
		t.Fatalf("rows after claim = %+v, want none", rows)
	}
}

// TestDismissInvite_ClearsTheRowThenConflicts covers Dismiss end to end: it
// retires an exact expired generation and refuses a repeat rather than
// reporting a second success.
func TestDismissInvite_ClearsTheRowThenConflicts(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	_, _ = e.createMember(t, adminCookie, "Ben")
	e.clk.t = e.clk.t.Add(auth.InviteTTL + time.Hour)

	rows := e.invitesOverview(t, adminCookie).Items
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want 1", rows)
	}
	path := "/api/v1/invites/" + rows[0].ID + "/dismiss"

	dismiss := e.request(t, http.MethodPost, path, adminCookie, nil)
	if dismiss.StatusCode != fiber.StatusNoContent {
		t.Fatalf("dismiss status = %d, want 204", dismiss.StatusCode)
	}
	if left := e.invitesOverview(t, adminCookie).Items; len(left) != 0 {
		t.Fatalf("rows after dismiss = %+v, want none", left)
	}

	again := e.request(t, http.MethodPost, path, adminCookie, nil)
	if again.StatusCode != fiber.StatusConflict {
		t.Fatalf("second dismiss status = %d, want 409", again.StatusCode)
	}
}

func TestInviteActions_UseExactGenerationAndState(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	_, _ = e.createMember(t, adminCookie, "Ben")

	rows := e.invitesOverview(t, adminCookie).Items
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one open invite", rows)
	}
	oldID := rows[0].ID

	dismissOpen := e.request(t, http.MethodPost, "/api/v1/invites/"+oldID+"/dismiss", adminCookie, nil)
	if dismissOpen.StatusCode != fiber.StatusConflict {
		t.Fatalf("dismiss open = %d, want 409", dismissOpen.StatusCode)
	}
	replace := e.request(t, http.MethodPost, "/api/v1/invites/"+oldID+"/replacement", adminCookie, nil)
	if replace.StatusCode != fiber.StatusCreated {
		t.Fatalf("replace = %d, want 201", replace.StatusCode)
	}
	staleRevoke := e.request(t, http.MethodDelete, "/api/v1/invites/"+oldID, adminCookie, nil)
	if staleRevoke.StatusCode != fiber.StatusConflict {
		t.Fatalf("stale revoke = %d, want 409", staleRevoke.StatusCode)
	}

	current := e.invitesOverview(t, adminCookie).Items
	if len(current) != 1 || current[0].ID == oldID {
		t.Fatalf("current rows = %+v, want a different generation", current)
	}
	revoke := e.request(t, http.MethodDelete, "/api/v1/invites/"+current[0].ID, adminCookie, nil)
	if revoke.StatusCode != fiber.StatusNoContent {
		t.Fatalf("current revoke = %d, want 204", revoke.StatusCode)
	}

	numeric := e.request(t, http.MethodDelete, "/api/v1/invites/1", adminCookie, nil)
	if numeric.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("numeric invite id = %d, want 400", numeric.StatusCode)
	}
}

func TestClaimPassword_ConcurrentRedemptionHasOneOwner(t *testing.T) {
	e := setupAuthApp(t)
	_, adminCookie := e.adminSession(t)
	_, token := e.createMember(t, adminCookie, "Ben")

	type result struct {
		claim auth.ClaimResult
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for range 2 {
		go func() {
			<-start
			claim, err := e.h.invites.ClaimPassword(
				context.Background(), token, "ben", "a good long password",
			)
			results <- result{claim: claim, err: err}
		}()
	}
	close(start)

	successes, used := 0, 0
	for range 2 {
		res := <-results
		switch {
		case res.err == nil:
			successes++
		case errors.Is(res.err, auth.ErrInviteUsed):
			used++
		default:
			t.Fatalf("claim error = %v, want nil or ErrInviteUsed", res.err)
		}
	}
	if successes != 1 || used != 1 {
		t.Fatalf("successes/used = %d/%d, want 1/1", successes, used)
	}
}

// TestInvitesOverview_AdminOnly proves invite status and actions are admin-only.
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
	dismiss := e.request(t, http.MethodPost, "/api/v1/invites/public-invite-handle-000000/dismiss", memberCookie, nil)
	if dismiss.StatusCode != fiber.StatusForbidden {
		t.Fatalf("member dismiss = %d, want 403", dismiss.StatusCode)
	}
}
