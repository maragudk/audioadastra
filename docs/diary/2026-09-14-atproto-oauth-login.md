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

## Step 9: a profile page instead of the nav viewer, and a glue update

**Author:** oauth-login-builder

### Prompt Context

**Verbatim prompt:** "Fourth review round on PR #12, one item, triaged with Markus: Remove the
viewer-in-context mechanism; replace with a Profile link and page. ... New `GET /profile`
(`http/profile.go`, `html/profile.go`): requires login ... The handler resolves the handle via the
service ... and renders `html.ProfilePage(html.ProfilePageProps{PageProps, Handle})` showing
`@handle` and the "Log out" POST form. ... Retake screenshots ... redeploy the Artifact at the SAME
url". Then: "update `maragu.dev/glue` to the newest commit on its `main` as of today ... Today's change
adds props to the error pages ... adapt our `html/glue.go`/`http/glue.go`/`routes.go` accordingly".

**Interpretation:** the nav decides on `PageProps.UserID` alone; the handle lookup moves to a page
that asks for it; the layout no longer reads anything from the context. Separately, take glue's
newest error page API.

**Inferred intent:** no per-request identity lookups for pages that do not show an identity, and a
`html` package that renders from props only.

### What I did

Deleted `/html/viewer.go`. The nav in `/html/common.go` renders a "Log in" link when
`props.UserID` is nil and a "Profile" link otherwise, nothing else. `AddUserToContext` in
`/http/auth.go` lost the handle resolver and the viewer; it still loads the user, keeps the OAuth
session ID in the context, and destroys a cookie session whose OAuth session is gone. New
`/http/profile.go` registers `GET /profile` behind a `requireUser` middleware of its own (glue's
`Authorize` needs a permissions getter, which the app no longer has); it sends a logged-out request
to `/login?redirect=/profile`, and otherwise resolves the handle through the service, falling back
to `handle.invalid`, and renders `/html/profile.go`: the handle and the Log out form. Tests: the nav
per `UserID` in `/html/common_test.go`, the page in `/html/profile_test.go`, and in
`/http/profile_test.go` the redirect-and-return, the handle, logout from the page, and the
`handle.invalid` fallback; the login flow tests now look for the profile link.

`maragu.dev/glue` is at `v0.0.0-20260915091829-6b8d41e94cf4` (today). Its `html.ErrorPage` and
`html.NotFoundPage` now take the request's `PageProps` as well as the page function, so the error
pages render for the same user as the page that failed; `/html/glue.go` passes them through and
`/http/login.go` gives its three error page returns the props. `http.NotFound` in glue passes the
props itself, so `/http/routes.go` and `/http/glue.go` needed nothing.

Screenshots retaken on the local network on port 8081: login page, consent, the front page logged in
with the Profile link, and the profile page; the Artifact was redeployed at the same URL. The run also
proved logout from the profile page: zero sessions and two `/oauth/revoke` calls afterwards.
`make atproto-down`; `docker compose ps` lists nothing.

### Why

A layout that reads the context is a hidden dependency on middleware; a page that receives its props
is not. The handle is only interesting on the profile page, so only that page pays for the lookup.

### What worked

`requireUser` is eight lines and reuses the `redirect` handling the login page already has, so the
round trip login-then-back-to-profile needed no new code.

### What didn't work

The first version of the `handle.invalid` test nilled the fake directory after login, which does
nothing: the service holds its own reference. Re-inserting the identity with an invalid handle into
the mock directory is what makes the lookup return `handle.invalid`.

### What I learned

glue's new error pages keep every prop but the title and description, so a logged-in user sees the
nav with their Profile link on a 404 too.

### What was tricky

Nothing in particular.

### What warrants review

`/http/profile.go` (the middleware and the redirect target) and the updated flow assertions in
`/http/login_test.go`.

### Future work

Profile editing on the same page.

## Step 10: bound the login flows

**Author:** oauth-login-builder

### Prompt Context

**Verbatim prompt:** "One more finding from the external review, verified, apply on PR #12: glue's
`NewServer` defaults `WriteTimeout` to 10s ... indigo's OAuth client allows 30s per outbound request
... A slow upstream can push a handler past 10s, and Go then drops the response after
`oauth_sessions` was persisted: no cookie, dead connection, orphan session. Fix: 1. In
`cmd/app/main.go` set `WriteTimeout: 30 * time.Second` ... 2. Bound the login operations ... with
`context.WithTimeout(ctx, loginTimeout)` where `loginTimeout = 20 * time.Second` ... 3. A test that
a deadline-expired login returns an error."

**Interpretation:** the server's write window must outlast the sequence of outbound calls a login
makes, and the sequence must have a bound of its own that leaves room for the error page.

**Inferred intent:** a slow PDS or auth server produces an error the user sees, never an orphaned
session behind a dropped connection.

### What I did

