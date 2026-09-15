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

Walked the schema table by table. Kept: `goqite`, `sessions`, and at first `roles`, `users_roles`,
`permissions`, `roles_permissions` (dropped later in this feature at Markus's request, see Step 5).
Dropped: `accounts`, `tokens`. `users` is recreated as `id`,
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

## Step 4: external review of the PR

**Author:** oauth-login-builder

### Prompt Context

**Verbatim prompt:** "Three findings from an external review of PR #12 to apply (I verified each
against the code): 1. `http/oauth.go` `OAuthMetadata`: the JWKS URI is built from the raw `baseURL`
... 2. `docker-compose.yml`: bind the published ports to loopback ... 3. `service/auth.go`
`putProfile`: make first-login profile creation conditional on absence. Send `"swapRecord": null` ...
Then: run `make test` and `make lint`, add a diary Step 4 ... commit as one commit ... push."

**Interpretation:** three concrete defects with concrete fixes; apply them with tests, no redesign.

**Inferred intent:** close the gaps a fresh pair of eyes found before the PR merges.

### What I did

`/http/oauth.go`: `OAuthMetadata` trims a trailing slash from the base URL once, the same rule
`service.NewOAuthClientConfig` applies, so `client_uri` and `jwks_uri` agree with `client_id` and the
callback. A test builds the metadata route with `https://app.test/` and asserts all four URLs are
slash-free.

`/docker-compose.yml`: Caddy and the PLC publish on `127.0.0.1` only, since the dev PDS has open
signup and a committed admin password; the README says so.

`/service/auth.go`: `putProfile` sends `"swapRecord": null`, which the PDS reads as "the record must
not exist", and treats the `InvalidSwap` error as a profile another login wrote in the meantime:
the login succeeds with `login.profile_created=false`. `/atprototest/network.go` honours the null
swap and has a `PutRecordRaces` knob that plants a record just before each write; the service test
covers the race, and the existing first-login test covers the plain write.

### Why

A trailing slash in `BASE_URL` would have advertised a `jwks_uri` that 404s and broken every
confidential login; the loopback binding keeps the dev network off other interfaces; the conditional
write is what the atproto spec provides for exactly this read-then-write race.

### What worked

`atclient.Post` marshals the body with `encoding/json`, so a nil map value goes out as a JSON `null`
rather than being omitted, which is what the swap needs; the `putRecord` lexicon lists `InvalidSwap`
as its one error.

### What didn't work

Nothing failed in this step.

### What I learned

`swapRecord` is the only atproto-native guard against duplicate first writes; indigo's client passes
it through untouched, and the PDS answers `InvalidSwap` with the current CID in the message.

### What was tricky

The fake PDS has no CIDs, so it honours only the null form of `swapRecord`; a CID swap would need
real content addressing, which nothing here sends.

### What warrants review

`putProfile` in `/service/auth.go` and the `PutRecordRaces` branch in the fake; the trailing-slash
test in `/http/login_test.go`.

### Future work

None beyond what earlier steps list.

## Step 5: drop user roles and permissions

**Author:** oauth-login-builder

### Prompt Context

**Verbatim prompt:** "Scope change from Markus on PR #12: drop the user roles/permissions machinery
entirely (we re-add it if a need appears). ... Migration: extend this feature's up migration to also
drop `roles`, `users_roles`, `permissions`, `roles_permissions` ... Remove `GetPermissions` from
`sqlite/auth.go` and its tests, any `model` aliases for `Role`/`Permission` that are now unused, the
`PermissionsGetter: db` line in `main.go`, and anything in `sqlite/testdata/fixtures/admin.sql` that
seeded roles."

**Interpretation:** the login feature keeps only what a DID-keyed user needs; the role tables from
the template go with the email scaffolding.

**Inferred intent:** no unused machinery in the schema or the model until a feature asks for it.

### What I did

