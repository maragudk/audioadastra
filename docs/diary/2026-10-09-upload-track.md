# Diary: upload a track (#34)

The first slice of milestone 1 (#6): a logged-in musician uploads an audio file with a title and
optional description; it is published as a `com.audioadastra.track` record in their own repo and
listed on their profile page. Folds in #32 (login blob scope). Transcoding (#10), playback (#11),
edit and delete (#35) and firehose indexing (#9) are separate.

## Step 1: requirements from the brainstorm

**Author:** main

### Prompt Context

**Verbatim prompt:** "Yes, and make an issue for it" (to "Shall we brainstorm the upload feature?")

**Interpretation:** create the missing upload issue under #6 and refine it into requirements with
Markus, one question at a time.

**Inferred intent:** get from a published lexicon to tracks actually landing in users' repos, in
the smallest slice that proves the write path end to end.

### What I did

Created #34 (sub-issue of #6) and #35 (edit and delete, deferred), and commented the blob scope
decision on #32. Checked `file-type` v22 (the reference PDS's MIME detection library) source on
unpkg for how audio is labelled, and the project's Datastar build for upload progress support.
Decisions with Markus:

- **Slice:** upload only. No transcoding or playback; the track shows in a plain list.
- **Validation:** `ffprobe` on the server; ffmpeg/ffprobe become a runtime dependency (Docker
  image and CI). Run as a separate process.
- **No copy kept:** the upload passes through a temp file to the PDS and is deleted. #10 will
  fetch originals from the PDS with `com.atproto.sync.getBlob`, the same path as indexed tracks.
- **Size limits:** a configurable server cap (default 300 MiB) plus the user's PDS
  `blobUploadLimit` from `describeServer` on their PDS host, shown on the upload page and checked
  in the browser and on the server. If the PDS reports no limit, show none and pass on the PDS's
  rejection. `describeServer` results cached in memory per PDS host for 24 hours, failures cached
  briefly. Aggressive, call-appropriate timeouts on all PDS calls: short for `describeServer` and
  `createRecord`, an idle timeout for `uploadBlob`.
- **Where it shows:** a plain list (title, upload date, newest first) on the user's profile page.
  No player.
- **Edit and delete:** deferred to #35, but the database keys tracks by AT URI and stores the
  record CID so `putRecord` edits and firehose updates fit later.
- **Progress:** Datastar's actions use `fetch`, which has no upload progress events (the project's
  `datastar.js` does multipart but nothing tracks progress). A small `XMLHttpRequest` script sends
  the form and writes progress into a Datastar signal; Datastar renders the bar, then
  "Publishing…", then the result.
- **Scopes:** add `repo:com.audioadastra.track` and keep `blob:audio/*`. No `blob:video/*`: in
  `file-type` v22 only MP4-family audio without the `M4A ` brand (`video/mp4`), WebM
  (`video/webm`) and Matroska (`video/matroska`) are labelled as video; Apple M4A is
  `audio/x-m4a`, Ogg with Opus/Vorbis/FLAC/Speex is `audio/ogg`, and only unknown-codec Ogg is
  `application/ogg`. Those uploads get a clear error asking for WAV, FLAC or MP3. No handling for
  existing sessions: nobody uses the app yet.
- **Title:** typed by the user, no pre-fill.
- **Temp files:** `TEMP_DATA_DIR` (default: system temp dir), uploads in its `uploads/`
  subdirectory, deleted after every upload, and that subdirectory emptied at startup. Production
  mounts a host directory there (Markus's compose, not in this repo). Not tmpfs.
- **Read your own writes:** after `createRecord`, insert the track into our database at once;
  idempotent by AT URI and CID so #9 later changes nothing for our own tracks.
- **Telemetry:** the upload request span as a wide event (size, ffprobe format/duration/channels/
  sample rate, declared MIME type, PDS host, DID, AT URI, outcome, failed step, important
  durations as attributes). Child spans only for outbound PDS calls (`describeServer`,
  `uploadBlob`, `createRecord`); cache hit/miss as an attribute. No new logs except one startup
  line when stale temp uploads were removed.
- **Testing:** a chromedp browser test against the local PDS (upload a short committed audio file,
  see it on the profile, record and blob exist on the PDS; and a non-audio file rejected with the
  title kept). Unit tests with the existing fakes for size limits, PDS failures, idempotent
  insert, temp cleanup and the cache.

### Why

Every choice aims at the smallest slice that proves the write path, without closing doors for
#9, #10, #11 and #35.

### What worked

Reading `file-type` v22's source settled the scope question with facts instead of guesses.

### What didn't work

Nothing failed at this stage; it was discussion and source reading.

### What I learned

The blob scope may be checked against the PDS-detected MIME type rather than the declared one;
that is not verified yet and the builder should find out.

### What was tricky

Keeping the slice small: edit and delete, transcoding and playback all pulled at it.

### What warrants review

That nothing in this slice blocks #35 (edit/delete) or #9 (indexing).

### Future work

#10, #11, #35, #9.

## Step 2: build the upload, end to end

**Author:** builder

### Prompt Context

**Verbatim prompt:** "Build \"Upload a track\" (GitHub issue #34, also closing #32) for Audio Ad Astra, an open-source Go web app for sharing music on atproto. Open a PR when done.

Worktree: /Users/maragubot/Developer/audioadastra/.claude/worktrees/upload-track (branch `worktree-upload-track`). Read first:
- /docs/diary/2026-10-09-upload-track.md (my Step 1: the full requirements and their reasons; add your own steps via the diary skill)
- /docs/decisions.md (especially atproto OAuth login, browser tests against a real local PDS via docker compose, OpenTelemetry traces as primary telemetry, the 2026-10-09 track audio decision)
- /lexicons/com/audioadastra/track.json (the published record schema)
- /PRODUCT.md and /DESIGN.md (product and visual system; new surfaces start from these)
- recent diaries in /docs/diary/ for login and OAuth (2026-09-14-atproto-oauth-login.md etc.)

Use these skills: go, gomponents, datastar, atproto, observability, sql, git. Follow CLAUDE.md/AGENTS.md (app runs with `make watch`, logs in app.log, DB is app.db; use `pragma foreign_keys = 1;`). Dev and tests must never write to the real atproto network: use the local PDS from docker compose (`make test-up`) and the existing fakes in `atprototest`.

## Requirements

Scope: a logged-in user uploads an audio file with a title (required) and description (optional). It is published as a `com.audioadastra.track` record in their repo, with the file as `audio.original`, and listed on their profile page. Out of scope: transcoding, playback, edit/delete, firehose indexing.

1. Login scopes: add `repo:com.audioadastra.track`. Keep `blob:audio/*`; do NOT add `blob:video/*`. No special handling for existing sessions.
2. Profile page links to a new upload page (follow DESIGN.md).
3. Upload page shows the user's PDS blob limit from `com.atproto.server.describeServer` on their PDS host (`blobUploadLimit`, optional field; if absent, show no limit). Cache describeServer results in memory per PDS host for 24 hours; cache failures briefly too, so an unreachable PDS isn't hit on every page load.
4. Form: file, title (required; no pre-fill), description (optional). Browser blocks files over the shown PDS limit before uploading. Field limits match the lexicon (title 1..300 graphemes / 3000 bytes, description ≤5000 graphemes / 20000 bytes), enforced server-side.
5. Progress: Datastar actions use fetch, which has no upload-progress events. Use a small XMLHttpRequest script that posts the form and writes upload progress into a Datastar signal; Datastar renders a progress bar, then \"Publishing…\", then redirects to the profile on success. Keep the script small and plain.
6. Server pipeline:
   a. Stream the request body to a temp file (never hold it in memory). A configurable server cap (default 300 MiB) aborts larger requests early; also reject early when over the cached PDS limit if known.
   b. Run `ffprobe` (separate process) to confirm it decodes as audio; capture format, duration, channels, sample rate.
   c. `uploadBlob` to the user's PDS, declaring the MIME type derived from what ffprobe found.
   d. `createRecord` for `com.audioadastra.track` with `audio.original`, `title`, `description`, `createdAt` (record creation time). Validate the record against the lexicon before writing (the repo has lexicon validation in /lexicons).
   e. Delete the temp file in every outcome.
   f. Timeouts: aggressive and call-appropriate on every PDS call — short for describeServer and createRecord; an idle (no-progress) timeout rather than a total deadline for uploadBlob.
7. Temp files: new config `TEMP_DATA_DIR` (default: the system temp dir). Uploads live in its `uploads/` subdirectory; empty that subdirectory at startup (log one line only if it removed something). Document the variable wherever config is documented.
8. Read your own writes: right after createRecord succeeds, insert the track into our database. New table keyed by AT URI, storing DID, record key, record CID, title, description, createdAt, blob CID, blob MIME type, blob size, and our own indexedAt. Insert is idempotent (same URI+CID = no-op), so a future firehose indexer and future edits (putRecord on the same TID) fit. Use a migration in the project's existing style.
9. Profile page: plain list of the user's tracks, newest first, title and upload date. No player.
10. Errors: clear messages on the upload page, keeping title and description: not audio; too big (server cap or PDS limit); PDS rejected the blob — including the case where the PDS's own MIME detection labels the audio as non-audio and our `blob:audio/*` scope rejects it: tell the user \"your server doesn't recognise this file as audio, please export it as WAV, FLAC or MP3\"; network/PDS errors. If createRecord fails after uploadBlob, do nothing extra (the PDS discards unreferenced blobs). If our DB insert fails after createRecord, record it on the span and show the user the track was published (the future indexer will pick it up).
11. ffmpeg/ffprobe: add to the production Dockerfile and to CI (check how the shared CI workflows are wired in .github/workflows; if CI can't get ffprobe without changing a shared workflow repo, stop and report rather than hacking around it).
12. Telemetry (use the observability skill; follow the decision that traces are primary and logs stay quiet): the upload request span is a wide event with attributes for file size, ffprobe format/duration/channels/sample rate, declared MIME type, PDS host, DID, AT URI, outcome and failed step, plus important durations (e.g. temp write, ffprobe run) as attributes. Child spans ONLY for outbound PDS calls (describeServer, uploadBlob, createRecord); NOT for local steps like ffprobe. describeServer cache hit/miss as an attribute. No new log lines except the startup cleanup line.
13. Tests:
   - A chromedp browser test against the local PDS (like the existing login browser tests in cmd/app): log in, upload a short committed audio file (a few seconds of WAV or FLAC; keep it small), see it at the top of the profile list, confirm the record and blob exist on the local PDS. Plus: a non-audio file is rejected with the message and the title is kept.
   - Unit tests with the existing fakes: over server cap, over PDS limit, PDS blob rejection, createRecord failure, idempotent insert, temp file deleted in every outcome, describeServer cache incl. failure caching.
   - Verify (by reading the reference PDS source, github.com/bluesky-social/atproto packages/pds, and/or against the local PDS) whether the `blob:` OAuth scope is checked against the declared or the PDS-detected MIME type. Record the answer in the diary and adjust the error handling if needed.

Self-review before committing (code-review skill). Commit with the git skill, reference #34 and #32 so both close. Open a PR against main, short body, no attribution lines. Don't merge.

If you hit a question of product intent you can't resolve from the above, stop and report it with the question rather than guessing. Report back with the PR URL, what the scope check turned out to be, anything that deviated from the requirements and why, and anything surprising."

**Interpretation:** build the whole slice from Step 1: scopes, upload page and pipeline, the tracks table, the profile list, telemetry, Docker and CI, and the tests, then review, commit and open a PR.

**Inferred intent:** a first track can go from a musician's disk into their own repo and show up on their profile, with every failure explained to them and visible in traces.

### What I did

The pipeline is split along the existing layers:

- `/atproto/upload.go`: `Client.DescribeServer` with a per-host cache (a day for descriptions, a minute for failures, nothing kept when the caller's own context was cut short), `Client.UploadBlob` and `Client.CreateRecord`. The upload is a raw `http.Request` sent through the session's `DoWithAuth`, because `atclient.APIClient.Do` cannot set `ContentLength` and the session's HTTP client has a 30-second total timeout (10 seconds for the local network) that a 300 MiB upload must not have. A timer that every body read resets cancels the request after 30 seconds without progress. Login now asks for `repo:com.audioadastra.track` as well (`/atproto/client.go`).
- `/ffprobe/ffprobe.go`: runs `ffprobe` with `-protocol_whitelist file`, `-format_whitelist` of the ten demuxers it maps to MIME types, and a `file:` prefix on the path. A file is audio when it is in one of those formats and has an audio stream with codec, channels and sample rate, and no video stream other than cover art. Matroska with Opus or Vorbis is declared as `audio/webm`, any other Matroska as `audio/x-matroska`.
- `/service/track.go`: `Fat.UploadTrack` validates and trims the fields (`/model/track.go`, graphemes counted with the same `uniseg` the lexicon validator uses), probes, uploads, builds the record with the blob exactly as the PDS returned it, validates it, creates it and saves it. A failed save is recorded on the span and still counts as published. Also `GetBlobUploadLimit` and `GetTracks`.
- `/lexicons/lexicons.go`: `ValidateRecord` now marshals the record to JSON and reads it back with `atdata.UnmarshalJSON` before validating, so a `model.Blob` (which marshals itself to the data model's blob form) validates as what goes on the wire.
- `/sqlite/migrations/1791544256-tracks.up.sql` and `/sqlite/track.go`: a `tracks` table keyed by `uri`. `SaveTrack` is an upsert that changes nothing for the same CID and replaces the row, with a new `indexed_at`, for a new one. `GetTracksByDID` orders by the record's `created_at`.
- `/http/upload.go`: `GET /upload` and `POST /upload`. The POST streams the multipart body to `<TEMP_DATA_DIR>/uploads/upload-*` and deletes the file in a defer. It refuses early on `Content-Length` over the limit plus 64 KiB of form overhead, and cuts the body off with `http.MaxBytesReader`. It keeps reading the form after an oversized file so the title and description can be shown again, and answers JSON to `Accept: application/json` and HTML otherwise. A notice in the cookie session tells the profile page to say the track is published.
- `/public/scripts/app.js`: about 40 lines. A `change` listener sets a custom validity message on a file over the page's `data-max-size`, so the browser itself refuses to submit. A `submit` listener posts the form with `XMLHttpRequest` and dispatches `upload-progress`, `upload-publishing` and `upload-failed` events on the form, which `data-on:*` attributes in `/html/upload.go` turn into signals.
- `/html/upload.go`, `/html/profile.go`: the upload sheet with its progress bar, and the profile with an "Upload a track" pill, the notice and the list. A writer sub-agent recorded the new surfaces in `/DESIGN.md` and updated `/PRODUCT.md`.
- `/cmd/app/main.go`: `TEMP_DATA_DIR` and `UPLOAD_MAX_BYTES` (documented in `/.env.example`), the uploads directory emptied at startup with one log line when something was removed, and startup refuses to run without `ffprobe`. `/Dockerfile` installs `ffmpeg` with `--no-install-recommends`.
- Fakes in `/atprototest/network.go` for `describeServer`, `uploadBlob`, `createRecord` and `getSession`, with knobs for limits, failures, relabelling, scope refusal, delays, nonce rotation and forgotten tokens. `/atprototest/local.go` gained `GetBlob` through `com.atproto.sync.getBlob`. `/audiotest` embeds a 13 KB, two-second FLAC tone made with `ffmpeg -f lavfi -i "sine=frequency=440:sample_rate=8000:duration=2" -ac 1 -c:a flac -compression_level 12 -map_metadata -1 -fflags +bitexact`.
- The browser test is `/cmd/app/upload_test.go`; the login steps moved into a `browser.logIn` helper in `/cmd/app/login_test.go`. `startApp` now sets `TEMP_DATA_DIR` and `CSP_ALLOW_UNSAFE_EVAL=true`.

**The scope check, verified in the reference PDS source** (a research sub-agent read `bluesky-social/atproto` main and the `@atproto/pds` 0.5.37 that the `pds:0.4` image pins; the relevant files did not change between them):
- The `blob:` scope is checked against the **declared** `Content-Type`, in the `authorize` callback of `/packages/pds/src/api/com/atproto/repo/uploadBlob.ts` (`permissions.assertBlob({ mime: parseReqEncoding(req) })`). That happens before any bytes are read.
- The bytes decide only the stored and returned `mimeType`. `uploadBlobAndGetMetadata` in `/packages/pds/src/actor-store/blob/transactor.ts` uses `file-type`'s detection and falls back to the declared type.
- A refusal is HTTP 403 `{"error":"ScopeMissingError","message":"Missing required scope \"blob:<type>\""}`.

So with an `audio/*` type declared from ffprobe, the reference PDS never refuses an upload because its own detection reads the bytes as video. The relabelled type (`video/webm` and the like) lands in the blob ref, which the record carries unchanged and which the lexicon allows, since `original` has no `accept`. I kept the "your server doesn't recognise this file as audio" message, mapped from `ScopeMissingError`, for PDS implementations that check the detected type instead.

### Why

The layers follow the existing login feature. The SDK stays inside `/atproto`, and the service's operations are wired with narrow interfaces. Telemetry is attributes on the request span plus a child span per PDS call.

### What worked

The fakes paid off. `TestFat_UploadTrack` and `TestUpload` in `/http/upload_test.go` run the real client, the real `ffprobe`, the real lexicon catalog and a real database against the fake PDS. They cover every refusal, assert that `/uploads` is empty after each outcome, and that the error text comes back in both HTML and JSON.

I could not run the local PDS (see below), so I checked the JavaScript and Datastar wiring with a throwaway, uncommitted chromedp test. It served the test router and the static files, and Chrome's `--host-resolver-rules` mapped `app.test` and `auth.test` to the in-process fakes. Chrome logged in through the fake auth server and got "doesn't look like audio" with the title and description kept. A FLAC upload then landed at the top of the profile with the notice. A 2 MiB file was refused by the browser with the page's own message, and an upload throttled to 200 kB/s through `Network.emulateNetworkConditionsByRule` showed the bar at 33%.

`docker build --platform linux/arm64 .` succeeds, and `ffprobe -version` in the image reports 7.1.5 from Debian trixie. The image is now 548 MB, since ffmpeg brings its codec libraries even with `--no-install-recommends`.

### What didn't work

- `make test-up` failed before creating anything: `failed to create network upload-track_default: Error response from daemon: all predefined address pools have been fully subnetted`. Docker had run out of address pools for networks. Removing a stale, unused network left over from an earlier worktree of this project was denied by the permission system as modifying a shared resource, so I did not get the compose stack up. **The two chromedp tests in `/cmd/app/upload_test.go` have never run.** They fail at `atprototest.LocalNetwork` with `no container found for service "caddy"`. The real-PDS questions are therefore unverified here: which `mimeType` the local PDS gives the FLAC, and whether its consent screen shows the new repo scope.
- CI: the shared `test.yml` in `maragudk/workflows` runs on `ubuntu-latest`, which does not ship ffmpeg (the runner image readme lists MediaInfo but no ffmpeg), and the workflow has no input for extra packages. As instructed I did not hack around it, so CI's test job will fail on this branch until the shared workflow can install ffmpeg.
- The first JSON for a blob used a `map[string]any`, and the test compared strings. `encoding/json/v2` does not sort map keys, so the order changed from run to run. A struct fixed it.
- `data.Attr("aria-valuenow", ...)` and `data.Class("animate-pulse motion-reduce:animate-none", ...)` render the object form, `data-attr="{aria-valuenow: $percent}"`, whose unquoted hyphenated key is not valid JavaScript. I wrote those two as `data-attr:aria-valuenow` and `data-class:animate-pulse` with `Attr`.
- My first test for the ffprobe whitelists passed with the whitelists removed. ffprobe's HLS demuxer refuses an `http:` segment in a `file:` playlist by itself. The test now uses a local-file playlist and checks that the refusal comes from the whitelist.
- Some long single-line shell commands with heredocs were refused by the worktree guard as "too complex to verify"; edit scripts written to the scratchpad and run separately worked.

### What I learned

- In `net/http`, once a request body has been read to EOF the server starts a background read on the connection. If that read hits the connection's read deadline without being aborted, `handleReadError` cancels the request context. Glue's server has `ReadTimeout: 10s`, so any handler still working ten seconds after reading its body can be cancelled. The upload handler moves the read deadline forward on every body read and clears it once the body is in. The same applies to existing handlers, though login's work happens before it reads much.
- A client that stops sending mid-body hits that same read deadline, the context is cancelled, and the outcome is `client_gone` (499), not `receive_failed`.
- `ffprobe -format_whitelist` matches demuxer names that contain commas (`mov,mp4,m4a,3gp,3g2,mj2`) against `mov`, because `av_match_list` splits both sides.
- indigo's OAuth session reports a refused token refresh only in its error text: `token refresh failed (HTTP 400)` or `auth server request failed (HTTP 400)`. `/atproto/upload.go` matches that with a regexp in `refreshRefused`, covered by `TestRefreshRefused`.

### What was tricky

DPoP retries and large bodies, which both reviewers caught. `DoWithAuth` retries a 401 (stale nonce, expired token) with `GetBody`. My first version rewound one shared `*os.File`, while the transport could still be writing the abandoned first attempt from it, so the second attempt could corrupt the blob. Each attempt now reads through its own `io.NewSectionReader` over an `io.ReaderAt`; `TestClient_UploadBlob` sends 4 MiB of random bytes through a forced retry and compares them. Because the stored nonce is usually stale by the time someone uploads, the upload first makes a cheap authenticated `com.atproto.server.getSession` call, best effort and inside the `uploadBlob` span. That way the nonce and token are fresh before the body goes out, instead of 300 MiB being sent twice.

### What warrants review

- **Datastar and CSP.** This is the first page whose Datastar expressions must actually run. Datastar 1.0.3 needs `script-src 'unsafe-eval'` without a nonce, and `/.env.example` and the code default both say `CSP_ALLOW_UNSAFE_EVAL=false`. Under that configuration `app.js` still posts the upload, and success still redirects, but the progress bar and every error message are dead. Production must set `CSP_ALLOW_UNSAFE_EVAL=true`, or Datastar's nonce mode has to land first (see the 2026-09-02 Datastar diary). I did not change the security default.
- `/http/upload.go`: the deadline handling, the early refusal on `Content-Length` (a browser still sending may see a reset instead of the 413; the page's size check is what users normally meet), and the error-to-message table.
- `/atproto/upload.go`: the retry readers, the `getSession` priming, `blobRefusal` and `refreshRefused`.
- Telemetry keys on the request span: `upload.outcome`, `upload.failed_step`, `upload.size_bytes`, `upload.received_bytes`, `upload.max_size_bytes`, `upload.pds_limit_bytes` or `upload.pds_limit_error`, `upload.temp_write_duration_ms`, `upload.probe_duration_ms`, `upload.upload_blob_duration_ms`, `audio.*`, `upload.declared_mime_type`, `atproto.blob_mime_type`, `atproto.blob_cid`, `atproto.uri`, `atproto.cid`, `atproto.pds_host`, `atproto.describe_server_cache`, and `upload.save_error`.
- Run `make test-up && go test ./cmd/app -run TestUpload` once docker networks are free.

### Future work

- CI needs ffmpeg in the shared test workflow, for example an apt-packages input.
- Uploads have no concurrency cap; each can hold up to 300 MiB on disk and a PDS connection.
- Two uploads as the same OAuth session overlap, against the client's "calls as the same session must not overlap" rule; a token refresh in one can invalidate the other's refresh token. Serialising calls per session belongs in `/atproto`.
- Emptying `uploads/` at startup assumes one process per `TEMP_DATA_DIR`.
- `GetTracksByDID` orders by the author-declared `createdAt`. That is right for our own uploads, but an indexer will see arbitrary values from other clients.
- `uniseg` v0.1.0 matches indigo's validator but is old. A newer emoji sequence could count differently on the PDS.
- A session that expires between page load and upload makes the XHR follow the redirect to the login page, and the user sees "Something went wrong" instead of being sent to log in.
- AGENTS.md and the README don't say that ffmpeg is needed for running and testing locally.

## Step 3: run the browser tests for real, CI and docs

**Author:** builder

### Prompt Context

**Verbatim prompt:** "Markus decided all three:

1. CI ffprobe: a new `apt-packages` input is being added to maragudk/workflows test.yml by another agent (separate PR in that repo). Once it exists, this repo's .github/workflows/ci.yml test job passes `apt-packages: ffmpeg` (alongside `compose: true`). I'll tell you when it's merged; meanwhile add that line already (it will fail CI until the shared PR merges — that's fine, don't push yet).
2. Docker networks: I ran `docker network prune` (32 → 5 networks). Start the local PDS (`make test-up`) and run the browser tests for real (`go test ./cmd/app -run TestUpload` and the full suite). Fix whatever fails. Also verify what the local PDS labels the test FLAC/WAV blob as, and note it in the diary.
3. CSP: production will set `CSP_ALLOW_UNSAFE_EVAL=true` (Markus's prod config, not in this repo). Keep the code default `false`. Issue #36 tracks moving to Datastar's nonce mode. Mention in the PR description that production needs `CSP_ALLOW_UNSAFE_EVAL=true`, plus the new `TEMP_DATA_DIR` (mount a host dir) and `UPLOAD_MAX_BYTES` settings.

Also, from your follow-ups: add a line to AGENTS.md (CLAUDE.md symlinks to it) and the README saying ffmpeg/ffprobe must be installed locally to run the app.

Commit to the same branch, update the diary, but do NOT push or open the PR yet. Report back when the browser tests pass locally."

**Interpretation:** with the docker networks freed, run the real-PDS tests and fix what breaks. Record what the PDS labels the audio as. Wire ffmpeg into CI through the coming shared-workflow input, and document ffmpeg for local development.

**Inferred intent:** prove the write path against a real PDS before the PR goes up, and leave nothing about the new dependency undocumented.

### What I did

`make test-up` came up healthy. `go test -v -run TestUpload ./cmd/app/` passed both browser tests on the first run. The full suite passed too: `go test -tags sqlite_fts5,sqlite_math_functions -shuffle on ./...`, and `go test -race -count 2` with CI's tags on `/cmd/app`.

To find out what the local PDS (`ghcr.io/bluesky-social/pds:0.4`, `@atproto/pds` 0.5.37) labels audio as, I created a throwaway account with `com.atproto.server.createAccount`. I then posted four tones made with ffmpeg straight to `com.atproto.repo.uploadBlob`, each with the type our ffprobe mapping would declare:

| File | Declared | Stored by the PDS |
| --- | --- | --- |
| WAV | `audio/wav` | `audio/wav` |
| FLAC (the committed test tone) | `audio/flac` | `audio/flac` |
| Opus in WebM | `audio/webm` | `video/webm` |
| AAC in M4A (ffmpeg's default brand) | `audio/mp4` | `audio/x-m4a` |

The browser test logs the FLAC's label: "the PDS labelled the FLAC as audio/flac".

I added `apt-packages: ffmpeg` to the test job in `/.github/workflows/ci.yml`. I also added a line on installing ffmpeg to `/AGENTS.md` and `/README.md`.

### Why

The real PDS is the thing the fakes only imitate. This run shows that the consent screen grants the new repo scope and that the blob and record land where the record says.

### What worked

Everything passed the first time against the real PDS. The upload test ran in about four seconds.

### What didn't work

Nothing failed in this step.

### What I learned

The PDS image we test against stores FLAC as `audio/flac`. The 2026-10-09 decision expects `audio/x-flac` from 0.4.x releases, so at least the current 0.4 image no longer emits that alias. The decision's advice for a future FLAC field to accept both is still harmless. The WebM relabelling to `video/webm` is real, and it is now confirmed harmless for uploads: the declared type passed the `blob:audio/*` scope, and only the stored label changed.

### What was tricky

Nothing was tricky in this step.

### What warrants review

The `apt-packages` line in `/.github/workflows/ci.yml` depends on the shared workflow's new input, so CI fails until that change merges in `maragudk/workflows`.

### Future work

The PR description needs to say that production must set `CSP_ALLOW_UNSAFE_EVAL=true` (#36 tracks Datastar's nonce mode). It also needs to cover the new `TEMP_DATA_DIR`, with a host directory mounted there, and `UPLOAD_MAX_BYTES`.

## Step 4: sessions from before the track scope, and the PR

**Author:** builder

### Prompt Context

**Verbatim prompt:** "The shared workflow change is merged (maragudk/workflows#10), so `apt-packages: ffmpeg` in ci.yml now works against @main. Please push the branch and open the PR now. [...] (e) existing users must log in again to grant the new scopes, if that is true; check how the app behaves with an old session and say what you find. [...] Don't merge. Report back the PR URL and whether CI started."

**Interpretation:** find out what a session granted before `repo:com.audioadastra.track` existed does on upload. Then push and open the PR with a deployment section.

**Inferred intent:** deploying this must not strand existing users without telling them what to do.

### What I did

I read the reference PDS's `createRecord` handler (`/packages/pds/src/api/com/atproto/repo/createRecord.ts`). For OAuth credentials it calls `permissions.assertRepo({ action: 'create', collection })` in the handler. An old session has `blob:audio/*`, so `uploadBlob` succeeds. `createRecord` is then refused with 403 `ScopeMissingError`, and the user saw "Your server couldn't publish the track. Try again in a little while.", which retrying cannot fix. The app's login check (`atproto.Client.CheckSession`) only checks that a session exists, not its scopes, so nothing forces a new login. `atproto.Client.CreateRecord` now maps that refusal to `model.ErrorScopeDenied`. The upload page then says "Your login doesn't allow publishing tracks yet. Log out, log in again to allow it, and try again." with a 403 and `upload.outcome=scope_denied`. The fake PDS has a `CreateRecordScopeMissing` knob for it.

### Why

The requirements ask for no special handling of existing sessions, and this adds none. It only makes the error say what helps.

### What worked

The fake made the case cheap to test end to end through the handler.

### What didn't work

I didn't reproduce this against the local PDS. That would need a session granted by the old scopes, so this rests on the PDS source.

### What I learned

The PDS checks the repo scope inside the `createRecord` handler, because it needs the collection from the request body. The blob is uploaded first and left unreferenced, for the PDS to discard.

### What was tricky

Nothing was tricky in this step.

### What warrants review

The message wording. Whether the profile page should say up front that the login predates uploads is a product choice for later.

### Future work

The PR description tells operators that existing users must log out and in again before they can publish.

## Step 5: calls as the same OAuth session take turns

**Author:** builder

### Prompt Context

**Verbatim prompt:** "Please fix a second-opinion finding on PR #37 that Markus approved: serialize calls that use the same OAuth session. [...] Add a per-session lock in `atproto.Client`, keyed by session (DID + session ID), held from resume to the end of each public method that uses the session [...] Don't let the lock map grow forever [...] the wait must respect ctx cancellation [...] Update the \"must not overlap\" doc comments to match. Add a test showing that overlapping calls for the same session are serialized, and that calls for different sessions are not. Run it with -race."

**Interpretation:** make overlapping calls as one session take turns, so two tabs can't both refresh with the same refresh token or save stale tokens over fresh ones.

**Inferred intent:** uploads are long and easy to start twice, so a rule that was only documented now has to be enforced.

### What I did

`/atproto/sessionlock.go` adds `sessionLocks`, a map from DID and session ID to a one-slot channel. A call holds the lock by sending into the channel, so `select` can give up on `ctx.Done()`. Each entry counts the calls holding or waiting for it and is deleted when the count reaches zero, so the map only holds sessions in use. `Client.lockSession` takes the lock and only then resumes the session from the store, so a call that waited reads what the previous call persisted.

`GetRecord`, `PutRecordIfMissing`, `UploadBlob` and `CreateRecord` now go through `lockSession` and hold the lock until they return. Their doc comments say that calls take turns and that the error is the context's when it is done while waiting.

Three calls stay outside the lock, and their comments say so:
- `CheckSession` runs on every request through the auth middleware and only reads the store. With the lock, every page in another tab would hang behind an upload.
- `DescribeServer` only reads the session's host.
- `Logout` should not wait minutes behind an upload. A call still using the session fails once the tokens are revoked.

### Why

The app runs as one process, so an in-process lock is enough. I chose a refcounted map over striped mutexes because it never makes unrelated sessions wait on each other, and the cleanup is a few lines.

### What worked

`TestSessionLocks` uses `testing/synctest` and covers waiting, independence between sessions, giving up when the context is done, and the map ending empty. In `/atproto/upload_test.go`, the fake PDS now records the most `uploadBlob` calls it handled at once. Two overlapping uploads as one session reach at most one, and as two sessions they reach two. A second upload whose context expires while the first holds the lock returns `context.DeadlineExceeded` without reaching the PDS. Both client tests fail when `UploadBlob` goes back to `resumeSession` without the lock. Everything passes with `-race`.

### What didn't work

Nothing failed in this step.

### What I learned

Resuming has to happen after the lock is taken, not before. Otherwise the waiting call holds a copy of the tokens from before the other call's refresh.

### What was tricky

Deciding which calls must not take the lock. Locking `CheckSession` would have been the natural extension, and it would have frozen every other tab for the length of an upload.

### What warrants review

The choice to leave `Logout` unlocked.

### Future work

None from this step.

## Step 6: ffprobe prints only what is read, and its output is capped

**Author:** builder

### Prompt Context

**Verbatim prompt:** "One more second-opinion fix that Markus approved for PR #37. Do it after the session lock: bound ffprobe's output in `ffprobe/ffprobe.go`. [...] Replace `-show_format -show_streams` with `-show_entries`, listing only the fields `audioInfo` actually reads, so tags are never printed. Keep `-print_format json`. Cap both stdout and stderr with a limited writer [...] When stdout goes over the cap, fail the probe as `model.ErrorNotAudio`, or as an internal error if that fits better. Don't parse truncated JSON. Add a test: a short FLAC generated with ffmpeg and a large comment tag [...] Also add a unit test of the cap."

**Interpretation:** an uploaded file's metadata tags must not end up in the app's memory through ffprobe's output.

**Inferred intent:** keep a hostile file from multiplying into gigabytes of memory across concurrent uploads.

### What I did

`Prober.Probe` in `/ffprobe/ffprobe.go` now asks for `-show_entries stream=codec_type,codec_name,channels,sample_rate:stream_disposition=attached_pic:format=format_name,duration`, which are exactly the fields `output.audioInfo` reads. Stdout and stderr go into a `cappedBuffer`, which keeps up to 256 KiB for stdout and 16 KiB for stderr and drops the rest. It never fails a write, so ffprobe is never cut short with a broken pipe. When stdout went over its cap, the probe fails with `model.ErrorNotAudio` and the JSON is not parsed. An output that large after `-show_entries` means thousands of streams, which no audio file has.

### Why

With `-show_format -show_streams`, ffprobe printed every tag. Our test FLAC with an 8 MiB comment produced 8,390,582 bytes of JSON, all of it held in memory before parsing. With `-show_entries` the same file prints a few hundred bytes.

### What worked

The new `TestProber_Probe` case builds a FLAC carrying an 8 MiB comment and checks that it probes as `audio/flac` with a two-second duration. Because the cap is far below the tag's size, the case only passes when the tag is not printed. Swapping the old flags back in fails it with "not audio: ffprobe output is over 262144 bytes". `TestCappedBuffer` covers writes up to the cap, the cut at the cap, and that writes keep reporting success. The full suite passes with `-race`, browser tests against the local PDS included.

### What didn't work

An 8 MiB `-metadata comment=...` doesn't fit on a command line (macOS's `ARG_MAX` is 1 MiB), so the test writes the tag to an ffmetadata file and maps it in with `-i metadata.txt -map_metadata 1 -c copy`.

### What I learned

ffprobe's JSON output from `-show_entries` keeps the same shape (`streams[].disposition.attached_pic`, `format.format_name`), so the parsing struct did not change.

### What was tricky

Choosing between `model.ErrorNotAudio` and an internal error for output over the cap. I chose `ErrorNotAudio`: the cause is the file, and the user gets told to export it as WAV, FLAC or MP3, not that the app is broken.

### What warrants review

The cap sizes, 256 KiB and 16 KiB.

### Future work

None from this step.

## Step 7: logout takes the session lock too

**Author:** builder

### Prompt Context

**Verbatim prompt:** "Please make one more change to PR #37 that Markus decided: `Logout` must take the session lock like the other calls, waiting for any running call on that session to finish. Why: `SaveOAuthSession` upserts. With Logout outside the lock, a running upload can refresh the tokens while Logout revokes the old ones. Logout then deletes the row, and the upload's save re-inserts it with tokens that still work. The user thinks they're logged out, but we still hold a valid grant. [...] If the request context ends while waiting, return that error without revoking or deleting. Check how the logout handler reacts to that error, and make sure the user gets a sensible response instead of a silent half-logout. [...] Add a test: Logout waits while an upload as the same session is in progress, and the session is gone after both finish. CheckSession and DescribeServer stay unlocked."

**Interpretation:** I was wrong in Step 5 to leave `Logout` out of the lock, because the store's upsert lets a running call bring a deleted session back. Make logout wait for the running call, and handle a wait that gives up.

**Inferred intent:** once a user has logged out, the app must hold no working grant for that session.

### What I did

- `atproto.Client.Logout` takes the session lock before it resumes the session and holds it through revocation and `DeleteSession`.
- A wait that the context ends now returns the new `model.ErrorOAuthSessionBusy`, which wraps the context's own error, from both `Logout` and `lockSession`. In that case `Logout` revokes and deletes nothing.
- `sessionLocks.lock` tries a non-blocking acquire before it waits. Without that, a free lock and an already-cancelled context make two ready cases in the `select`, and Go picks one at random. The existing behaviour, where a logout whose context is already done still deletes the session, depends on getting the free lock.
- The logout handler in `/http/login.go` used to log any `Logout` error as `oauth.cleanup_error`, then destroy the cookie session and send the user home. On a busy session that is the silent half-logout: logged out here, with the grant alive in the store. Now `model.ErrorOAuthSessionBusy` leaves the cookie session in place. The user is redirected to `/profile` with the notice "Logging out has to wait for your upload to finish. Log out again once it's done." The span gets `logout.condition=session_busy`.
- `service.Logout`'s 20-second timeout bounds the wait. The doc comments on `atproto.Client.Logout`, `service.Fat.Logout` and the handler say all of this.

### Why

A logout that leaves a working grant behind is worse than a logout that takes a while or asks the user to try again.

### What worked

The new `TestClient_Logout` cases pass:
- A logout started during a 300 ms upload as the same session doesn't return until the upload is done. Afterwards the session is gone and both tokens are revoked.
- A logout whose context expires while an upload holds the lock returns `model.ErrorOAuthSessionBusy` and `context.DeadlineExceeded`. The session is still in the store and nothing is revoked.

Both fail without the lock in `Logout`. `TestSessionLocks` covers the non-blocking first try over 100 rounds with an already-cancelled context. In `/http/login_internal_test.go`, a new `TestLogout` checks the handler's busy path: a redirect to `/profile`, the notice, and no `Destroy`. The full suite passes with `-race`, browser tests included.

### What didn't work

`golangci-lint` flagged `case <-loggedOut:` in the new client test as an unchecked error (errcheck), because the channel carries an `error`. Receiving into `err` and printing it fixed that.

### What I learned

A store write that upserts turns any concurrent writer into a way to bring a deleted row back. Deleting a session has to be serialised with every call that can persist it.

### What was tricky

The order of the two selects in `sessionLocks.lock`, so that a done context only stops a wait and never a lock that nobody holds.

### What warrants review

The notice wording, and sending the user to the profile page.

### Future work

None from this step.

## Step 8: the record URI a PDS answers with must be the account's own

**Author:** builder

### Prompt Context

**Verbatim prompt:** "A security fix for PR #37 that Markus approved, found by the Codex review: bind the URI in the createRecord response to the publishing account. The problem: `session.CreateRecord` in `atproto/upload.go` (around line 326) accepts any well-formed AT URI from the PDS response. A malicious self-hosted PDS can return another user's track URI with a different CID. `SaveTrack` then upserts on `uri`, and when the CID differs it overwrites the victim's title, description and audio while keeping the victim's DID. Any user with their own PDS could deface other users' tracks on our site. [...] reject the response unless the URI's authority is a DID equal to `s.DID()` [...] its collection equals the requested collection [...] its record key parses as a TID [...] Reject with `model.ErrorRecordWriteFailed`, and put what didn't match on the span. As defence in depth, make `SaveTrack`'s conflict update refuse to change a row whose `did` differs from `excluded.did` [...] Add client tests with a fake PDS that returns another DID's URI, a different collection, a non-TID record key, a handle authority. Each must be rejected, and nothing may reach the database."

**Interpretation:** a PDS is the user's server, not ours. Nothing in its answer may decide which row of our database gets written.

**Inferred intent:** no user can change another user's tracks on the site by running their own PDS.

### What I did

- **The check.** `session.CreateRecord` checks the returned URI with the new `recordURIMismatch` before it trusts it. The authority must be a DID equal to the session's, the collection must be the one asked for, and the record key must parse as a TID. Otherwise the call fails with `model.ErrorRecordWriteFailed`, and the span gets `atproto.uri_mismatch` (what didn't match) and `atproto.returned_uri`.
- **The defence in depth.** `sqlite.Database.SaveTrack`'s conflict update now requires `tracks.did = excluded.did`. When nothing changed, a second query tells "this version is saved already" from "this URI is saved for another DID", and the second is an error. The service records it as `upload.save_error` like any other failed save.
- **The fake.** The fake PDS can answer `createRecord` with any URI (`CreateRecordURI`).

### Why

Before, a PDS could answer with a victim's URI and a new CID, and `SaveTrack` would replace the victim's title, description and audio while keeping the victim's DID. That made every user with their own PDS able to deface anyone's tracks.

### What worked

- **Client tests.** `TestClient_CreateRecord` refuses each of the four forged answers: another DID, another collection, `self` as the key, and a handle as the authority. Each puts the right mismatch on the span.
- **End to end.** In `TestFat_UploadTrack`, each forged answer fails the upload at `create_record`. Bob's saved track keeps its title and CID, and Alice has no rows. All four cases fail if the URI check is switched off.
- **The database guard.** `TestDatabase_SaveTrack` shows that a track under another DID with the same URI and a new CID is refused, and leaves the row as it was.
- **The full suite** passes with `-race`, browser tests included.

### What didn't work

Nothing failed in this step.

### What I learned

indigo's `syntax.ParseATURI` accepts a handle as the authority. Checking that a URI is well-formed says nothing about whose record it names.

### What was tricky

Telling the two kinds of "nothing changed" in `SaveTrack` apart. The upsert's `returning` clause returns no row in both cases, so the DID check needs a second query, which only runs on the no-op path.

### What warrants review

That `SaveTrack` treats a DID mismatch as an error rather than a silent no-op. An indexer that later saves records from the firehose would rely on that too.

### Future work

None from this step. A separate audit is looking for other places that trust what a PDS answers.

## Step 9: response bodies from PDSes and auth servers are capped

**Author:** builder

### Prompt Context

**Verbatim prompt:** "First of the security audit's findings for PR #37, which Markus approved: cap the size of response bodies from PDSes, auth servers and identity resolution. [...] indigo's `atclient` [...] and `oauth` [...] decode response bodies with `json.NewDecoder(resp.Body)` and no limit. A self-hosted PDS can stream an endless JSON string, and the app allocates memory until the call's deadline [...] In `atproto/client.go`'s `NewClient`, wrap the outbound transports once so every SDK call is covered [...] Use a RoundTripper that replaces `res.Body` with a reader limited to 1 MiB. Reading past the limit must give an error, not a silent truncation that might parse. Keep the existing SSRF transport underneath [...] Check whether the identity directory already caps its bodies [...] Test: a fake server answering with more than 1 MiB makes a call fail quickly with a clear error, and a normal answer still works."

**Interpretation:** every answer the SDK reads from someone else's server gets a size limit, applied once in the transport.

**Inferred intent:** no user's server can make the app run out of memory.

### What I did

`/atproto/limit.go` adds `limitBodies`, which copies an `http.Client` and wraps its transport in `bodyLimitTransport`, so that SSRF protection and the local dialler still run underneath. Each response body becomes a `limitedBody`, which reads up to the limit and fails the first read that finds more, with "response body is over N bytes". Silently ending a body early could leave a truncated JSON document that still parses.

At the end of `NewClient`, after the local and test clients are chosen, `c.app.Client` and `c.app.Resolver.Client` are wrapped at 1 MiB. That covers:
- every session call, since `ResumeSession` takes `app.Client`
- the token exchange, PAR and revocation
- auth server discovery and metadata
- `describeServer` and the upload's own client, which reuses `app.Client.Transport`

The identity directory is left alone. did:web and handle resolution already cap their bodies (`identity/did.go`, `identity/handle.go`). PLC resolution reads the PLC directory without a limit, but that is a fixed service, not anyone's PDS.

The fake PDS can pad its `describeServer` and `getRecord` answers (`ResponsePadding`).

### Why

The audit found that a self-hosted PDS could stream an endless JSON string, and the app would hold it in memory until the call's deadline, 5 to 30 seconds later. Concurrent requests would multiply that.

### What worked

- **`TestLimitBodies`** covers a body of exactly the limit, a body one byte over it, and that the real network's clients are still wrapped. Through both clients, a request to `127.0.0.1` still fails with "not a public IP address".
- **Client tests:**
  - `TestClient_DescribeServer`: a 2 MiB answer fails in well under a second with the clear message.
  - `TestClient_GetRecord`: a 512 KiB answer still works, and a 2 MiB one fails.
- Without the wrapping in `NewClient`, all three tests fail.
- The full suite passes with `-race`, browser tests against the local PDS included.

### What didn't work

Nothing failed in this step.

### What I learned

indigo's own identity code truncates silently at its limits; its TODO points at bluesky-social/indigo#1460. Ours errors instead.

### What was tricky

Where to wrap. It has to happen after the clients are chosen (real, local or test fake). Otherwise the local and test clients would replace the wrapped one.

### What warrants review

The 1 MiB limit. No PDS answer the app reads comes near it, and `uploadBlob` reads at most 64 KiB anyway.

### Future work

None from this step.

## Step 10: the PDS's description of an uploaded blob is checked

**Author:** builder

### Prompt Context

**Verbatim prompt:** "Second audit finding for PR #37, which Markus approved: verify the blob description the PDS returns from uploadBlob. [...] `session.UploadBlob` [...] accepts the PDS's `mimeType`, `size` and CID with only a syntax check on the CID. They go into the record and into `tracks.blob_*`. A user's own PDS could claim `text/html`, a size of -1, or another blob's CID. [...] Size: the returned `size` must equal the size we sent. CID: compute the expected CID while streaming the content [...] CIDv1 with the raw codec (0x55) and a sha2-256 multihash [...] MIME type: the returned type must be one we expect for the declared type, based on what we measured on the local PDS [...] Keep the mapping in one small table next to the formats. Don't require `audio/*`, because WebM comes back as `video/webm`. Any mismatch returns an error [...] puts which check failed on the span, and writes nothing to the database. [...] Don't change the lexicon"

**Interpretation:** the blob in the record and in `tracks` must be the file that was sent, described truthfully, whatever the user's own PDS says.

**Inferred intent:** playback (#11) can rely on these fields.

### What I did

- **Size and CID** are checked in `session.UploadBlob` (`/atproto/upload.go`). Before sending, it hashes the content once with SHA-256 through a fresh `io.NewSectionReader`. That is simpler than hashing while streaming, and the result is the same whichever attempt the PDS keeps. From the hash it makes a CIDv1 with the raw codec. `blobCheck` then requires the returned size to equal the size sent, and the returned CID to parse, be CIDv1 with the raw codec and a SHA-256 multihash, and equal the content's CID. Otherwise the call fails with `model.ErrorBlobRejected`, and the span gets `atproto.blob_check_failed` set to `size`, `cid_syntax`, `cid_codec`, `cid_hash` or `cid`.
- **The MIME type** is checked in the service. Which labels are acceptable for which audio is domain knowledge, so the table lives in `/ffprobe/ffprobe.go`, next to `formats`, as `relabels`: declared type to the other types a PDS may store. `model.AudioInfo.StoredMIMETypes` carries the declared type plus those. `service.UploadTrack` refuses a blob stored as anything else, with `model.ErrorBlobRejected` and `upload.blob_check_failed=mime_type`, before any record is created.
- **The fake PDS** can rewrite its `uploadBlob` answer (`UploadBlobAnswer`).

The table has the types measured on the local PDS in Step 3:
- WAV as `audio/wav`
- FLAC as `audio/flac`
- WebM as `video/webm`
- M4A as `audio/x-m4a`

It also has `audio/x-flac` from the 2026-10-09 decision, and `video/mp4` for MP4 audio without the M4A brand (Step 1's reading of `file-type`). Beyond the list I was given, I added older `file-type` names for WAV, AIFF, AAC, Ogg Opus, Matroska and WavPack. They cost nothing and keep older PDS releases from being refused.

### Why

A user's own PDS could describe the blob as `text/html`, give a size of -1, or point at another blob's CID. Today that would corrupt only their own row. Playback will read these fields.

### What worked

- **Refusals:** `TestClient_UploadBlob` refuses another size, another blob's CID, a CID with the DAG-CBOR codec, one with a SHA-512 multihash, and one that doesn't parse. Each puts the right check on the span. `TestFat_UploadTrack` refuses a blob stored as `text/html`: no record is created and no row saved.
- **The table:** the `/ffprobe` tests check it for FLAC, WebM and M4A.
- **Against the real PDS:** both browser tests still pass. That shows our CID computation matches what the PDS computes, since the FLAC upload's CID check passed there.

### What didn't work

Nothing failed in this step.

### What I learned

The CID of a blob is fully determined by its bytes: CIDv1, raw codec, SHA-256. That makes it the one field of the PDS's answer the app can check exactly.

### What was tricky

Where to check what. Size and CID are protocol facts and belong in `/atproto`. Which labels are acceptable for an audio format is domain knowledge, and keeping it in `ffprobe` puts it next to the formats that produce the declared types.

### What warrants review

The extra old names in `relabels`, and the extra read of the file for hashing: about a second for 300 MiB on local disk.

### Future work

None from this step.