`/cmd/app/main.go` sets the server's `WriteTimeout` to 30 seconds. In `/service/auth.go` the
package variable `loginTimeout` is 20 seconds, with a comment tying it to the write timeout, and
`StartLogin`, `FinishLogin` and `Logout` wrap their context with it. The deferred delete of the OAuth
session after a failed callback already used `context.WithoutCancel`, so it runs after the deadline
as well. `/service/auth_internal_test.go` lowers `loginTimeout` to 100ms, points a login at the fake
network with its new `Stall` knob, which holds every request until the client gives up, and asserts
`model.ErrorAuthServerUnavailable` wrapping `context.DeadlineExceeded`, promptly.

### Why

Indigo's client allows 30 seconds per request and a login makes three to four in a row, so the
default 10 second write timeout could expire mid-handler with the session already persisted.

### What worked

A `var` for the timeout keeps the override in an internal test and out of configuration.

### What didn't work

Nothing failed in this step.

### What I learned

The fake network needed a way to stall, which is one `Stall` field and a wait on the request context;
`testing/synctest` does not fit a test that crosses a real TLS listener.

### What was tricky

Nothing in particular.

### What warrants review

The three `context.WithTimeout` wraps and the comment on `loginTimeout` in `/service/auth.go`.

### Future work

None beyond what earlier steps list.

## Step 11: browser tests against the local network, in `make test` and CI

**Author:** oauth-login-builder

### Prompt Context

**Verbatim prompt:** "New scope on PR #12, agreed with Markus: replace the manual playwright check
with an automated browser integration test against the docker-compose local network, and wire it
into `make test` and CI. ... Use `github.com/chromedp/chromedp` ... Gate like glue's `s3test`: a
helper in `atprototest` (e.g. `atprototest.LocalNetwork(t)`) that calls `t.SkipNow()` when
`testing.Short()` ... Start the real app in-process for the test ... Create a fresh PDS account per
test run ... Test 1, happy path ... Test 2, deny ... Makefile: `test: test-up` ... `.github/workflows/ci.yml`:
... pass `compose: true`."

**Interpretation:** the manual end-to-end runs of the earlier steps become a test that CI runs on
every push, with the compose stack as a test dependency, and the fast suite unchanged in `-short`
mode.

**Inferred intent:** the real PDS, not the fakes, is the arbiter of whether login works, and it is
checked without anyone remembering to.

### What I did

`/boot/boot.go` is the app's wiring, factored out of `main`: `boot.Start(ctx, log, eg, boot.Options)`
with one option per configuration value; `/cmd/app/main.go` reads the environment into the options
and calls it. `/atprototest/local.go` has `LocalNetwork(t)`, which skips in short mode, copies the
proxy's root certificate out of the compose stack with `docker compose cp` when `data/caddy-root.crt`
is missing, checks the PDS and PLC health endpoints and fails with "run make test-up" otherwise, and
offers `CreateAccount` (a fresh `test-<random>.test` account per call) and `GetRecord`.