`/sqlite/migrations/1789409854-oauth.up.sql` now drops `tokens`, `users_roles`, `roles_permissions`,
`permissions`, `roles`, `users` and `accounts`, children before parents, and no longer recreates
`users_roles`; the down migration recreates the role and permission tables as `1755714454-auth`
defined them, `admin` row included. `sqlite.Database.GetPermissions` and its test are gone, as are
`model.Role`, `model.RoleAdmin` and `model.Permission`, the role row in the `admin` fixture, and the
`PermissionsGetter` in `/cmd/app/main.go`. The server adds its permissions middleware only when a
getter is given, so nothing else changes. The decisions entry never listed the kept tables, so it
stands; the Step 1 sentence that did is corrected above.

### Why

The role tables were template inheritance with no reader: `GetPermissions` always returned an empty
list, since no permission was ever defined.

### What worked

The repository grep for `Permission`, `Role`, `roles` and `users_roles` found only the places listed
above plus `role="alert"` in the login form, which is an ARIA attribute and stays.

### What didn't work

Nothing failed in this step.

### What I learned

The glue server's `Authenticate` and `Logout` need no permissions type; only `Authorize` and
`SavePermissionsInContext` do, and both are opt-in.

### What was tricky

Nothing in particular.

### What warrants review

The drop order at the top of the up migration and the recreated tables at the bottom of the down
migration.

### Future work

Re-add roles when a feature needs an admin, with a lexicon-shaped idea of what that admin may do.

## Step 6: review feedback on the local network and config

**Author:** oauth-login-builder

### Prompt Context

**Verbatim prompt:** "Review feedback on PR #12, all triaged with Markus; apply as one batch: 1.
`.env.example`: remove every comment line ... 2. `docker-compose.yml`: remove all comments. 3.
Volumes: replace the `./local/...` bind mounts for plc-db, pds and caddy with named docker volumes
... 4. `postgres:16-alpine` -> `postgres:18-alpine`. 5. PLC: build from source instead of the 2023
image ... 6. `README.md`: revert to exactly what's on `main` ... 7. `.test` handle suffix stays as is.
Then: bring the local network up from scratch ... re-run the full playwright login + logout once ...
Diary Step 6 covering the review batch, the CA-extraction step, and the `goat key generate` hint that
left `.env.example`."

**Interpretation:** the local network becomes self-contained (named volumes, a source-built PLC, a
current Postgres) and the config files carry values only; the README stays as it was on `main`.

**Inferred intent:** keep dev tooling reproducible from the repo alone, and keep configuration files
free of prose.

### What I did

`/.env.example` is `KEY=value` lines only; the hint it carried is now here: a confidential client
needs a P-256 key in multibase encoding, made with `goat key generate -t P-256 --terse`, in
`OAUTH_PRIVATE_KEY`, and any short name for it in `OAUTH_KEY_ID`.

`/docker-compose.yml` has no comments, uses the named volumes `plc-db`, `pds` and `caddy`, runs
`postgres:18-alpine`, and builds the PLC directory from
`https://github.com/did-method-plc/did-method-plc.git#996e23b5ced9c15b32bcc612dd304880342ca4ab`
with `packages/server/Dockerfile`; that commit's `service/index.js` still reads `DB_CREDS_JSON`,
`DB_MIGRATE_CREDS_JSON`, `ENABLE_MIGRATIONS` and `PORT`, so the environment is unchanged, and the
`platform` pin is gone since the build is native.

Caddy's root certificate now lives in the `caddy` volume, so `make atproto-ca` copies it out with
`docker compose cp caddy:/data/caddy/pki/authorities/local/root.crt data/caddy-root.crt`;
`make atproto-account` depends on it, and `ATPROTO_CA_FILE=data/caddy-root.crt` is the path for the
app. `make atproto-down` stops the containers and keeps the volumes; `make atproto-clean` runs
`docker compose down --volumes` and removes the copied certificate. `/data/` replaces `/local/` in
`.gitignore`, and the `local/` directory is deleted. `/README.md` is back to `main`.

