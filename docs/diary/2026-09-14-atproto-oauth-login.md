# Diary: log in with atproto OAuth

Replace the (never wired) email magic-link login with atproto OAuth. The DID becomes the user
identity; the app is a confidential OAuth client; first login writes the user's
`com.audioadastra.actor.profile` record. Design decisions are in `/docs/decisions.md`
(2026-09-14 entry); this diary is the build narrative.

## Step 1: brainstorm and requirements

**Author:** main

### Prompt Context

**Verbatim prompt:** "Let's do OAuth login. What do we need?" followed by a one-question-at-a-time
brainstorm; key answers: "Replace", "Handle for now", "atproto + repo:com.audioadastra.*",
"Include audio and image" (blob scopes), confidential client "Yes", drop `accounts`/`tokens`, keep
`goqite`/`sessions`/roles tables, "What about just leaving the handle out for now?", "Per-device"
OAuth sessions, auto-create the profile record with `createdAt` only, profile write failure fails
login ("No"), minimal UI, localhost dev mode, "don't write anything to the network without my
explicit approval ... I want the real network only in prod", and include a local PDS in docker
compose for manual end-to-end runs.

**Interpretation:** a complete login/logout feature on atproto OAuth, nothing beyond it (no profile
page), with strict no-real-network-writes discipline in dev and tests.

**Inferred intent:** get identity right once, in the shape the protocol rewards, so every later
feature (profile editing, uploads, indexing) builds on a DID-keyed user and an authenticated PDS
client.

### What I did

Researched the current auth surface (glue's scs session middleware reads one `userID` key; no
login route exists; `tokens`/`accounts` unused), indigo's `atproto/auth/oauth` client package
(`ClientApp`, `ClientAuthStore`, `StartAuthFlow`, `ProcessCallback`, `ResumeSession`, `Logout`,
`NewLocalhostConfig`, and the `cmd/oauth-web-demo` reference app), and how app views keep handles
fresh (Bluesky's `actor` table with nullable handle, bidirectional re-verification on `#identity`
and daily; Tap verifies before emitting; Frontpage verifies per render). Outcome: no handle column
yet, since only the logged-in user's handle is rendered and indigo's directory resolves and
verifies it with an in-memory cache.

Walked the schema table by table. Kept: `goqite`, `sessions`, `roles`, `users_roles`,
`permissions`, `roles_permissions`. Dropped: `accounts`, `tokens`. `users` is recreated as `id`,
`created`, `updated`, `did unique`, `active`. Added: `oauth_auth_requests`, `oauth_sessions`.

### Why

See the decisions entry. The table walk was deliberate: every column that stays is one a DID-keyed
app actually uses.

### What worked

Two parallel researchers on handle freshness converged on the same mechanism from different
codebases, which made "leave the handle out for now" an easy call rather than a guess.

### What didn't work

I initially listed "exclude metadata/JWKS from origin protection" as a requirement; Go's
`http.CrossOriginProtection` only rejects cross-origin unsafe methods, and every externally-hit
endpoint here is a GET. Dropped.

### What I learned

`createRecord`/`putRecord` `validate` is tri-state; goat sends `true` by default, apps should leave
it unset and validate against the lexicon catalog themselves. Indigo's `ClientMetadata()` does not
fill `jwks_uri`; a confidential client sets it.

### What was tricky

Testing without touching the real network: the OAuth consent page needs a browser, so automated
tests use in-process fakes and the local docker network is for manual runs only.

### What warrants review

The decisions entry and the requirements handed to the builder (next step).

### Future work

Profile page/editing; cached handles + identity events with the indexing/Tap feature; token
encryption at rest; rate limiting on `/login`.

## Step 2: build the login, the fakes, the local network, and review

**Author:** oauth-login-builder

### Prompt Context

**Verbatim prompt:** "Login with atproto OAuth, replacing the never-wired email magic-link
scaffolding. Full requirements follow; the project's existing conventions apply" followed by the seven
numbered sections (identity model and schema, OAuth client and config, login flow, sessions/store/logout,
telemetry, tests with no network, local network in docker compose) and the acceptance criteria, plus the
hard constraints: never write to the real atproto network, open source, do not commit, do not modify
`.env`. Two follow-up messages from the lead refined conventions: one file per domain in `service/`,
wiring functions panic on bad options, key-bearing clients built in `main`, named returns with a
deferred recorder, `login.condition` instead of a `login.outcome` enum, semconv keys where they exist,
and tests through the fake network with `oteltest` assertions.

**Interpretation:** a complete, tested login/logout on atproto OAuth using indigo's client, with the
persistence, telemetry and local-network scaffolding around it, built against in-process fakes.

