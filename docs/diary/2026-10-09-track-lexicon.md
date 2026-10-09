# Diary: `com.audioadastra.track` lexicon

The second lexicon in Audio Ad Astra and the remaining half of issue #7 ("Add lexicons for user
profile and track"; the profile shipped in PR #2). Lexicon-only, the same slice as the profile:
the schema JSON, valid and invalid record fixtures, and the existing lexicon tests picking it up.
No upload, transcoding, playback, OAuth scope or network publishing.

## Step 1: shape the requirements

**Author:** main

### Prompt Context

**Verbatim prompt:** "Let's have a look at issue 7 together." The brainstorm closed with "Just
publish the lexicon in a PR and I'll comment inline."

**Interpretation:** issue #7 asks for profile and track lexicons, with a later comment "Maybe also
playlist?". The profile already exists, so the work is designing the track record, field by
field, with Markus, then putting it up as a PR for inline review.

**Inferred intent:** get the track record right before the upload milestone (#6) builds on it,
because published lexicon constraints can never be tightened or loosened.

### What I did

Read the issue, the profile diary, `/docs/decisions.md` and `/PRODUCT.md`, and sent two research
sub-agents out: one on prior art (plyr.fm, Comet, teal.fm, Rocksky, Bluesky video, PDS blob
limits), one on track title limits on other platforms. Then walked the design with Markus one
question at a time. Decisions:

- **Scope:** track only. Playlists are not needed for milestone 1 and would be designed on
  guesses.
- **NSID:** `com.audioadastra.track`, flat. "Feed" was rejected because this is not a feed
  product; `audio.track` was considered. Flat resolves through the existing
  `_lexicon.audioadastra.com` TXT record, and the music apps that host audio (plyr.fm) are flat
  too. `actor.profile` stays as it is.
- **Record key:** `tid`.
- **Audio:** a required `audio` field referencing a named `#audio` object def with two required
  blobs: `original` (`accept: ["audio/*"]`, the upload unchanged) and `lossless`
  (`accept: ["audio/flac"]`, a FLAC at the source's sample rate and bit depth, used for
  playback). The app always transcodes, even a FLAC upload, so it controls the playable file and
  can re-encode it later. The original is kept as insurance against transcoding bugs. A lossy
  encoding can later be an optional `lossy` field in `#audio`. No `maxSize` on either blob: PDS
  limits keep rising (50, 100, 300 MiB) and any number written is permanent; each PDS enforces
  its own limit. Consequence accepted: an upload whose original exceeds the user's PDS limit
  cannot be published.
- **Title:** required, `minLength: 1`, `maxGraphemes: 300`, `maxLength: 3000`.
- **Artist:** no field. The repo owner is the artist, shown with their current
  `com.audioadastra.actor.profile` `displayName`, falling back to handle. Labels and curators
  are not target users. Collaborators can come later as an optional field holding DIDs.
- **Duration:** left out. It is derived from the FLAC, so it belongs in the app view's database;
  an optional hint can be added later.
- **Description:** optional, `maxGraphemes: 5000`, `maxLength: 20000`, for notes, credits and
  lyrics.
- **createdAt:** required datetime.
- **Left out for now:** artwork, tags, self-labels, playlists.

### Why

Every constraint in a published lexicon is permanent, so the fields were decided deliberately
and kept minimal, with later additions planned as optional fields (which are non-breaking).

### What worked

Checking indigo's `lexlint` source directly settled two questions quickly: it has no rule
requiring blob `maxSize`, and its `large-string` rule warns on any string `maxLength` over
20 KiB, which capped the description at 20000 bytes.

### What didn't work

My first figures for PDS blob limits, from memory, were out of date (I said the installer sets
50 MB and bsky.social around 100 MB). The research agent found the installer now sets 300 MiB and
the Bluesky PDS hosts report `"blobUploadLimit":314572800` via
`com.atproto.server.describeServer`. The reference PDS code default is still 5 MB.

I also cited a Bandcamp title limit of about 300 characters that the title research could not
confirm. Only SoundCloud publishes one (100 characters); plyr.fm caps at 256 bytes and Rocksky
at 512 bytes.

### What I learned

- `lexlint`'s breaking-change check flags any change to a blob's `accept` or `maxSize`, so both
  are permanent once published.
- teal.fm and Rocksky are scrobblers with no hosted audio; plyr.fm and Comet are the real prior
  art for audio hosting.
- Bluesky video's recommended flow has a service write an optimised blob to the user's PDS, so
  the record holds the processed file, which is the same shape as transcoding on upload.
- The split between record and app view: records hold what only the author can say; anything
  derivable from records and blobs belongs to the app view, except small hints clients need
  before fetching a blob (Bluesky's image `aspectRatio`).

### What was tricky

The audio shape went back and forth: original only, then FLAC only, then original plus FLAC,
then all three with a lossy version, before settling on original plus lossless. The deciding
argument for keeping the original was insurance against our own transcoding bugs.

### What warrants review

The whole schema, inline on the PR. In particular the record description ("The account is its
artist."), which encodes the artist decision for other clients.

### Future work

- #10 now means transcoding during upload to FLAC at the source's sample rate and bit depth,
  keeping the original.
- A notice-and-takedown route is needed before public launch.
- Adding `repo:com.audioadastra.track` to the login scopes belongs to the upload feature.
- Publishing the lexicon to the network is manual (`goat lex publish`), as with the profile.

## Step 2: build the track lexicon, fixtures and tests

**Author:** track-lexicon-builder

### Prompt Context

**Verbatim prompt:** "Add the `com.audioadastra.track` lexicon to Audio Ad Astra (issue #7) and
open a PR. Lexicon-only, the same slice as the profile lexicon in PR #2: no Go app code, no OAuth
scopes, no tables, routes, views, and do NOT publish anything to the atproto network." (The full
brief gave the schema JSON verbatim, the fixture cases to cover, and asked to verify what indigo's
`ValidateRecord` actually enforces before asserting on it.)

**Interpretation:** land the schema from Step 1 unchanged, bring back the `nsid` column in the
table-driven test, cover every constraint with a one-defect fixture, and report any constraint
indigo does not enforce rather than fake a test for it.

**Inferred intent:** get the schema in front of Markus for inline review, with tests proving each
constraint does what Step 1 decided.

### What I did

- Wrote `/lexicons/com/audioadastra/track.json`. The path follows the NSID like the profile:
  `com.audioadastra.track` maps to `lexicons/com/audioadastra/track.json`. Checked it is
  JSON-equal to the brief's schema. `TestLexiconSchemas` picks it up with no edits and lints it
  clean; `goat lex lint` agrees.
- Generated 14 fixtures in `/lexicons/testdata/com/audioadastra/track/` from a scratch Python
  script. Each invalid fixture is the minimal valid record with exactly one defect, following the
  profile review's "one defect per fixture". Blobs use the full form
  `{"$type": "blob", "ref": {"$link": ...}, "mimeType": ..., "size": ...}` with two real
  raw-codec CIDv1s (sha256 of short strings). The minimal fixture uses `audio/flac` for the
  original too, so a FLAC upload matching `audio/*` is covered; the full one uses `audio/wav`.
- Brought back the `nsid` column in `TestLexicons` and added 14 track rows.
- Added a `TestNewCatalog` subtest that loads the embedded catalog and validates the minimal
  track fixture through `lexicons.Catalog.ValidateRecord`, so a broken embed would fail too.
- Before writing assertions, dumped the real errors from a throwaway test.

### Why

Every constraint in the schema is permanent once published, so each one gets a fixture that
proves indigo rejects a record breaking it, for the reason the fixture name claims.

### What worked

Dumping the errors first. indigo enforces everything in the schema, including blob `accept`:

- `required field missing: audio` / `original` / `lossless` / `title` / `createdAt`
- `blob mimetype doesn't match accepted: audio/mpeg` (lossless) and `... image/png` (original)
- `string length outside specified range: 0` (empty title, `minLength`)
- `string length (graphemes) outside specified range: 301` / `5001`
- `string length outside specified range: 3025` (title of 121 family emoji: 121 graphemes but
  3025 UTF-8 bytes, so `maxLength` is checked)
- `Datetime syntax didn't validate via regex`

Showing red first: with `track.json` moved out, the track rows fail with
`schema not found in catalog: com.audioadastra.track#main`.

### What didn't work

- My first fixture script produced a 4997-grapheme description (`"La la la la. " * 384 +
  "Fin!!"`). An `assert` in the script caught it before any test ran.
- Two compound shell commands were refused by the worktree sandbox ("too complex to verify that
  it stays inside the worktree"). Splitting them, and writing the script with the Write tool,
  worked.
- `go test ./...` fails in `app/cmd/app` with `local atproto network is not up, run make
  test-up: ... no container found for service "caddy"`. It is the Docker-backed login test and
  unrelated to this change; every other package passes.

### What I learned

- indigo's `acceptableMimeType` (`atproto/lexicon/mimetype.go`) is an exact, case-sensitive
  string match, with a trailing `*` turned into a prefix match. `audio/*` is the prefix
  `audio/`. `audio/x-flac`, `audio/FLAC` and `audio/flac; charset=x` all fail `lossless`.
  `audio/ogg; codecs=opus` passes `original`, because it starts with `audio/`.
- A blob object without `"$type": "blob"` is not parsed as a blob; validation says
  `expected a blob`. With no `maxSize`, indigo accepts any size, including 0.
- `lexicons.Catalog.ValidateRecord` takes `map[string]any`, so blob fields must hold
  `atdata.Blob` values (as `atdata.UnmarshalJSON` produces), not plain JSON maps. That matters
  for the upload feature.
- The reference PDS stores the type it sniffs from the bytes, not the type the client sends:
  `packages/pds/src/actor-store/blob/transactor.ts` uses `typeDuplex.fileTypeResult?.mime ||
  fallbackMime`. Its `file-type` dependency (`^22.0.1`) labels FLAC `audio/flac`, so the
  `lossless` accept list matches. It labels some audio files with non-audio types:
  `application/ogg` (Ogg with an unknown codec), `video/mp4` (MP4-branded AAC), `video/webm`
  and `video/matroska`. Such uploads fail the `original` accept list.

### What was tricky

Deciding which review findings to act on, because the brief froze the schema. Anything touching
the JSON went to the lead instead (see below).

### What warrants review

- Self-review (competing reviewers via the code-review skill) raised eight findings. I acted on
  two: a byte-limit fixture (`title-too-many-bytes-invalid.json`) and the embedded-catalog
  subtest. I left the `nsid` column explicit rather than deriving it from the fixture path; a
  wrong NSID fails loudly, so the column costs little. I did not add a missing-`$type` track
  fixture; the profile already covers that check in indigo.
- Reported to the lead and not changed, because they touch the frozen schema or the lead's
  decision record:
  1. The `audio/*` accept on `original` rejects audio that the PDS sniffs as
     `application/ogg`, `video/mp4`, `video/webm` or `video/matroska`. The app can reject or
     remux those at upload, or `accept` could be widened before publishing.
  2. A FLAC made from a lossy upload can be many times larger than the original. A one-hour
     128 kbps MP3 set is about 57 MB, but as 16-bit/44.1 kHz FLAC it is several hundred MB.
     So the lossless blob, not the original, can be the one over the PDS limit. The decision
     record covers only the original.
  3. The `lossless` description promises the original's "sample rate and bit depth". FLAC has
     no float samples, and lossy sources have no bit depth.
  4. `audio`, `title` and `#audio` have no `description`.
- Validate with `go test -tags sqlite_fts5,sqlite_math_functions -shuffle on ./lexicons/...` and
  `golangci-lint run`.

### Future work

The four schema findings above need a decision from Markus before the lexicon is published.
After that, nothing more is needed in this slice.

## Step 3: apply the PR #30 review round

**Author:** track-lexicon-builder

### Prompt Context

**Verbatim prompt:** "Review of PR #30 is triaged with Markus. Apply these changes in the same
worktree, commit, and push to the PR branch. All GitHub review threads are already answered and
resolved; don't post on the PR." (The brief listed six exact schema edits, the fixture changes,
the `PRODUCT.md` rewording, and the review decisions for this entry.)

**Interpretation:** make exactly the listed edits to the schema, turn the non-audio `original`
fixture into a valid one, move the full fixture's `createdAt` to a recent date, narrow
`PRODUCT.md` to music, and commit the lead's `docs/decisions.md` edit unchanged.

**Inferred intent:** settle the schema findings from Step 2 before the lexicon is published, and
make the descriptions say what the record is, not how the app makes it.

### What I did

- `/lexicons/com/audioadastra/track.json`: new descriptions on the record, `description`,
  `createdAt`, `#audio`, `original` and `lossless`. Removed `accept` from `original`, so any
  mimetype is valid. `lossless` keeps `["audio/flac"]`. Required lists, limits, the `tid` key and
  the lack of `maxSize` are unchanged.
- Renamed `original-not-audio-invalid.json` to `original-relabelled-by-pds-valid.json` with
  `git mv`. Its original is now `video/webm`, one of the labels the reference PDS gives to
  WebM audio. The test row now expects it to pass.
- `full-valid.json`: `createdAt` moved from `1977-08-20T14:29:00Z` to `2026-10-01T20:15:00Z`,
  so nobody reads it as the date the music was made.
- `/PRODUCT.md`, Product Purpose: "sharing audio and music" is now "sharing music", plus a
  sentence on scope: songs, demos and sets, but not podcasts. Other lines that say "audio" refer
  to the stored files, so I left them alone.
- Committed the lead's edit to the 2026-10-09 entry in `/docs/decisions.md` unchanged.

### Why

The review decisions, briefly:

- **`tid` kept.** No key is a natural default here. `any` keys built from slugs collide, and a
  slug can't follow a title rename.
- **Replacing audio** is a `putRecord` on the same TID with `swapRecord`. The PDS keeps only the
  current state, so it garbage-collects the old FLAC blob.
- **`lossless` stays required**, even though a FLAC made from a lossy upload is much larger than
  the upload.
- **`original` has no `accept`**, because the PDS relabels some audio by sniffing the bytes
  (`application/ogg`, `video/mp4`, `video/webm`, `video/matroska`). The login's `blob:audio/*`
  OAuth scope has the same problem; that is issue #32, a sub-issue of #6.
- **Notice-and-takedown** is tracked in issue #29.
- A second opinion from OpenAI's gpt-6-astra (via codex) raised the FLAC size, MIME relabelling
  and float-sample points independently of the Step 2 self-review.

### What worked

The table needed one row change. With `accept` gone, nothing else in the fixtures depended on
it. `TestLexiconSchemas` and `goat lex lint` both stay clean: lexlint has no rule against a blob
without `accept`.

### What didn't work

Nothing failed.

### What I learned

A blob without `accept` is valid lexicon and lints clean, so leaving a type open is a deliberate
option, not a lint gap.

### What was tricky

Keeping the `PRODUCT.md` edit minimal. "Audio" in that file means two things: the product's
scope, which changed, and the stored files ("Audio lives in your own repo"), which did not. Only
the scope wording was changed.

### What warrants review

- `/lexicons/com/audioadastra/track.json`: compare against the six edits in the brief.
- `/PRODUCT.md`: one paragraph.
- Validate with `go test -tags sqlite_fts5,sqlite_math_functions -shuffle on ./lexicons/...` and
  `golangci-lint run`.

### Future work

Issue #32 (the `blob:audio/*` login scope) and #29 (notice-and-takedown). Publishing the lexicon
stays manual, after merge.

## Step 4: drop the `lossless` blob from the track record

**Author:** track-lexicon-builder

### Prompt Context

**Verbatim prompt:** "Another review round with Markus (prompted by two more gpt-6-astra
reviews) changed the audio design. Apply in the same worktree, commit, push to the PR branch.
Don't post on the PR." (The brief: remove `lossless` from `#audio` so only a required
`original` is left, remove the lossless fixtures, strip `lossless` from the others, add a valid
fixture with an unknown field inside `audio` only if indigo accepts it, commit the lead's
rewritten decision entry unchanged, and record the review round here.)