`/integrationtest/login_test.go` starts the app in-process on a free loopback port with a temporary
database and the local-network options, opens a headless Chrome with `chromedp` (`CHROME_PATH`, else
the platform's default install path; `DefaultExecAllocatorOptions` plus `ExecPath` and
`IgnoreCertErrors`, since the proxy's certificate is self-issued), and drives the login: `/login`,
the handle, the PDS sign-in page, Authorize, the profile link, `/profile` with `@handle`, the profile
record on the PDS, Log out, and an empty `oauth_sessions`. The second test denies consent and
checks the cancelled message, that `/profile` redirects to `/login`, and that neither
`oauth_sessions` nor `oauth_auth_requests` has a row. Every navigation waits on an element of the
destination page rather than on the location. On a failed step a screenshot goes to the test's
temporary directory and its path is logged. The logout button got `id="logout"`.

For the deny assertion, `FinishLogin` now deletes the auth request when the token exchange fails
(the code, if any, was single use), which the fake-network denial test also asserts. In local mode
the atproto HTTP client dials `.localhost` hosts on loopback as well as the handle suffix, so the app
does not depend on the operating system's resolver for `pds.localhost`.

Makefile: `test` depends on `test-up` (`docker compose up --wait --wait-timeout 300`), `test-down`
stops the stack, `atproto-clean` also wipes the volumes, `atproto-ca` and `atproto-account` stay.
CI passes `compose: true` to the shared test workflow, which brings the stack up and runs the tests
without `-short`, so the browser tests run there too; the runner has Chrome and Docker.

From a clean state (`make test-down`, `make atproto-clean`), `make test` built the PLC image, waited
for the stack, and ran everything including the browser tests; `go test -short ./...` skips them and
the short suite runs in seconds. Timings are in the report.

### Why

The fakes prove the client against a protocol the fakes were taught; the PDS proves it against the
protocol as shipped. `chromedp` drives the Chrome that is already there, so there is no browser
download to keep current and nothing to install on the runner.

### What worked

Both browser tests passed on their first run, in under five seconds together: the PDS UI's
buttons are reachable by their visible text through XPath (`//button[normalize-space()="Sign in"]`,
`"Authorize"`, `"Deny access"`), and the password field is the only `input[type="password"]`, so no
selector depends on the PDS UI's class names. The identifier field arrives prefilled from the login
hint and needs no typing.

### What didn't work

Nothing failed in this step.

### What I learned

The shared CI test workflow does not run `make test`; it runs `docker compose up --wait` itself
when `compose` is true and then `go test -race ./...` with its own tags, so the test helper, not the
Makefile, must be what copies the certificate out of the stack.

### What was tricky

Chrome resolves `*.localhost` on its own, but Go does not everywhere, which is why the local client
dials `.localhost` on loopback too.

### What warrants review

`/boot/boot.go` against the old `main`, the waits in `/integrationtest/login_test.go`, and the
`test`/`test-up` targets in the Makefile.

### Future work

More browser tests as pages arrive: profile editing, uploads.

## Step 12: indigo confined to the `atproto` package

**Author:** oauth-login-builder

### Prompt Context

**Verbatim prompt:** "Fifth review batch on PR #12, all triaged with Markus. One structural item plus
small ones; apply all ... 1. Confine indigo to the `atproto` package (the big one). Goal: `sqlite` and
`service` import nothing from `github.com/bluesky-social/indigo`; only `atproto` (and `atprototest`,
and `lexicons` for the catalog) do. Direction: `atproto -> sqlite` via narrow interfaces, never the
reverse ... 2. Small items ... `GetOrCreateUser` -> `CreateUserIfMissing` ... delete the 8-goroutine
concurrency subtest ... `ProfilePageProps.Handle` becomes `model.Handle` ... `docker-compose.yml`:
`postgres:18-alpine` -> `postgres:18`; remove `PDS_DATA_DIRECTORY`, `PDS_BLOBSTORE_DISK_LOCATION`,
`PDS_BLOB_UPLOAD_LIMIT` (they restate the image defaults)."

**Interpretation:** the SDK becomes an implementation detail of one package, and the rest of the app
speaks in `model` types and `model.Error*` values.

**Inferred intent:** swapping or upgrading the SDK, or adding a second network client, touches one
package; `service` reads as business logic with no protocol types in it.

### What I did

`model` gained `Handle` (with `HandleInvalid`), `AuthFlow`, `OAuthAuthRequest` and `OAuthSession`.
`sqlite/oauth.go` persists the latter two under its own method names (`GetOAuthAuthRequest`,
`SaveOAuthAuthRequest` with the ten-minute sweep, `DeleteOAuthAuthRequest`, `GetOAuthSession`,
`SaveOAuthSession` with the one-year sweep, `DeleteOAuthSession`) and imports only `model` and glue.

`atproto` owns everything that touches the SDK: `store.go` adapts a narrow `Store` interface, which
`*sqlite.Database` satisfies, to the SDK's store, converting DIDs, scopes and nullable fields;
`client.go` exposes `StartAuthFlow` (returning a `model.AuthFlow` with the redirect URL, state, DID,
handle, PDS host and auth server host), `ProcessCallback` (returning the persisted
`model.OAuthSession`, spending the auth request on failure, and putting a denial's code on the span
as `oauth.callback_error`), `CheckScopes`, `ResumeSession`, `Logout`, `ResolveHandle` (returning
`model.HandleInvalid` when unverified), `ClientMetadata` and `JWKS` as `any` for the HTTP layer, and
the accessors `Confidential`, `ClientID`, `CallbackURL`, `RequestedScopes` and `Local`; `session.go`
has the `Session` interface with `GetRecord`, `PutRecordIfMissing` (the null-`swapRecord` write),
`Revoke` and `Delete`. The SDK's error types are translated at this boundary: a callback error is
`model.ErrorLoginCancelled`, a token exchange failure `model.ErrorAuthServerUnavailable`, a missing
session `model.ErrorOAuthSessionNotFound`. The per-outbound-call spans moved with the calls.

`service` wires its operations against interfaces over `*atproto.Client` (`authFlowStarter`,
`callbackProcessor`, `logouter`, `sessionResumer`, `handleResolver`) and a `recordValidator` over the
`lexicons.Catalog`, which now wraps the SDK catalog behind `ValidateRecord(record, nsid)`. `PDSClient`
became `PDSSession`, returning an `atproto.Session`. The service tests are stubs of those
interfaces plus the real SQLite store; the fake-network flow tests moved to `atproto/flow_test.go`,
and the HTTP tests decode the metadata documents into plain structs. `http` imports `atproto` for
the `Session` type only.

The small items: `CreateUserIfMissing`, the concurrency subtest removed, the migration comment
line removed, `ProfilePageProps.Handle` typed, `postgres:18`, `PDS_BLOB_UPLOAD_LIMIT` removed.

`go list` over every package, test imports included, shows indigo imported by `app/atproto`,
`app/atprototest` and `app/lexicons` only.

### Why

The rule is the same as for the database: the rest of the app names what it needs in its own words,
and one package translates. That is what let the service tests become stubs without a fake network.

### What worked

The adapter is mechanical, and the SDK returns the store's own errors unchanged, so
`model.ErrorOAuthSessionNotFound` crosses the SDK and comes back out to `service` without a special
case.

### What didn't work

- Removing `PDS_DATA_DIRECTORY` and `PDS_BLOBSTORE_DISK_LOCATION` from the compose file, as the
  review asked, stops the PDS: `PDS failed to start: Error: Must configure either S3 or disk blobstore`
  (the image sets only `PDS_PORT`), and the browser tests then saw `502 Bad Gateway` from the proxy.
  Both are back; only `PDS_BLOB_UPLOAD_LIMIT` was a restated default.
- A scope check test assumed a multi-value scope (`blob?accept=audio/*&accept=image/*`) compares
  equal to the two single-value scopes; parsed permissions render back in the form they came in,
  so the check compares one requested scope at a time, and the test now uses single-value forms.
- The proxy answered 502 for a few seconds after `docker compose up --wait` reported the stack
  healthy, so `atprototest.LocalNetwork` now retries the health checks for up to 30 seconds.

### What I learned

`go list -f '{{join .Imports "\n"}}{{join .TestImports "\n"}}{{join .XTestImports "\n"}}'` is the
check that catches an import hiding in a test file, which a grep over non-test files would miss.

### What was tricky

Where the `Session` type lives: `service` and `http` need to name it in their interfaces, so it is an
interface in `atproto` that stubs can implement, rather than a struct.

### What warrants review

`/atproto/store.go` conversions, the error translation in `/atproto/client.go`, and the service
stubs in `/service/auth_test.go`.

### Future work

`Session` grows generic `Get`/`Post` calls when a feature needs an endpoint other than records.

## Step 13: valid example DIDs, no store sweeps, and a healthcheck chain

**Author:** oauth-review-finisher

### Prompt Context

**Verbatim prompt:** the review comments behind this batch: "Is that a thing we made up or an official
thing?" (on `HandleInvalid`), "These should go in auth.go ?", "Put in atproto.go", "Add the DID of
audioadastra.com as the example. (At least, it should be a valid DID).)", "Make sure all example DIDs
are valid", "Is this a correctness thing or a cleanup? If cleanup, skip.", "Also skip this for same
reason as above.", "This PR has been open a while, check if version still current", "can we do any
health check for the plc that the pds can usage, like between plc and plc-db?".