**Inferred intent:** get the identity foundation right, so every later feature can assume a DID-keyed
user and an authenticated PDS client, and prove it without touching the real network.

### What I did

Schema: `/sqlite/migrations/1789409854-oauth.up.sql` drops `tokens`, `users_roles`, `users` and
`accounts` in that order (foreign keys are on, so children first), recreates `users` as `id`, `created`,
`updated`, `did unique`, `active`, recreates `users_roles`, and adds `oauth_auth_requests` and
`oauth_sessions` with one column per field of indigo's `AuthRequestData` and `ClientSessionData`,
scopes stored space-separated as on the wire. The down migration recreates the previous shape.
`/sqlite/testdata/fixtures/admin.sql` now inserts a DID-keyed admin.

Model: `/model/auth.go` has `User{ID, Created, Updated, DID, Active}` and a `model.DID` string type;
`Account`, the email fields, the token errors, the login email job (`/jobs/email.go`) and its job data
are gone. `/model/error.go` gained the login refusals (`ErrorIdentityUnresolved`,
`ErrorAuthServerUnavailable`, `ErrorLoginCancelled`, `ErrorScopeDenied`, `ErrorProfileWriteFailed`) and
the store's not-found errors.

Store: `/sqlite/oauth.go` makes `*sqlite.Database` an `oauth.ClientAuthStore`. `SaveAuthRequestInfo`
sweeps requests older than ten minutes in the same transaction; `SaveSession` is an upsert that sweeps
sessions untouched for a year. `/sqlite/auth.go` adds `GetOrCreateUser` (`insert ... on conflict do
nothing` plus `select changes()`, so concurrent first logins agree on one user).

Lexicons: `/lexicons/lexicons.go` embeds the schemas and exposes `NewCatalog()` and the
`ActorProfile` NSID, so the profile record is validated against the real catalog before `putRecord`.

Service: `/service/auth.go` has `NewOAuthClientConfig` (localhost mode for a `localhost`/`127.0.0.1`
base URL, with the callback on `127.0.0.1` as the OAuth spec requires; confidential client with the
P-256 key otherwise, refusing to start without one), and the operations `StartLogin`, `FinishLogin`,
`Logout`, `PDSClient`, `ResolveHandle`, each with a wiring function that panics on a missing
dependency. `StartLogin` is a re-composition of indigo's `StartAuthFlow` from its lower-level pieces
(`Dir.Lookup`, `Resolver.ResolveAuthServerURL`/`ResolveAuthServerMetadata`, `SendAuthRequest`) so the
identity, PDS host and auth server land on the span and each outbound call gets its own child span.
`FinishLogin` takes the callback query plus the state the browser's session started with, runs
`ProcessCallback`, checks the granted scopes as parsed permissions, gets or creates the user, refuses
inactive users, reads or writes the profile record, and deletes the OAuth session on any refusal
(with `context.WithoutCancel`). A `loginEvent` gathers attributes as they become known, sets them on
the span in the context and logs them all on failure; `login.condition` names the refusal.

HTTP and HTML: `/http/login.go` (`GET /login`, `POST /login`, `GET /oauth/callback`, `POST /logout`),
`/http/oauth.go` (client metadata with `jwks_uri`, `client_name`, `client_uri`, and the JWKS),
`/http/auth.go` (the middleware now also loads the OAuth session ID and the handle into the context and
exports `GetUserFromContext`/`GetOAuthSessionIDFromContext`; a cookie whose OAuth session is gone is
destroyed and sent to `/login`), `/html/login.go` (the form, from the Tailwind Plus simple sign-in
form trimmed to one field), `/html/viewer.go` and the nav in `/html/common.go`.

Tests: `/atprototest/network.go` is an in-process fake auth server and PDS behind one TLS test server
that a custom transport routes every https host to, plus indigo's `MockDirectory`. It does the DPoP
nonce dance on both the auth server (400 `use_dpop_nonce`) and the PDS (401 with `WWW-Authenticate`),
PKCE, single-use codes, a client assertion check for confidential clients, token refresh, revocation,
and `getRecord`/`putRecord`. `/service/auth_test.go` and `/http/login_test.go` run the real
`oauth.ClientApp` and the real router (over TLS at `https://app.test`, with a cookie jar per browser)
against it. `/sqlite/oauth_test.go` and `/sqlite/auth_test.go` cover the store.