**Interpretation:** cut the schema down to one required blob, keep the `#audio` object so
encodings can come back later as optional fields, and test that a record with such a later
field still passes today's schema.

**Inferred intent:** publish nothing about playback copies that the schema can never take back.
Playback copies become the app view's job.

### What I did

- `/lexicons/com/audioadastra/track.json`: removed `lossless`. `#audio` now has
  `required: ["original"]` and one blob, `original`, with no `accept` and no `maxSize`. The
  `#audio` description and the required `audio` ref on the record are unchanged.
- Deleted `missing-lossless-invalid.json` and `lossless-not-flac-invalid.json`, and stripped
  `lossless` from the other 11 track fixtures with a short Python script.
  `missing-original-invalid.json` now has `"audio": {}`.
- Added `audio-unknown-field-valid.json`: the minimal record plus a `lossless` blob labelled
  `audio/x-flac`, which is what a newer client might write. It passes. indigo's
  `validateObject` (`atproto/lexicon/validation.go`) checks the required keys and the declared
  properties and ignores any other keys, so fields added later do not break validation against
  this schema.
- Committed the lead's rewrite of the 2026-10-09 entry in `/docs/decisions.md` unchanged.

### Why

This review round had two gpt-6-astra reviews via codex, one resumed and one fresh.