**Interpretation:** sort the `model` types by what they are, make every example DID one the protocol
would accept, take out the two deletes in the store that were housekeeping, bring the PLC pin up to
date, and make the compose stack report healthy only when it is.

**Inferred intent:** a reader should not have to wonder whether a value is real or invented, the
store should do only what a login needs, and "the network is up" should be one fact that compose
knows rather than something the test helper polls for.

### What I did

This step was done by two builders; the first made the model and store commits, and I verified them
against the review and finished the local network.

`/model/atproto.go` now holds only protocol vocabulary: `CollectionActorProfile`, `DID` and `Handle`
with `HandleInvalid`, whose comment says it is the handle the atproto spec reserves rather than one
of ours. `/model/auth.go` holds `UserID`, `User`, `AuthFlow`, `OAuthAuthRequest` and `OAuthSession`.
The `DID` comment's example is `did:plc:xj4bpglaht36jqc4dopoh3va`.

Every example DID in Go code and fixtures is well-formed: `did:plc:` followed by exactly 24
characters of `a-z2-7`. The values are `did:plc:alicealicealicealicealic`,
`did:plc:bobbobbobbobbobbobbobbob`, `did:plc:nobodynobodynobodynobody` and
`did:plc:adminadminadminadminadmi`, as test constants where a file repeats one. The `sqlite` tests
have their own constant, so that package still depends on nothing that imports the SDK.
`/atproto/examples_test.go` walks the repository's `.go`, `.sql` and `.json` files, skipping
dot-directories, `tailwind-plus*` and `data`, and requires each occurrence to parse with the SDK's
`syntax.ParseDID` and to match `^did:plc:[a-z2-7]{24}$`. It needs no network and runs under `-short`.

`sqlite.SaveOAuthAuthRequest` is a single insert and `sqlite.SaveOAuthSession` a single upsert. The
deletes of auth requests older than ten minutes and sessions untouched for a year are gone, with
their tests and with `oauth_auth_requests_created_idx`, which existed only for the first of them.

In `/docker-compose.yml` the PLC build pin moved from `996e23b5` to `9c8ea2fe`, the head of the
directory's repository; only a README and a website template differ between the two, the server is
the same. The services now start in a chain of healthchecks: `plc-db` (already `pg_isready`), then
`plc` on `http://127.0.0.1:2582/_health`, then `pds` on `http://127.0.0.1:3000/xrpc/_health`, then
`caddy` on `https://pds.localhost/xrpc/_health`, each `depends_on` the one before with
`condition: service_healthy`. Each check runs every two seconds for up to 150 tries, and `caddy` has an `extra_hosts` entry mapping `pds.localhost` to `127.0.0.1`. `atprototest.LocalNetwork` in `/atprototest/local.go` lost its
30-second retry loop and checks each health endpoint once.

### Why

The sweeps were cleanup, not correctness. A stale auth request cannot finish a login, because the
auth server expires the pushed request on its side, and nothing reads a session by its age. A write
path that also deletes unrelated rows inside a transaction is more than the login needs, and the
deleting belongs in a job.

