# Project Decisions

This document records significant architectural and design decisions made throughout the project's development.

## 2026-09-14: Publish `com.audioadastra.*` lexicons from a dedicated account on our own PDS

Lexicon schemas are published as `com.atproto.lexicon.schema` records in the repository of the DID
that `_lexicon.audioadastra.com` points to. That DID is the permanent authority for the whole
`com.audioadastra.*` namespace, so which account holds it matters.

Alternatives considered:
- Markus's personal account: conflates a person with the project; every future lexicon must be
  published from it.
- A `bsky.social`-hosted project account: trivial to create, but the project's identity and data
  would live on infrastructure we don't control. (The Bluesky PDS "unknown lexicon" 400 is not a
  factor: it only appears when a client sends `validate=true`, as goat does by default; apps that
  leave `validate` unset write unknown record types fine.)
- A dedicated project account on the PDS Markus already runs (`https://pds.atproto.dk`).

Decision: create a new account with handle `audioadastra.com` on `pds.atproto.dk` and publish all
lexicons from it. The handle and both `_lexicon.audioadastra.com` and
`_lexicon.actor.audioadastra.com` TXT records point at that account's DID. This account is the
publishing identity only; a future app view service DID or labeler gets its own identity. Users
can be on any PDS, including Bluesky's.

## 2026-09-14: Log in with atproto OAuth only; the DID is the user identity

Context: the glue template ships email magic-link login scaffolding (never wired up here). An
atproto app's users already have a durable, cryptographically verified identity, and every record
they create lives in their own repository, so a second identity kind buys nothing.

Alternatives considered:
- Keep email login alongside atproto OAuth and link a DID later: two identity kinds to reason about
  everywhere, for users who by definition can't use the product without an atproto account.
- Public OAuth client: less to operate, but shorter sessions; we have a backend, so the
  confidential client is the shape the protocol rewards.

Decision: atproto OAuth is the only way to log in. The DID is the sole identity key in `users`;
email, accounts and login tokens are dropped. The app is a confidential OAuth client with a P-256
key from config. Login requests `atproto`, `repo:com.audioadastra.actor.profile`, `blob:audio/*`
and `blob:image/*`, and fails if any is not granted. The blob scopes are asked for up front so
uploads don't re-prompt for consent; the repo scope names the collection explicitly because the
permission syntax has no partial wildcard (`repo:com.audioadastra.*` is invalid and `repo:*` is
far too broad), so each new record type will re-prompt until a published permission set
(`include:com.audioadastra.<set>`) bundles them; that is a follow-up. First login writes an
empty `com.audioadastra.actor.profile` record to the user's repo, as a hard requirement, so a
profile record marks every account that has used the app. OAuth sessions are per device. Handles
are not cached yet: the only handle rendered is the logged-in user's, resolved (and
bidirectionally verified) through the identity directory; caching and identity-event refresh are
deferred to the indexing feature.

Dev and tests never write to the real atproto network: automated tests run against in-process
fakes, manual end-to-end runs use a local PDS + PLC in docker compose, and the first real-network
login happens in production.

## 2026-09-18: Browser tests run against a real local PDS, in CI, through docker compose

Context: the login is an OAuth dance with a real auth server UI in the middle, and the fakes in
`atprototest` only prove what the fakes were taught. The PDS consent flow, its handling of DPoP
nonces, `swapRecord`, and revocation are the things that actually break.

Alternatives considered:
- Manual runs against the compose network before each merge: proved the flow twice during the
  feature, but nobody re-runs them, and the fakes drift from the PDS.
- A browser automation toolkit that downloads its own browser (playwright-go, rod): a second
  runtime to keep current, and downloads in CI.
- `chromedp`: pure Go, drives the Chrome already on the machine and on the CI runner, no
  downloads.

Decision: the compose file (PLC directory built from a pinned source commit, the real PDS image in
dev mode, Caddy with a local CA) is a test dependency, started by `make test-up` and by the CI
workflow's `compose` input. Browser tests in `cmd/app` start the app in-process, create a fresh
PDS account per run, and drive Chrome with `chromedp` through login, consent, profile, logout and
denial. They skip in `-short` mode, so the fast suite stays fast; `make test` and CI run
everything. The fakes stay for the refusal paths and telemetry assertions, where a real PDS cannot
be made to misbehave on demand. Nothing in any test reaches the real atproto network.