Local network: `/docker-compose.yml` (Postgres, the published PLC image, the real PDS image in dev mode,
Caddy with its internal CA in front of the PDS as `https://pds.localhost`), `/docker/caddy/Caddyfile`,
Makefile targets `atproto-up`, `atproto-down`, `atproto-account`, the env vars `ATPROTO_PLC_URL`,
`ATPROTO_CA_FILE`, `ATPROTO_LOCAL_HANDLE_SUFFIX` handled in `/cmd/app/atproto.go`, and a README section
written by a writer sub-agent from a brief.

Manual run: `make atproto-up`, `make atproto-account HANDLE=alice PASSWORD=alice-password`, then the
app with `BASE_URL=http://127.0.0.1:8080 ATPROTO_PLC_URL=http://localhost:2582
ATPROTO_CA_FILE=local/caddy/caddy/pki/authorities/local/root.crt ATPROTO_LOCAL_HANDLE_SUFFIX=.test
DATABASE_PATH=local/app.db go run -tags sqlite_fts5,sqlite_math_functions ./cmd/app`, and
`playwright-cli` with a config of `{"browser":{"contextOptions":{"ignoreHTTPSErrors":true}}}`:
`/login`, handle `alice.test`, the PDS sign-in page at `https://pds.localhost/oauth/authorize`, password,
Authorize, back on `/` with `@alice.test` and a Log out button. `local/app.db` had one user, one
`oauth_sessions` row with all four scopes granted, zero pending auth requests; the PDS had the profile
record with `$type` and `createdAt`; Log out left zero sessions and the PDS log showed two
`/oauth/revoke` calls.

Then `fabrik:code-review` with two competing reviewers, and fixes for their findings (below).

### Why

The flow is composed from indigo's lower-level helpers rather than `StartAuthFlow` because that helper
returns only the redirect URL, and the span needs the DID, handle, PDS host and auth server that are
resolved on the way. The fakes speak the real protocol so the real client runs unchanged in tests; a
stub at the `ClientApp` boundary would have left DPoP, PKCE and the store callbacks untested.

### What worked

The fake network was the right investment: the service and HTTP suites passed on their first run, and
the real PDS then behaved the same way. Go accepts Caddy's `*.test` wildcard certificate and resolves
`alice.test` bidirectionally through the local PLC and the PDS's well-known endpoint, so handle login
works locally with no `/etc/hosts` edits.

### What didn't work

- `repo:com.audioadastra.*` does not parse: `auth.ParsePermissionString` returns `invalid permission
  parameters: NSID syntax didn't validate via regex`, and the permission spec says partial wildcards
  are not supported (only `*`). `auth.ParseOAuthScope` silently drops unparseable entries, so a check
  built on it alone would pass vacuously. The code requests `repo:com.audioadastra.actor.profile`
  instead, `TestOAuthScopes` asserts every required scope parses, and this is the first open question
  for the lead, since the decisions entry says the wildcard.
- The first published PLC image tag (`plc-f0599f71...`) fails with `Error: Cannot find module
  '/app/packages/plc/service/index.js'`; the newest tag (`plc-f2ab7516...`) has the current layout.
  Both are amd64 only.
- `PDS_SERVICE_HANDLE_DOMAINS=.pds.localhost` is refused by `createAccount` with
  `{"error":"InvalidHandle","message":"Handle TLD is invalid or disallowed"}`: `.localhost` is a
  disallowed handle TLD. `.test` is the one reserved TLD both the PDS and indigo allow.
- `curl --cacert root.crt https://alice.test/...` fails with `SSL: no alternative certificate subject
  name matches target host name 'alice.test'` even though the certificate's SAN is `*.test`: macOS
  curl refuses a wildcard on a top-level domain. Go does not, which a throwaway test in `cmd/app`
  confirmed before it was deleted.
- A store test that backdated `oauth_sessions.updated` with an `update` got the timestamp reset by the
  `oauth_sessions_updated_timestamp` trigger; the test now inserts the aged rows directly.
- The fixation test first asserted a session cookie after `GET /login`, but scs only writes a cookie
  on the first session write, which is `POST /login`; the test now captures the cookie after the post.
- `make lint` flagged `FormEl` as deprecated; `Form` is the current gomponents element.

### What I learned

The OAuth spec's localhost exception is strict: the client ID is `http://localhost` without a port,
and loopback redirect URIs must be `127.0.0.1` or `[::1]`, never the `localhost` name. That is why
localhost mode always puts the callback on `127.0.0.1` and why `.env.example` now uses
`BASE_URL=http://127.0.0.1:8080`. Indigo's resolver, on the other hand, insists on `https` without a
port for the PDS and auth server, so a local PDS needs a TLS proxy on 443, and its SSRF transport
blocks loopback, so local mode swaps in plain clients (only when `ATPROTO_PLC_URL` is set). chi
overwrites a route registered twice, which is how the app's `POST /logout` replaces the one the server
setup registers first.