The retry in the helper was covering for compose: `up --wait` returned when the containers were
running, not when the PDS answered through the proxy. With Caddy's healthcheck fetching the PDS
health endpoint through Caddy itself, `up --wait` returning means exactly what the helper used to
poll for.

### What worked

All three images have busybox `wget` (v1.37.0), so one style of check serves them all; the PLC and
PDS images also have `node`, and the Caddy image `curl`, but none was needed. Busybox `wget` in the Caddy container accepts
`--no-check-certificate`, which the check needs because the certificate comes from Caddy's own local
CA. `wget` exits 1 on a refused connection and on an HTTP error status, so the checks do discriminate.

From wiped volumes (`make atproto-clean`), `make test` passed three times in a row with the single
check, and a fourth run the way CI does it (`docker compose up --wait --wait-timeout 300`, then
`go test -race -count=1 ./...`) passed too. The stack takes about ten seconds to report healthy. The
migrations still round-trip with `pragma foreign_keys = 1`: all up, this feature's down, up again.

### What didn't work

Nothing failed outright, but the self-review found one thing that only looked proven. In the Caddy
container `pds.localhost` resolved to `127.0.0.1` without any configuration, yet the container's
`/etc/hosts` has no such entry: the answer came from Docker's embedded DNS forwarding to the host,
which on this machine answers for `.localhost` names. A CI runner's resolver need not, and the check
would then fail with a bad address and take `up --wait` down with it. The `extra_hosts` entry makes
the name resolve inside the container whatever the host does.

The same review (two reviewers, both of whom raised these) found that with 30 retries a service
had a hard 60 seconds to answer before being marked unhealthy, which makes the 300-second wait
timeout moot on a slow runner, and that the migration comment on `oauth_auth_requests` said a
row lives "until the flow finishes", which promised more than a store without sweeps delivers. Both
are fixed. For the first I tried a `start_period: 60s`, and the stack then took about 30 seconds to
report healthy instead of 10, because Docker probes only every five seconds during the start period
unless `start_interval` says otherwise; 150 retries gives the same patience as the wait timeout with
no such cost. Both reviewers also noted that nothing deletes stale rows any more; that is the decision
of this batch and is under future work. One noted that `/atproto/examples_test.go` has no matching
source file; it lives in `atproto` because that is the package allowed to import the SDK, and I left
it.

### What I learned

`docker compose up` does not rebuild an image when the build context's pinned commit changes; it
reuses the image with that name. After a pin bump, `docker compose build plc` is what picks it up
locally. CI starts from nothing, so it always builds the pinned commit.

`docker inspect --format '{{json .State.Health}}' <container>` shows the last five check runs with
exit codes and output, which is the quickest way to see why a service stays `starting`.

### What was tricky

Caddy's check has to go through the proxy to be worth anything: a check on Caddy's admin endpoint
would go healthy while the upstream still answered 502, which is the gap the retry was papering over.

### What warrants review

`/docker-compose.yml`: the three healthchecks, the `depends_on` conditions and the `extra_hosts`
entry, whose worth only a Linux runner shows. To validate:
`make atproto-clean && make test`, and `docker compose ps` should show all four services `(healthy)`.
`/atprototest/local.go`: the helper now fails at once when the stack is not up, with
"run make test-up".

### Future work

Abandoned auth requests, and sessions of devices that never log out, now accumulate; a periodic job
should delete them. `POST /login` is unauthenticated and creates a row per attempt, so it wants rate
limiting.

## Step 14: external review: sessions that could be left behind, and the helper's resolver

**Author:** oauth-review-finisher

### Prompt Context

**Verbatim prompt:** "Three findings from an external review of PR #12 at `23080e8`. I checked each
against the code and all three hold. ... 1. **Logout can leave the OAuth session row behind**
(`atproto/client.go`, `Client.Logout`) ... Fix: delete with a context detached from the deadline
(`context.WithoutCancel(ctx)`) ... 2. **A failed login can orphan the session it just created**
(`service/auth.go`, the `FinishLogin` closure) ... Fix: register the cleanup immediately after
`ProcessCallback` succeeds, deleting by DID and session ID without needing a resumed session and with
a detached context ... 3. **The test helper depends on the OS resolving `pds.localhost`**
(`atprototest/local.go`) ... Make the helper do the same, preferably by reusing that function".

**Interpretation:** two paths on which a deadline or a cancelled request left an `oauth_sessions` row
with its tokens and no one holding its ID, and one test helper that worked only where the system
resolver makes up answers for `.localhost` names.

**Inferred intent:** every session row must have an owner or be gone. That mattered less while the
store deleted year-old sessions on its own; since step 13 removed the sweeps, a row left behind stays
for good.

### What I did

`atproto.Client.DeleteSession` deletes a session from the store by DID and session ID without
revoking anything. `atproto.Client.Logout` in `/atproto/client.go` now ends with that delete under
`context.WithoutCancel`, so a revocation that uses up the deadline no longer takes the delete with
it. Resuming the session is only needed to revoke: if it fails for a reason other than the session
not existing (a context that is already done, in practice), the revocation is skipped, `oauth.revoked`
is false on the span, and the delete still runs. `Session.Delete` had no users left and is gone from
the `atproto.Session` interface. `Store.DeleteOAuthSession` now says that a missing session is not an
error, which `DeleteSession` relies on.