## 2026-10-06: OpenTelemetry traces are the primary telemetry; logs stay mostly silent

Context: the login feature emitted the same facts twice, as attributes on wide-event spans and as
structured log lines carrying the same attributes. Debugging happens in traces, so the logs were a
second, unused copy to keep in sync.

Decision: what happened in a request, and why it failed, goes on spans: attributes on the main
span as a wide event, child spans for outbound calls, recorded errors and span status. Logs are
for what traces cannot carry, such as startup and shutdown, configuration warnings, and failures
outside any request. A log line that only repeats what is already on a span is not added. Library
logs go through the app's logger, so the few that remain carry trace IDs.

## 2026-10-06: A pink sky as the visual identity, recorded in PRODUCT.md and DESIGN.md

Context: the app had a stock look (a white panel under a pink header bar, Inter, an ear logo the owner
disliked) and no record of what the product is or how it should look, so every new surface would
start from scratch.

Alternatives considered:
- Era references (a pirate radio dial, a riso gig poster, a 70s space-disco sleeve, the Voyager
  Golden Record): rejected as too retro and referential.
- A violet night sky with pink accents: rejected because pink stopped being the dominant colour.
- A conventional music-platform landing page: rejected as indistinguishable from the category.

Decision: the name taken literally. Every page sits on a drenched Tailwind pink-600 sky of white
stars, and the front page draws the name as a constellation that plays as music. Violet-950 is
reserved for whatever is playing. Bluu Next is the display face for the site's own titles, and Inter
is used for body text, controls and user content. Small text goes on white sheets, because
light text on pink-600 cannot meet AA contrast below large sizes. The product context lives in
`PRODUCT.md` and the visual system in `DESIGN.md`. New surfaces start from those two files, and an
owner-approved change to the look updates `DESIGN.md` in the same change.

## 2026-10-09: Tracks keep the original upload and an app-made FLAC, both in the user's repo

Context: the track record (`com.audioadastra.track`) had to say which audio file it carries, and
blob constraints in a published lexicon can never change. The goal is that no track is ever
unplayable in a major browser, while the musician's own file is never lost.

Alternatives considered:
- The original upload only, with streaming versions made and served by the app view (Bluesky
  video's simple flow): other clients without a transcoder would have to play whatever was
  uploaded, including formats some browsers cannot play.
- A FLAC only, as the master: smaller, but a transcoding bug would be baked into every track for
  good, with nothing to regenerate from.
- Original, FLAC and a lossy version from the start: a third blob per track before anyone needs
  one.

Decision: every track carries a required `audio` object with two required blobs. `original` is
the upload, unchanged. `lossless` is a FLAC that the app always generates, even from a FLAC
upload, keeping the source's sample rate and bit depth where FLAC can (it stores integer samples
only, so a floating-point master is converted), so the app controls the playable file and can
regenerate it from the original. Transcoding happens during upload, before the record is written.
Regenerating later updates the same record; the PDS keeps only current state, so the replaced
FLAC is gone, and the original is never touched.

Neither blob has a `maxSize`: each PDS enforces its own limit, and both blobs must fit it. A FLAC
made from a lossy upload can be several times bigger than the upload (an hour-long 128 kbps MP3
of about 57 MB becomes roughly 350 MB), so in practice the ceiling is about an hour of CD-quality
audio whatever the upload format. A user who hits their PDS's limit can move their account to a
PDS with a higher one. `lossless` stays required anyway, so every track is guaranteed playable.

`original` has no `accept` list. The PDS sets a blob's MIME type from the bytes it detects, and
labels some audio as something else (audio-only WebM as `video/webm`, some M4A as `video/mp4`),
so an `audio/*` constraint would permanently reject common files. Instead, the app checks that
an upload decodes as audio, and the app view applies its own rules to records from any client;
both can change without touching the lexicon.

A lossy encoding can be added later as an optional field. Facts derivable from the audio, such as
duration, live in the app view's database, not the record.