End to end on the rebuilt network: `make atproto-clean`, `make atproto-up` (the PLC build from
source took a few minutes the first time), `make atproto-account HANDLE=alice PASSWORD=alice-password`
(new DID `did:plc:mlnmbian4vh5hvqycog7z5k3`), the app with `ATPROTO_CA_FILE=data/caddy-root.crt` and
`DATABASE_PATH=data/app.db`, then Playwright: `/login`, handle `alice.test`, PDS sign-in, Authorize,
back on `/` as `@alice.test`. The database had one active user, one OAuth session and no pending
auth request; the PDS had the profile record. Log out left zero sessions and the PDS logged two
`/oauth/revoke` calls. `make atproto-down` afterwards; `docker compose ps` lists nothing. The login
page and the nav did not change, so the screenshots stand.

### Why

Named volumes need no host directories and clean up with one flag; building the PLC from a pinned
commit replaces a two-year-old published image whose layout had already drifted once during this
feature.

### What worked

The pinned PLC commit builds with its own Dockerfile and starts against Postgres 18 with the same
environment as the published image.

### What didn't work

Nothing failed in this step.

### What I learned

`docker compose cp` reads straight out of a named volume through the running container, so the
certificate never needs a bind mount. The certificate is issued once per volume: `atproto-clean`
invalidates the copied file, which is why the target removes it.

### What was tricky

Nothing in particular.

### What warrants review

The Makefile targets `atproto-ca`, `atproto-account` and `atproto-clean`, and the compose `build`
block with the pinned commit.

### Future work

None beyond what earlier steps list.

## Step 7: second review round

**Author:** oauth-login-builder

### Prompt Context

**Verbatim prompt:** "Second review batch on PR #12, triaged with Markus; apply all: 1.
`sqlite/testdata/fixtures/admin.sql`: the DID `did:plc:admin000000000000000000` is invalid ... 2.
`sqlite/auth.go` `GetOrCreateUser`: drop the transaction and `select changes()` ... 3. `.env.example`:
no blank lines, all keys sorted lexically. 4. Migration `1789409854-oauth.up.sql`: add `updated` ...
to `oauth_auth_requests` ... 5. Restore `model/jobs.go` ... and `jobs/email.go` + `jobs/email_test.go`
from `main` as generic send-email infrastructure ... but remove the `case "login"` branch ... 6.
`model/auth.go`: on `DID.String()` use the doc comment `// String satisfies [fmt.Stringer].` ... 7.
Move the `ActorProfile` NSID constant out of `lexicons/lexicons.go` into `model`."

**Interpretation:** seven small corrections, none changing behaviour of the login itself.

**Inferred intent:** keep the model, fixtures and infrastructure in the shape the rest of the project
will build on.

### What I did

The fixture and the two tests that assert on it use `did:plc:adminadminadminadminadmi`, a
well-formed did:plc (24 base32 characters). `sqlite.Database.GetOrCreateUser` is one statement,
`insert ... on conflict (did) do nothing returning *`, with a plain select when the insert returns no
row; `created` is whether it did. `oauth_auth_requests` has `updated` and its trigger, and the row
struct scans it. `/model/jobs.go`, `/jobs/email.go`, `/jobs/email_test.go` and `/jobs/register.go`
are back from `main` minus the login branch: the switch has no cases yet and returns an error for
any type, which the test covers; `model.Keywords` is back for the job data. `model.DID.String` has
the `fmt.Stringer` comment and assertion. The profile NSID is `model.CollectionActorProfile` in
`/model/atproto.go`, and `lexicons` keeps only the embedded schemas and `NewCatalog`.
`.env.example` is sorted with no blank lines.

### Why

Email stays as infrastructure because the app will send email for other reasons than login; the
NSID belongs in `model` because collections are domain vocabulary, not schema loading.

### What worked

`returning *` on an `insert ... do nothing` is the idiom SQLite offers for exactly this: a row on
insert, no row on conflict, and the existing concurrency test still passes without a transaction.

### What didn't work

Nothing failed in this step.

### What I learned

A did:plc identifier is exactly 24 characters of base32 after the prefix; the old fixture value
had 23 and digits outside the alphabet, which no code checked but a stricter parser would.

### What was tricky

Nothing in particular.

### What warrants review