- **The fresh review found `audio/x-flac`.** The lead checked `@atproto/pds` 0.4.x on unpkg
  (0.4.0, 0.4.100 and 0.4.150). Those releases depend on `file-type` ^16.5.4, which labels FLAC
  `audio/x-flac` (core.js, around line 570, at v16.5.4). The current 0.5.x releases use
  `file-type` ^22, which says `audio/flac`. RFC 9639 lists `audio/x-flac` as a deprecated
  alias. A PDS still running 0.4.x would have rejected every `lossless` blob under
  `accept: ["audio/flac"]`. My Step 2 check covered only the current PDS source.
- **Both reviews said a required FLAC made sets unpublishable.** A FLAC made from a lossy
  upload is many times larger than the upload, so long sets would go over the PDS blob limit.
- **The "about an hour, whatever the format" ceiling in the decision record was wrong.** At
  300 MiB, a CD-quality WAV runs out at about 29.7 minutes, and 24-bit/96 kHz stereo at about
  9 to 10 minutes.
- **Markus first made `lossless` optional.** His reasoning: app-view rules can change, but
  schema requirements cannot.
- **Then he dropped it.** The app view makes, stores and serves the playback copies itself. It
  can redo them at any time without the user's OAuth session.
- **A list of audio files tagged by role was considered and rejected.** That shape cannot
  require an original, and it cannot give each kind its own `accept` rules. Named optional
  fields can be added later just as easily.