In `/service/auth.go`, the `FinishLogin` cleanup is registered directly after `ProcessCallback`
returns and calls `DeleteSession` on the `callbackProcessor` interface with a detached context, so
it no longer needs a resumed session. A failing `ResumeSession` is now one of the failures it covers.

`newLocalHTTPClient` became `atproto.NewLocalHTTPClient`, and `atprototest.LocalNetwork` builds its
client with it. The health checks, `CreateAccount` and `GetRecord` all go through that client, so
`pds.localhost` is dialed on loopback by the client itself; the PLC address is plain `localhost`,
which every system resolves. The browser in the integration tests resolves `.localhost` on its own.

Tests: in `/atproto/flow_test.go`, a logout against a stalled auth server with a 100 ms deadline and
a logout with an already cancelled context both end with no rows in `oauth_sessions`, plus tests of
`DeleteSession`. In `/service/auth_test.go`, a stub whose `ResumeSession` fails after the callback
succeeded, asserting the session was deleted, once with the caller's context already cancelled and
the delete's context still live. In `/atproto/client_test.go`, `NewLocalHTTPClient` reaches a local
server under a `.test` and a `.localhost` name and the request keeps the name it was made for.

### Why

The HTTP logout handler destroys the cookie session whatever the OAuth logout returns, and a failed
login never hands the session ID to anyone. In both cases the row is unreachable afterwards, so the
delete has to happen in the same call and must not share a deadline with the slow step before it.

### What worked

Putting the delete temporarily back on the caller's context makes both new logout tests fail with
`deleting OAuth session: context deadline exceeded` and `context canceled`, so they test the fix.

From `make atproto-clean`, `make test` passes including the browser tests through the new helper
client; `go test -short ./...` and `make lint` pass.

### What didn't work

The first run of the stalled-revocation test hung until the test binary's timeout:

`httptest.Server blocked in Close after 5 seconds, waiting for connections: *tls.Conn ... in state active`

`Logout` had returned; the fake server's stalled handler had not. The `Stall` knob waits on the
request context, and `net/http` only notices a client going away once the request body has been
read, which for a revocation `POST` never happened. `atprototest.Network` now drains the body before
waiting. Nothing had used `Stall` with a `POST` before.

### What I learned

A handler blocked on `r.Context().Done()` with an unread request body is never woken by the client
hanging up. For a `GET` there is no body, so the same handler works, which hides the problem.

### What was tricky

What `Logout` should return when the context is done before it starts. Resuming fails with the
context error, which says nothing about whether the session exists, so the session is deleted
unseen and a missing one is not reported. The doc comment says so.

### What warrants review

`/atproto/client.go` `Logout`: the three-way switch on the resume error. `/service/auth.go`: the
order of the cleanup and `ResumeSession`. The self-review (two reviewers) found no path in these
that still leaves a row. Both noted that a delete under `context.WithoutCancel` has no deadline of
its own; that matches the existing delete of the auth request in `ProcessCallback` and I left it.

### Future work

One reviewer pointed at a remaining path outside this change: `/http/login.go` stores the session ID
in the cookie session after `FinishLogin` succeeds, and if saving the cookie session fails, the
OAuth session survives with its ID nowhere. The periodic job from step 13 would cover it; so would
deleting the session when the save fails.

## Step 15: `service.Fat` between `http` and the network, and a synctest timeout test

**Author:** oauth-review-finisher

### Prompt Context

**Verbatim prompt:** the review comments of the seventh batch: "inline" and "Not inlined yet?" (a
misread: GitHub re-anchored the thread to the public delegate after the package-level function was
deleted; no change), "Way too elaborate. Drop the test.", "Isn't it a bit weird that the service
returns something from the atproto package? Leaking implementation details? `model` package?",
Markus's reply "Oh, it's an interface? Yeah, let's try your approach. I want the service.Fat to be
at the center of every interaction", and "Ugh, this is ugly. Don't rely on global state." Added to
the batch afterwards: route the client metadata and the JWKS through `service.Fat` too.

**Interpretation:** drop the DID guard test; take `atproto.Session` off the `service` and `http`
boundary so they speak only `model` types; let `http` reach the network only through `service.Fat`;
and test the login timeout without swapping a package variable.

**Inferred intent:** `service.Fat` is the one door from the web layer to everything else, and `boot`
is the only place that knows the network client is an `atproto.Client`.

### What I did

`/atproto/examples_test.go` is gone.

`atproto.Session` is gone. `atproto.Client` gained `GetRecord` and `PutRecordIfMissing`, keyed by DID
and OAuth session ID, and `CheckSession`; each resumes the session internally (a store read and a key
parse, no server call), and the record calls keep their outbound spans. `ResumeSession` became the
unexported `resumeSession`. The client keeps its own base URL, so `ClientMetadata()` takes no
argument.