`GetOrCreateUser` in `/sqlite/auth.go` and the trigger added to `/sqlite/migrations/1789409854-oauth.up.sql`.

### Future work

The first email type to be sent adds its case to `jobs.SendEmail`.

## Step 8: an `atproto` package for client composition

**Author:** oauth-login-builder

### Prompt Context

**Verbatim prompt:** "Third review batch on PR #12, triaged with Markus; apply all: 1. New package
`atproto` (directory `atproto/`, import `app/atproto`) that owns all composition of the atproto/OAuth
clients, moved out of `service` and `cmd/app` ... The scopes become unexported (`scopes`); `service`
reads the requested scopes from the injected client's config ... 2. In `service/auth.go`, inline the
operation bodies into their wiring closures ... following how `GetUser` in `fat.go` does it ... 3.
`make test`, `make lint`; run the local-network login once more only if `main.go` wiring changed
materially (it will -- do it, then `make atproto-down`, `docker compose ps` empty). Diary Step 8."

**Interpretation:** the client construction becomes one package with one constructor, `service` only
consumes the results, and the operation bodies sit where the wiring is.

**Inferred intent:** one place to read for "how does the app reach the atmosphere", and `service`
kept to business logic.

### What I did

`/atproto/client.go` has `atproto.New(atproto.NewOptions{...}) (*atproto.Client, error)`, which
builds the OAuth client configuration (`NewOAuthClientConfig`, moved from `service` with its tests
into `/atproto/client_test.go`), the `*oauth.ClientApp` on the given store, and the identity
directory: indigo's default for the real network, or a `BaseDirectory` on the local PLC with the
loopback-dialing, extra-CA HTTP client that used to live in `/cmd/app/atproto.go`, which is deleted.
`Client` exposes `OAuth`, `Directory` and `Local`; `main` passes `OAuth`, `OAuth.Config` and
`Directory` on to `service.Setup` and `http.InjectHTTPRouter`. The scope list is the unexported
`scopes`; `service` checks granted scopes against `app.Config.Scopes`, the tests read the same, and
the parse check on the scopes is an `atproto` test. `service` does not import `app/atproto`;
`atprototest.NewClientApp` builds the app through `atproto.New` and repoints its clients at the fakes.

In `/service/auth.go` the bodies of `StartLogin`, `FinishLogin`, `Logout` and `PDSClient` are the
wiring closures themselves, as `GetUser` is. The child-span wrappers around outbound calls
(`lookupIdentity`, `discoverAuthServer`, `pushAuthRequest`, `exchangeToken`, `revoke`), the scope
check, the profile read and write, and the login event stay as functions: each is a span or a piece
shared by more than one operation, and inlining them would fold the deferred span ending into the
closures. Behaviour is unchanged.

Local network once more, since `main` changed: `make atproto-up` on the kept volumes, the app on port
8081 (another process of the app was already listening on 8080, so that one was left alone), and
Playwright through `/login`, the PDS sign-in and consent pages, back as `@alice.test`; one user, one
session, no pending request; Log out left zero sessions and two `/oauth/revoke` calls on the PDS.
`make atproto-down`; `docker compose ps` lists nothing.

### Why

The composition rules (localhost versus confidential, real versus local network, which clients lose
SSRF protection) belong together, and `service` should not know them.

### What worked

The localhost OAuth client ignores the callback port, so running on 8081 needed nothing but the base
URL.

### What didn't work

The first attempt to run the app for the re-check failed with `listen tcp :8080: bind: address
already in use`: an `app` process from another checkout was on 8080 and served a 404 for `/login`.
The re-check ran on 8081 instead.

### What I learned

`atprototest` importing `atproto` is the right direction: the fakes exercise the app's own client
construction rather than a parallel one, so a change to the scopes or the client shape is tested
without a second copy.

### What was tricky

Keeping `service` free of the scopes: the granted-versus-requested check now takes the requested
list as an argument, read from the client app's configuration at the call site.

### What warrants review

`/atproto/client.go`, the closure bodies in `/service/auth.go`, and `/cmd/app/main.go`.

### Future work

None beyond what earlier steps list.