- **Expected later:** an optional `lossless` accepting both `audio/flac` and `audio/x-flac`.

### What worked

Checking indigo before adding the unknown-field fixture: `validateObject` loops over
`s.Properties` only, so the fixture tests real behaviour, not an accident.

### What didn't work

The design changed twice, and some earlier figures were wrong:

- The decision record's "about an hour whatever the format" upload ceiling was wrong. The real
  figures are about 29.7 minutes for CD WAV and about 9 to 10 minutes for 24/96 stereo at
  300 MiB.
- Step 2 reported that `audio/flac` matched what the PDS sniffs, from the current source alone.
  That was true only for `@atproto/pds` 0.5.x. Older PDS versions still in use label FLAC
  `audio/x-flac`.
- The design went from a required `lossless` (Steps 1 to 3), to an optional one, to none. That
  undid the Step 1 decision that the record should always carry a playable file the app controls.

Nothing failed in the tests or lint during this step.

### What I learned

- What the PDS sniffs depends on the PDS version, not only on the current source, so an
  `accept` list must allow for every PDS version still running.
- indigo ignores unknown keys inside objects, which is what makes later optional fields safe
  for older readers.

### What was tricky

Keeping `#audio` as an object with a single field looks odd on its own. It stays so that encodings
can be added later as optional fields next to `original`, without a breaking change.

### What warrants review

- `/lexicons/com/audioadastra/track.json`: `#audio` has only `original`.
- `/lexicons/testdata/com/audioadastra/track/audio-unknown-field-valid.json` and its test row.
- `/docs/decisions.md`: the lead's rewritten 2026-10-09 entry.
- Validate with `go test -tags sqlite_fts5,sqlite_math_functions -shuffle on ./lexicons/...` and
  `golangci-lint run`.

### Future work

- The app view needs to make, store and serve the playback copies (upload feature, #6).
- Later: an optional `lossless` field accepting `audio/flac` and `audio/x-flac`.
- The login's `blob:audio/*` scope (#32) has the same version-dependent sniffing problem.