In `/service`, `Fat.PDSSession` and its wiring function became `Fat.CheckOAuthSession`, which
returns `model.ErrorOAuthSessionNotFound` when the session no longer exists. `ensureProfile` calls the
record methods with the new session's DID and ID through a `recordGetPutter`, which `FinishLogin` now
takes as its own parameter. `Fat.OAuthClientMetadata` and `Fat.OAuthJWKS` are new operations over an
`oauthDocumenter`. `Setup` takes an unexported `networkClient` interface composed of the operations'
interfaces instead of `*atproto.Client`, so `service` no longer imports `app/atproto`; the reflective
wiring test in `/service/fat_internal_test.go` passes a struct that embeds that interface.

In `/http`, the middleware in `/http/auth.go` that destroys a cookie session whose OAuth session is
gone calls `CheckOAuthSession`, `OAuthMetadata` takes the service, and `InjectHTTPRouter(log, svc)`
lost its client and base URL parameters. `boot` hands the client to `service.Setup` only.

`loginTimeout` is a `const` again and `/service/auth_internal_test.go` is gone. The timeout test is
in `/service/auth_test.go`: inside `synctest.Test`, a flow starter that blocks on `<-ctx.Done()`,
then `errors.Is(err, context.DeadlineExceeded)` and `time.Since(start) == 20*time.Second`.

`go list -f '{{.ImportPath}}: {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}' ./...`
grepped for `app/atproto` lists `app/atproto` (its external tests), `app/atprototest` and `app/boot`.

### Why

`service` returning an `atproto.Session` put a type from the network package into the business
layer and into `http`, which then held a second way onto the network next to the service. With the
calls keyed by session ID, `service` and `http` pass around only `model` types and strings, and
every web request that touches the network goes through one place.

### What worked

The synctest test takes no measurable real time (0.00s) and the exact equality holds: the bubble's
clock only moves when every goroutine in it is blocked, and then straight to the next timer, which
here is the timeout's.

### What didn't work

Nothing failed outright. The trailing-slash metadata test in `/http/login_test.go` built an
`atproto.Client` with `atproto.New`, which no longer fits a package that may not import `atproto`.
The behaviour it covered is now the client's, so the test moved to `/atproto/client_test.go`, and
`atprototest.PrivateKeyMultibase`, whose only user it was, went with it.

### What I learned

An exported function can take an unexported interface type; the composition root still passes the
concrete client, and a missing method fails at compile time naming it. For a test that only needs
something satisfying the interface, a struct embedding the interface is enough.

### What was tricky

Resuming per call changes two things, both reviewed and accepted. First, a session that cannot be
resumed after the token exchange now fails in `ensureProfile`, after the user row is created, so that
case no longer leaves zero users; it still deletes the OAuth session. Second, two overlapping calls
as the same session resume two copies of it and could clobber refreshed tokens; the record methods'
doc comments say calls as the same session must not overlap. Nothing overlaps them today.

### What warrants review

`/service/fat.go` `Setup` and `networkClient`; `/service/auth.go` `FinishLogin` and
`CheckOAuthSession`; `/http/auth.go` the middleware; `/atproto/client.go` the record methods and
`ClientMetadata`. Self-review by two reviewers found no behaviour regression; their comment and
coverage nits are applied.

### Future work

The 2026-09-21 entry in `/docs/decisions.md` still names `atproto.Session` as the handle on a
logged-in account's PDS. It is an earlier entry, so it is not edited here.

## Step 16: concrete client in `Setup`, and `model.LoginStart`

**Author:** oauth-review-finisher

### Prompt Context

**Verbatim prompt:** the review comments of the eighth batch: "The service always gets the concrete
implementation injected here.", "What is LoginStart? Exposes the service package to callers, no good.
They use private interfaces for methods and don't know about the service package.", and "This import
shouldn't be here. Figure out why."

**Interpretation:** `Setup` names its capabilities concretely, the client included; the start of a
login is a `model` type rather than a `service` one; and `http/login.go` then has no reason to import
`service`.

**Inferred intent:** callers of the service know only `model` types and their own private
interfaces; the composition root is the one place where concrete types meet.

### What I did

`service.Setup` takes `*atproto.Client` again and its doc comment is back to the wording on `main`.
The `networkClient` interface from step 15 is gone; the narrow per-operation interfaces stay on the
wiring functions, and no operation signature mentions an `atproto` type. `/service/fat.go` imports
`app/atproto` for this alone. The reflective wiring test passes `&atproto.Client{}`, an empty value
like the other capabilities there.

`service.LoginStart` is gone. `model.LoginStart` in `/model/auth.go` holds `RedirectURL` and `State`
and is embedded in `model.AuthFlow`, so `atproto.Client.StartAuthFlow` still fills both through the
promoted fields. `Fat.StartLogin` records the DID, handle and hosts on the span as before and returns
`flow.LoginStart`, or an empty one on error. The private `loginStarter` in `/http/login.go` returns
`model.LoginStart`, and that file no longer imports `app/service`; among the non-test files in `http`,
only `/http/routes.go` does, for `*service.Fat` in `InjectHTTPRouter`.

### Why

