# Research: OIDC relying-party library for moviepickarr

Resolves [#73](https://github.com/nuxencs/moviepickarr/issues/73). Part of #71, blocks #77.

Question: which Go library best supports moviepickarr as an OIDC relying party (RP)
against a single generic, discovery-based provider (Authelia first)? Candidates:
`coreos/go-oidc` + `golang.org/x/oauth2`, `zitadel/oidc`, or hand-rolling on
`x/oauth2` alone.

Constraints that shape the answer:

- Single provider, configured by issuer URL. Discovery-based, nothing
  Authelia-specific.
- After the callback the app mints its own server-side session (opaque HttpOnly
  cookie). Provider tokens are not used after login.
- The app runs on Fiber v2 (fasthttp), not net/http.

## Recommendation

Use `github.com/coreos/go-oidc/v3` + `golang.org/x/oauth2`.

It covers exactly the hard parts (discovery, ID-token signature/iss/aud/exp
verification, JWKS caching + rotation) with the smallest dependency and the
broadest adoption in the Go ecosystem, and its net/http coupling is limited to
outbound calls, which cost nothing behind Fiber. The parts it leaves to the app
(state, nonce comparison, PKCE wiring via x/oauth2, cookie plumbing) are small,
well-specified, and we would want to own them anyway because our callback ends
in a self-minted session, not in the library's cookie model. zitadel/oidc's RP
helpers automate more, but the automation lives in `http.Handler` wrappers and a
net/http `CookieHandler` that fight Fiber; stripped down to its non-handler
primitives it converges on the same surface as go-oidc while pulling in a much
larger dependency tree. Hand-rolling gains nothing: the only piece x/oauth2
lacks is exactly the piece easiest to get wrong (JWT verification + JWKS
rotation).

## What the spec requires of an RP

Authorization Code Flow, per OpenID Connect Core 1.0
([spec](https://openid.net/specs/openid-connect-core-1_0.html)):

1. Build the Authentication Request (§3.1.2.1): `response_type=code`,
   `scope=openid` (plus `profile email` as needed), `client_id`,
   `redirect_uri`, `state` (RECOMMENDED, CSRF binding to the user agent),
   `nonce` (binds the ID token to the login attempt; MUST be unguessable).
2. Redirect the user to the provider's authorization endpoint; receive
   `code` + `state` on the callback; verify `state` matches what we stored.
3. Token request (§3.1.3.1): POST `grant_type=authorization_code` with the
   code and `redirect_uri`, authenticating as a confidential client
   (`client_secret_basic`/`client_secret_post`).
4. Validate the ID token (§3.1.3.7). The load-bearing checks for a
   single-provider code-flow RP:
   - `iss` exactly matches the discovery issuer (check 2)
   - `aud` contains our `client_id`; `azp` equals it when present (checks 3-5)
   - JWS signature against the JWKS from discovery `jwks_uri` (check 6),
     `alg` = RS256 or what we registered (check 7)
   - `exp` in the future (check 9), `iat` freshness policy (check 10)
   - `nonce` claim equals the one we sent (check 11)
   - `acr`/`auth_time` only if we request step-up or `max_age` (checks 12-13);
     we don't.
5. Use `sub` (plus profile claims) as the stable user identity; mint our own
   session.

PKCE: RFC 9700 (OAuth 2.0 Security BCP, §2.1.1) says public clients MUST use
PKCE and "for confidential clients, the use of PKCE is RECOMMENDED, as it
provides strong protection against misuse and injection of authorization
codes", with `code_challenge_method=S256`. We are a confidential client, so
PKCE is defense-in-depth on top of the client secret, not a replacement for
`state` or `nonce`. Authelia supports S256 PKCE with per-client enforcement
(`enforce_pkce`), exposes standard discovery at
`/.well-known/openid-configuration`, and is OpenID Certified
([Authelia OIDC docs](https://www.authelia.com/configuration/identity-providers/openid-connect/provider/)).

### Refresh tokens: not needed

`offline_access` (OIDC Core §11) exists so an RP can call provider resources
(userinfo, APIs) while the user is absent. We never touch provider tokens after
the callback: session lifetime is governed entirely by our own cookie policy.
There is nothing to refresh, so we do not request `offline_access` and do not
handle refresh tokens. If we ever want periodic re-authentication against the
provider, that is a fresh code-flow round trip, still no refresh token.

## Candidate 1: coreos/go-oidc v3 + golang.org/x/oauth2

Sources: [godoc](https://pkg.go.dev/github.com/coreos/go-oidc/v3/oidc),
[repo](https://github.com/coreos/go-oidc) (`oidc.go`, `verify.go`, `jwks.go`),
[x/oauth2 godoc](https://pkg.go.dev/golang.org/x/oauth2).

- Discovery: `oidc.NewProvider(ctx, issuer)` fetches
  `{issuer}/.well-known/openid-configuration` and validates the discovered
  `iss` against the configured issuer (`IssuerMismatchError` on mismatch, the
  well-known trailing-slash footgun). `Provider.Endpoint()` drops straight
  into an `oauth2.Config`; `Provider.Claims(&v)` exposes raw discovery fields
  (e.g. `end_session_endpoint`) if we ever need them.
- ID-token verification: `provider.Verifier(&oidc.Config{ClientID: ...})`
  checks signature (RS/ES/PS 256-512 + EdDSA; defaults to RS256), `iss`,
  `aud`, `exp` (strict, no leeway) and `nbf` (5-minute leeway). It does NOT
  check `nonce` ("Verify does NOT do nonce validation, which is the caller's
  responsibility" per the source) and does not validate `azp` — the app
  compares `idToken.Nonce` itself.
- JWKS: `RemoteKeySet` caches keys in memory and refetches on unknown `kid`
  ("the strategy recommended by the spec" per the source comment), with a
  singleflight guard against thundering refreshes. Reuse one
  Provider/Verifier for the process lifetime; do not build per request.
- PKCE: from x/oauth2 (since v0.13.0, 2023): `oauth2.GenerateVerifier()`,
  `oauth2.S256ChallengeOption(v)` on `AuthCodeURL`, `oauth2.VerifierOption(v)`
  on `Exchange`. `AuthCodeURL`'s doc itself now recommends PKCE.
- State/nonce: not generated or validated by either library. `oidc.Nonce(n)`
  is just `SetAuthURLParam("nonce", n)`; `state` is a plain string argument.
  App-side responsibility (see "we implement" below).
- Refresh: `oauth2.TokenSource` auto-refreshes, but per the section above we
  don't use it. `oidc.ScopeOfflineAccess` exists if that ever changes.
- Maintenance health: latest release v3.20.0 (2026-07-08); steady 1-3 month
  cadence through 2025-2026 (v3.12.0 Jan 2025 ... v3.19.0 Jun 2026). ~2.4k
  stars, ~15 open issues, 1,331 known importers on pkg.go.dev. Used by dex,
  Ory, Argo CD, Coder, and Authelia itself. De-facto standard OIDC client
  for Go.
- Dependencies: tiny — `go-jose` + stdlib; x/oauth2 is near-stdlib.
- HTTP coupling: outbound only. A custom `*http.Client` goes in via
  `context.WithValue(ctx, oauth2.HTTPClient, c)` / `oidc.ClientContext`, and
  discovery, token exchange, and JWKS all use it. Nothing touches the inbound
  request type, so there is zero Fiber friction: read `c.Query("code")` /
  `c.Query("state")` off the Fiber ctx and call `Exchange` directly. No
  adaptor needed.
- Known gaps: no RP-initiated logout helper (build the `end_session_endpoint`
  URL ourselves via `Provider.Claims` if we want provider-side logout), no
  `at_hash` auto-check (irrelevant, we discard the access token), no JWE, no
  PAR/JARM (irrelevant for Authelia-class providers), strict `exp` with zero
  clock-skew leeway.

## Candidate 2: zitadel/oidc v3 (`pkg/client/rp`)

Sources: [repo](https://github.com/zitadel/oidc),
[rp godoc](https://pkg.go.dev/github.com/zitadel/oidc/v3/pkg/client/rp),
`relying_party.go` source, release history, issue tracker.

- Discovery: `rp.NewRelyingPartyOIDC(ctx, issuer, clientID, clientSecret,
  redirectURI, scopes, opts...)` runs discovery on the issuer and wires the
  endpoints, JWKS `RemoteKeySet`, and verifier automatically.
- Batteries-included flow: `rp.AuthURLHandler(stateFn, rp)` and
  `rp.CodeExchangeHandler(callback, rp)` handle state generation, transfer via
  a signed/encrypted cookie, state comparison, code exchange, and ID-token
  validation; `rp.UserinfoCallback` chains a userinfo call. PKCE via
  `rp.WithPKCE(cookieHandler)` stores the verifier in the cookie between
  redirect and callback.
- ID-token verification is stronger than go-oidc out of the box: signature,
  `iss`, `aud`, `azp` (go-oidc skips azp), `exp`, `iat` with configurable
  clock-skew (`WithIssuedAtOffset`), nonce via `WithNonce(fn)`, optional
  `acr`/`auth_time`, and `at_hash` via `VerifyTokens`. JWKS is cached with
  refetch-on-rotation, backed by go-jose/v4.
- Extras go-oidc lacks: `rp.RefreshTokens` (verifies the refreshed ID token)
  and `rp.EndSession` for RP-initiated logout.
- Maintenance health: very active — v3.47.9 released 2026-07-15, v4 prereleases
  in flight, ~1.9k stars, 29 open issues, OpenID Foundation certified RP
  (basic + config profiles). Adoption is narrower: 59 known importers of
  `pkg/client/rp` (incl. LXD, Incus, PhotoPrism, zrok) vs go-oidc's 1,331.
- The catch for us, part 1 — net/http coupling: every handler helper returns
  `http.HandlerFunc`, and the `CookieHandler` (gorilla/securecookie) is built
  on `http.ResponseWriter`/`*http.Request`/`*http.Cookie`; none of it works
  against fasthttp. The context-based primitives (`rp.AuthURL`,
  `rp.CodeExchange`, `rp.VerifyTokens`, `rp.EndSession`) are usable from
  Fiber, but then we hand-roll the state/nonce/PKCE cookie plumbing anyway —
  which is precisely the part zitadel was supposed to give us over go-oidc.
- The catch, part 2 — dependency footprint: it is a combined client+server
  module; importing `pkg/client/rp` drags in chi, rs/cors, logrus,
  OpenTelemetry, gorilla/securecookie, doublestar, and more (~20 direct
  deps). Requires the latest two Go versions only.
- Issue-tracker signal: mostly interop edges (Okta/Entra client auth,
  userinfo-as-JWT claims dropped [#849], stateFn can't see the request
  [#838]) — none blocking for an Authelia-first RP, but they cluster around
  exactly the convenience layers we couldn't use from Fiber anyway.

Verdict: excellent, certified library whose advantages (handler helpers,
cookie-based state/PKCE transfer, refresh, end-session) largely evaporate
behind Fiber, leaving a primitives-level API equivalent to go-oidc's with a
10x heavier dependency tree. Its stricter verifier (azp, iat skew, at_hash)
is real but replicable with a few lines on top of go-oidc.

## Candidate 3: hand-rolling on x/oauth2

x/oauth2 alone gives the code exchange and PKCE but no discovery document
parsing, no JWT verification, and no JWKS handling. We would be re-implementing
JWS signature verification and key rotation, the two pieces with the worst
failure modes, to save a single small, well-audited dependency. Rejected.

## Fiber v2 / fasthttp integration cost

- Outbound (discovery, token exchange, JWKS): plain `*http.Client`,
  independent of fasthttp. Zero coupling for any of the candidates.
- Inbound: the OIDC callback only carries `code` and `state` in the query
  string. With go-oidc we read them straight off `*fiber.Ctx`; no conversion.
  If a library insists on `*http.Request`/`http.Handler` (zitadel's helpers
  do), Fiber's `middleware/adaptor` (`HTTPHandler`, `ConvertRequest`, backed
  by `fasthttpadaptor.ConvertRequest`) bridges it at microsecond-scale cost on
  two low-traffic routes — workable but it drags net/http semantics (cookie
  writing via `http.ResponseWriter`) into a fasthttp app, which is exactly
  where the impedance mismatch bites.
  [Fiber adaptor docs](https://docs.gofiber.io/api/middleware/adaptor).

## Decision: what go-oidc gives us vs. what we implement

Gives us for free:

- Discovery document fetch + issuer validation (`oidc.NewProvider`)
- OAuth2 endpoints wired into `oauth2.Config` (`Provider.Endpoint()`)
- Authorization Code exchange incl. confidential-client auth
  (`oauth2.Config.Exchange`)
- PKCE S256 primitives (`GenerateVerifier`/`S256ChallengeOption`/
  `VerifierOption`)
- ID-token verification: signature, `iss`, `aud`, `exp`/`nbf`, alg allowlist
  (`Provider.Verifier(...).Verify`)
- JWKS fetch, in-memory cache, rotation on unknown `kid`, singleflight
- Claims decoding into our own struct (`IDToken.Claims(&v)`)
- Optional userinfo call (`Provider.UserInfo`) if ID-token claims ever fall
  short

We implement (all small, and mostly things we'd own regardless):

- Generate cryptographically random `state` + `nonce` + PKCE verifier per
  login attempt; stash them server-side or in a short-lived HttpOnly cookie
  bound to the login attempt; verify `state` on callback; compare
  `idToken.Nonce` after `Verify` (go-oidc explicitly leaves nonce to the
  caller)
- The two Fiber routes (`/auth/login` redirect, `/auth/callback`) reading
  query params off the Fiber ctx
- Map verified claims (`sub` as the stable identity key, plus
  `preferred_username`/`email`) to our user row and mint our own session
  cookie — deliberately ours, not the library's
- Config surface: issuer URL, client ID/secret, redirect URL, scopes
- (Later, optional) provider-side logout: read `end_session_endpoint` via
  `Provider.Claims` and build the redirect ourselves

Explicitly out: refresh tokens / `offline_access` (nothing to refresh),
multi-provider, JWE, PAR.

## Sources

- OpenID Connect Core 1.0 — https://openid.net/specs/openid-connect-core-1_0.html
  (§3.1.2.1, §3.1.3.1, §3.1.3.7, §11)
- RFC 9700 OAuth 2.0 Security BCP — https://datatracker.ietf.org/doc/html/rfc9700 (§2.1.1)
- go-oidc v3 — https://pkg.go.dev/github.com/coreos/go-oidc/v3/oidc,
  https://github.com/coreos/go-oidc
- x/oauth2 — https://pkg.go.dev/golang.org/x/oauth2
- zitadel/oidc v3 — https://pkg.go.dev/github.com/zitadel/oidc/v3/pkg/client/rp,
  https://github.com/zitadel/oidc
- Fiber adaptor — https://docs.gofiber.io/api/middleware/adaptor
- Authelia OIDC — https://www.authelia.com/configuration/identity-providers/openid-connect/provider/