### What was tricky

Making one custom transport serve both the app's OAuth clients and the test browser: it dials any
`:443` address to the fake server, except hosts routed elsewhere (the app under test), with the TLS
`ServerName` pinned to the test certificate's name so any hostname works.

The review found a real login CSRF: the callback trusted any `state` in the store, so a callback URL
captured from one flow could log another browser in as the attacker. `StartLogin` now returns the
state, the handler keeps it in the cookie session, and `FinishLogin` refuses a callback whose state is
not the session's. The review also found that a logged-in browser hitting the callback would overwrite
its session and orphan the old OAuth session (the callback now redirects logged-in users away), that
`RenewToken` after `FinishLogin` could orphan a fresh session on failure (it runs before), that
`POST /login` under `RedirectIfAuthenticated` would turn into a 405 via a 307 (the post now redirects
itself), that no-code and wrong-issuer callbacks were mapped to a 502 instead of "cancelled", and that
`ResolveHandle` defaulted to the real directory when wired without one (it panics now). Comments that
named the HTTP layer from `service` and `sqlite` were reworded, single-use package-level values were
inlined, and duplicate keys in the login event are deduplicated.

### What warrants review

- `/service/auth.go`: the state binding in `finishLogin`, the deferred session delete, and the scope
  check. `/http/login.go`: the callback's order of operations (renew, pop state and redirect, finish,
  put). `/sqlite/migrations/1789409854-oauth.up.sql`: drop order and the down migration.
- `make test`, `make lint`, and `go test -race ./http/ ./service/ ./sqlite/` all pass. The full
  suite runs without network access.
- The manual run above against the local network, reproducible from the README section.

### Future work

The decisions entry and the required scopes disagree (see the open question). `oauth.ClientSession`
instances are built per request and not shared, so a future feature that calls the PDS from two
concurrent requests of one session should cache the session per `(did, sessionID)` to serialize token
refreshes. Glue's built-in `POST /logout` could use an opt-out so the app does not rely on route
shadowing. The lead's Step 1 "Future work" (profile page, cached handles, token encryption at rest,
rate limiting on `/login`) stands.

## Step 3: settle the scope, stop the local network, open the PR

**Author:** oauth-login-builder

### Prompt Context

**Verbatim prompt:** "Markus chose option 1 (explicit `repo:com.audioadastra.actor.profile`); I amended
`docs/decisions.md` accordingly (staged -- don't rewrite it). Now: 1. Add a short Step 3 to the diary
... then also `make atproto-down` ... 2. Commit everything staged as a small number of logical commits
... 3. Push the branch ... 4. Open the PR ... Add a "## Screenshots" section ... publish it as a private
Artifact ... 5. Report: commit hashes, PR URL, Artifact URL, and confirm `docker compose ps` is empty."

**Interpretation:** the open question from Step 2 is closed in favour of the explicit collection;
finish the work by recording that, taking the local network down, and shipping the branch as a PR.

**Inferred intent:** leave nothing running and nothing undocumented, with the PR ready for review.

### What I did

Recorded here that the required scopes are settled as `atproto`, `repo:com.audioadastra.actor.profile`,
`blob:audio/*` and `blob:image/*`, matching `service.OAuthScopes` and the amended decisions entry; a
published permission set is the follow-up that will bundle future collections. Started the app against
the local network once more to capture three Playwright screenshots (`/login`, the PDS consent page,
the logged-in nav), built one HTML page with the images as data URIs and published it as a private
Artifact for the PR. Stopped the app and ran `make atproto-down`; `docker compose ps` lists nothing.
Committed the staged work as a few logical commits, pushed `worktree-atproto-oauth-login`, and opened
the PR with Markus as reviewer.

### Why

The scope decision was the one thing the build could not settle on its own. The screenshots are the
review surface for the user-facing part, kept out of GitHub as an Artifact per the project's PR
conventions.

### What worked

The local network came back up from its volumes with the `alice.test` account intact, so the
screenshot run needed no account setup.

### What didn't work

Nothing failed in this step.

### What I learned

`docker compose down` removes the network but keeps `local/`, so the next `make atproto-up` reuses
the PLC database, the PDS data and Caddy's CA; the `ATPROTO_CA_FILE` path stays valid across restarts.

### What was tricky

Nothing in particular.

### What warrants review

The PR itself; its description lists what shipped and links the screenshots.

### Future work

A `com.audioadastra` permission set so new collections do not each re-prompt for consent.