Step 15 composed an interface for `Setup` alone so that `service` would not import `atproto`. That
added a second abstraction over the client next to the wiring functions' own, where `Setup`'s job is
to name the real capabilities. The import was never the leak; types in operation signatures were.

### What worked

The embedding needed no change in `atproto`: assignments through promoted fields keep working.

### What didn't work

Nothing failed.

### What I learned

The `import "app/service"` in `/http/login.go` was a symptom: one private interface returned a
service type, and the import followed it.

### What was tricky

The wiring functions' `== nil` checks only catch an untyped nil interface. Through `Setup`, a nil
`*atproto.Client` becomes a non-nil interface and would fail on first use instead of at wiring. That
already held for `*sqlite.Database` and `*lexicons.Catalog`, and `boot` cannot hand over a nil client
because `atproto.New` returns an error instead, so I left it. Both reviewers raised it as minor.

### What warrants review

`/model/auth.go` `LoginStart` and `AuthFlow`, `/service/auth.go` the `StartLogin` closure, and
`/service/fat.go` `Setup`.

### Future work

None from this step.

## Step 17: types for OAuth state, session IDs, callbacks and URLs

**Author:** oauth-review-finisher

### Prompt Context

**Verbatim prompt:** the review comments of the ninth batch: "It's a bit weird to have url.values at
this layer. Also, should we have types for `state` and `sessionID`?", "I don't like `any` here. A
struct? Or map?" (on `oauthClientMetadata func() any`; decided: keep `any`, since a `model` struct
would duplicate the SDK's field list and pre-encoded bytes were not wanted), and, added to the batch,
"user proper URL types?" on `model.OAuthAuthRequest.AuthServerURL`.

**Interpretation:** name the two identifiers that travel through every layer, replace the raw query
below `http` with a struct, and hold URLs as `*url.URL` in the model.

**Inferred intent:** a signature should say what it carries; a state cannot be passed where a session
ID belongs, and nothing below the web layer knows the callback arrived as a query string.

### What I did

`model.OAuthState` and `model.OAuthSessionID` in `/model/auth.go`, next to the other login types
rather than in `/model/atproto.go`, since they are this app's OAuth client identifiers rather than
protocol vocabulary. They follow `model.DID`: doc comment, `String()`, and the `fmt.Stringer` check.
They replace plain strings in `service` operations and `Fat` fields, the `atproto.Client` methods,
`model.LoginStart.State`, `model.OAuthAuthRequest.State`, `model.OAuthSession.SessionID`, the `sqlite`
store methods, and `http`, which converts at the cookie session.

`model.OAuthCallback` carries state, code, issuer, error, error description and error URI. The
callback handler in `/http/login.go` builds it from the query, and `atproto.Client.ProcessCallback`
rebuilds the `url.Values` the SDK's `ClientApp.ProcessCallback` takes. That function reads six
parameters: `state`, `error`, `error_description`, `error_uri`, `iss` and `code`. The brief listed
five, so `ErrorURI` was added to carry the sixth.

The URL fields of `model.OAuthAuthRequest` and `model.OAuthSession` are `*url.URL`. The auth server
URL, token endpoint and host URL are never nil; the revocation endpoint is nil when the auth server
has none. `atproto` parses the SDK's strings and formats them back, and `sqlite` does the same for its
text columns; both reject a value that is not an absolute URL with a scheme and a host, and both map
an empty revocation endpoint to nil and back. `RequestURI` (an opaque identifier), the callback's
`Issuer` (compared byte for byte) and the hostnames on `model.AuthFlow` stay strings. `service` reads
`.Host` for its span attributes, so its `hostOf` and its `net/url` import are gone.

### Why

The callback check compares the issuer string against the stored auth server URL, so that URL must
come back from `url.Parse` and `String()` byte for byte. A test in `/sqlite/oauth_test.go` pins it on
the store path for `https://host`, `https://host/`, `https://host:1234`, `https://host/path` and
`http://127.0.0.1:2583`; all five round-trip unchanged.

### What worked

Passing the typed IDs as query arguments needs nothing in `sqlite`: `database/sql` converts named
string kinds, as it already did for `model.DID`.

### What didn't work

Nothing failed. The first draft parsed every URL leniently, so an empty or schemeless required URL
became nil, or a URL without a host, instead of an error; both reviewers flagged the nil dereference
it allowed in `service`, and the parsers now reject both.

### What I learned

`url.Parse` accepts almost anything: `auth.test`, `/oauth/token` and `urn:...` all parse. "Does not
parse" is only a useful error with a check for a scheme and a host on top.

### What was tricky

The denial test now also checks the error description and error URI, via the SDK's error text,
because a typo in either parameter name would otherwise pass every test. The fake auth server sends
an `error_uri` for that, and a new `http` test for denied consent checks that the auth request is
spent, which a dropped `error` parameter would not do.

### What warrants review

`/atproto/client.go` `ProcessCallback` and `callbackParams`; `/atproto/store.go` and `/sqlite/oauth.go`
the URL parsers; `/model/auth.go` the new types and the nil contract on the URL fields.

### Future work

A failure to save the session inside the SDK's callback processing is classified as the auth server
being unavailable, local errors included; that predates this step.
