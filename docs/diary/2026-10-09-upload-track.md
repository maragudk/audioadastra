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
