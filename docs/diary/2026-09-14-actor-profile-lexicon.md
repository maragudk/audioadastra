# Diary: `com.audioadastra.actor.profile` lexicon

First atproto artefact in Audio Ad Astra: the account profile record lexicon, modelled on
`com.lognship.actor.profile` in the sibling lognship project. Lexicon-only -- no OAuth, no profile
page, no indexing. Along with the JSON, this brings in the `goat lex` lint targets and a Go test
that validates fixtures against the lexicon with indigo.

## Step 1: shape the requirements

**Author:** main

### Prompt Context

**Verbatim prompt:** "Let's build com.audioadastra.actor.profile like in ../lognship" followed by
"Yes, do 1-4, but let's discuss what actually goes into the profile, one at a time."

**Interpretation:** replicate lognship's profile lexicon slice -- the JSON, Makefile lint, the
validation test, and the manual publish flow -- under the `com.audioadastra` namespace, but
decide every field deliberately rather than copy blindly.

**Inferred intent:** get the first lexicon into the repo with the discipline (lint, tests,
publishing notes) that later lexicons will follow, and pick field limits that won't need a
breaking change later.

### What I did

Sent an explorer through `/Users/maragubot/Developer/lognship`. Finding: lognship's
`actor.profile` is lexicon-only. There is no Go code for it -- no table, model, route, view or
OAuth (lognship auth is email magic-link via glue). What exists is
`lexicons/com/lognship/actor/profile.json`, `goat lex parse`/`goat lex lint` in the Makefile
`lint` target, and `lexicons/lexicons_test.go` validating JSON fixtures with
`lexicon.NewBaseCatalog()` + `lexicon.ValidateRecord` from indigo (no profile fixtures there).
Publishing is manual: `goat lex check-dns` -> DNS TXT records -> `goat lex publish lexicons/` ->
`goat lex status`.

Then walked the fields with Markus one at a time. Decisions:

- `displayName`: optional, `maxGraphemes: 64`, `maxLength: 640`. Kept at 64 because it is
  rendered in tight spots (track rows, player bar) and 64 already exceeds Bluesky/Mastodon/SoundCloud.
- `description`: optional, `maxGraphemes: 1000`, `maxLength: 10000`. Raised from lognship's 256
  because loosening a limit later is a breaking change (clients holding the old schema reject the
  longer record), so the safe move is to be generous up front.
- `website`: optional, `format: uri`. Single link; a links array can be added later as an optional
  field.
- `avatar`: left out. Adding an optional blob field later is non-breaking.
- `createdAt`: required datetime, "Client-declared timestamp when this account profile was
  originally created."
- Record key `literal:self`.

### Why

The profile is the anchor record for an account and (as in lognship's Tap setup) the natural
"signal collection" for deciding which DIDs an app view tracks. It has to exist before OAuth,
uploads or indexing can be designed against it.

### What worked

Reading lognship's diaries (`2026-05-04-first-records-on-the-atmosphere.md` especially) gave the
publish flow and the PDS `Unknown lexicon type` gotcha without having to rediscover them.

### What didn't work

Nothing failed at this stage; it was reading and discussion only.

### What I learned

CLAUDE.md already claims `lexicons/` exists; it did not until this feature. The repo had no indigo
dependency at all.

### What was tricky

Being clear that "like in lognship" meant a much smaller slice than it sounds -- no Go beyond a
test.

### What warrants review

The field limits above are the durable decisions; everything else is mechanical.

### Future work

Publish the lexicon to the network (manual: DNS + `goat lex publish`). OAuth login and a profile
page are separate features.

## Step 2: build the lexicon, lint target and validation test

**Author:** profile-lexicon-builder

### Prompt Context

**Verbatim prompt:** "Add the first lexicon to Audio Ad Astra, `com.audioadastra.actor.profile`, plus the
lint and test scaffolding that later lexicons will reuse. Lexicon-only: no OAuth, no models, no
tables, no routes, no views." (Full brief listed four deliverables: the JSON verbatim, the Makefile
`lint` lines, `lexicons/lexicons_test.go` with valid/invalid fixtures, and docs/diary notes,
under a hard constraint of publishing nothing to the network and not committing.)

**Interpretation:** land the exact JSON the lead settled on, wire `goat lex parse`/`goat lex lint`
into `make lint`, and add an indigo-backed table-driven test over JSON fixtures, following the
project's Go test conventions.

**Inferred intent:** make the lexicon directory real (CLAUDE.md already claimed it existed) with
lint and test discipline in place before a second lexicon shows up, so later ones just add a JSON
file and fixtures.

### What I did

- Wrote `/lexicons/com/audioadastra/actor/profile.json` byte-for-byte as specified.
- Extended the Makefile `lint` target with a `find | xargs -0 -r goat lex lint` line (see "What
  warrants review" for why `-r` was added, and "What I learned" for why there is no separate
  `goat lex parse` line).
- Added `github.com/bluesky-social/indigo` with `go get ...@latest` and `go mod tidy`. indigo has
  no tagged releases (`go list -m -versions` prints none), so it pins to the pseudo-version
  `v0.0.0-20260903211445-41278964ec8e`. The pull brought in about thirty indirect modules (ipfs,
  multiformats, prometheus, secp256k1) and bumped grpc/protobuf/x-net; unavoidable when importing
  `atproto/lexicon`.
- Wrote `/lexicons/lexicons_test.go` (package `lexicons_test`, `maragu.dev/is`, "should ..."
  subtests) which loads `lexicons/com` into `lexicon.NewBaseCatalog()` and runs
  `lexicon.ValidateRecord` over eight fixtures in
  `/lexicons/testdata/com/audioadastra/actor/profile/`: full valid (DJ Voyager, still on tour
  since 1977), minimal valid, and six invalid ones (missing `createdAt`, 69-grapheme
  `displayName`, 1039-grapheme `description`, non-URI `website`, non-datetime `createdAt`,
  missing `$type`). Invalid cases assert a substring of the error so each fixture is known to fail
  for the reason its name claims.
- Skipped lognship's `testdata/com/atproto` base schemas: the profile references no
  `com.atproto.*` defs, and `datetime`/`uri` formats are validated by indigo itself. Add them
  when a lexicon uses `com.atproto.repo.strongRef` or similar.
- Left `CLAUDE.md`/`AGENTS.md` untouched; their claim that lexicons live at `lexicons/` is now true.

### Why

Every later lexicon reuses this shape: drop a JSON under `lexicons/com/audioadastra/...`, add
fixtures under `lexicons/testdata/...`, add rows to the table. `make lint` catches schema
mistakes before publish; the test catches fixture/record drift in CI, where goat is not installed.

### What worked

Dumping the actual `ValidateRecord` error per fixture from a scratch module before trusting the
green run. It confirmed indigo's exact messages, which then became the asserted substrings:
`required field missing: createdAt`, `string length (graphemes) outside specified range: 69`,
`URI syntax didn't validate via regex`, `Datetime syntax didn't validate via regex`, and for the
missing-`$type` fixture `record data missing $type, or didn't match expected NSID`. So yes:
**indigo enforces `$type` on `ValidateRecord`** with default flags, and the test covers it.

### What didn't work

- `is.NotNil(t, err)` does not compile for an `error`: `in call to is.NotNil, type error of err
  does not match *T (cannot infer T)`. `is.NotNil` takes a `*T`; the idiom for errors is
  `is.True(t, err != nil, ...)`.
- `make lint` fails, but not on this work: `html/common.go:79:6: func a is unused (unused)`. That
  file is untouched here and the failure exists on the branch already; left alone as out of
  scope. Running the goat line directly exits 0 (`goat lex lint`: green; `goat lex parse` also
  reported `success` before that line was dropped).

### What I learned

- `goat lex lint` accepts a directory and recurses, and it already runs a `schema-json-parse`
  rule, so a separate `goat lex parse` line is belt-and-braces. The brief asked for both;
  both reviewers flagged the redundancy, and Markus decided to drop the parse line and keep only
  `goat lex lint`.
- Bare `goat lex lint` (no path) defaults to `lexicons/` and walks into `lexicons/testdata/`,
  where the record fixtures fail as schemas (`unsupported lexicon language version: 0`). Always
  pass `lexicons/com` (or the audioadastra subdirectory) to goat commands, as the Makefile does.
- CI's lint job (`maragudk/workflows/lint.yml`) runs `golangci-lint-action` directly, never
  `make lint`, so the goat checks are local-only. The Go test is what runs in CI.

### What was tricky

The `find | xargs` idiom copied from lognship differs across platforms when `find` matches
nothing: BSD xargs runs nothing, GNU xargs runs `goat` with no arguments (`parse` exits 255,
`lint` falls back to the default directory and goes red on the fixtures). Both reviewers flagged
it. `-r` (accepted by both BSD and GNU xargs) makes the empty case a no-op everywhere; verified
locally with a non-matching pattern.

### What warrants review

- Makefile: the `-r` on `xargs` is the one deviation from the lines in the brief.
- `/lexicons/lexicons_test.go`: the `nsid` column was dropped in favour of the literal at the
  `ValidateRecord` call (both reviewers: identical in every row). It comes back as a column when
  the second lexicon lands.
- Fixture directory lives under `lexicons/testdata/` like lognship. One reviewer suggested moving
  it out of `lexicons/` so bare `goat lex ...` invocations don't trip on it; not done, to stay
  consistent with lognship.
- Validate: `go test -tags sqlite_fts5,sqlite_math_functions -shuffle on ./lexicons/...` and the
  goat line from the Makefile.

### Future work

Manual publish procedure for Markus (not run here; nothing was published):

1. `goat lex check-dns` -- prints the TXT records needed.
2. Add DNS TXT records `_lexicon.audioadastra.com` and `_lexicon.actor.audioadastra.com`, each
   with value `did=<publishing DID>`; re-run `goat lex check-dns` until it reports all NSIDs
   resolved.
3. `goat lex publish lexicons/` -- writes `com.atproto.lexicon.schema` records into the publishing
   account's repo.
4. `goat lex status` -- should show green for `com.audioadastra.actor.profile`.

Gotcha from lognship: bsky-hosted PDSes validate against a hardcoded allowlist and reject unknown
types with `HTTP 400: InvalidRequest: Unknown lexicon type: ...` even after publishing, so record
writes need `goat record create -n` (`--no-validate`). Guard locally first with
`goat lex validate -c lexicons <record.json>`.

## Step 3: lint lexicon schemas in tests with `lexlint`

**Author:** profile-lexicon-builder

### Prompt Context

**Verbatim prompt:** "yes" (to the proposal "a second test in lexicons_test.go that runs
lexlint.LintSchemaFile over every schema and fails on any issue -- goat in CI for free"), with
two follow-up adjustments relayed by the coordinator: drop the negative-case fixture and test
("we don't want to test `LintSchemaFile` itself"), and walk `lexicons/` itself rather than
`lexicons/com/` so future namespaces are picked up without editing the test.

**Interpretation:** replicate what `goat lex lint` does -- `lexlint.LintSchemaFile` plus goat's
strict `DisallowUnknownFields` decode -- as a Go test over every schema file, failing on any
issue at any level, and make both tests discover schemas by walking the package directory.

**Inferred intent:** close the CI gap. CI's lint job runs `golangci-lint-action` directly, never
`make lint`, so goat's style rules only ran on developer machines. The catalog load in
`TestLexicons` only catches parse errors; it says nothing about missing descriptions,
unbounded strings, or misspelled constraint keys.

### What I did

- Added `TestLexiconSchemas` to `/lexicons/lexicons_test.go`. For each schema it calls
  `FinishParse`, runs `lexlint.LintSchemaFile`, then decodes the raw JSON again with
  `json.Decoder.DisallowUnknownFields` and appends an `unexpected-field` warn issue on error
  (the level and name goat v0.2.4 emits). Any issue fails the subtest with
  `[level] lint-name: message`.
- Added a `readSchemaFiles` helper that walks `.` with `filepath.WalkDir`, returns
  `fs.SkipDir` for `testdata`, and parses every `*.json` into a `lexicon.SchemaFile` (raw bytes
  kept alongside). Both tests use it; `TestLexicons` now loads the catalog with
  `cat.AddSchemaFile` per file instead of `cat.LoadDirectory("com")`, since `LoadDirectory`
  has no way to skip `testdata` and would parse the record fixtures as schemas.
- Makefile `lint` line is now
  `find lexicons -name '*.json' -not -path '*/testdata/*' -print0 | xargs -0 -r goat lex lint`,
  consistent with the walk. Green locally.
- `go mod tidy -diff` is clean: `lexlint` lives in the indigo module we already require and
  pulls nothing new.

### Why

The files are the table. Dropping a new schema anywhere under `lexicons/` (outside `testdata`)
gets it linted and loaded into the catalog with no test edits, and CI now enforces the same
rules as `make lint` without needing the goat binary.

### What worked

Reading goat's `lex_lint.go` (fetched read-only from `github.com/bluesky-social/goat` at
v0.2.4, since this indigo snapshot no longer ships goat under `cmd/`) gave the exact
`unexpected-field` shape to replicate. Two competing reviewers over the diff caught a latent
bug before it could bite (see "What was tricky").

### What didn't work

`is.NotNil` still does not take an `error`, so the lint subtest asserts with `t.Errorf` per
issue rather than an `is` call; there is nothing to compare against but "no issues".

### What I learned

- goat's `unexpected-field` check only catches unknown **top-level** keys. `SchemaDef` has a
  custom `UnmarshalJSON` that decodes nested defs with plain `json.Unmarshal`, so the outer
  decoder's `DisallowUnknownFields` never reaches a property. Verified locally before the
  negative case was dropped: `goat lex lint` on a scratch schema with `"maxGrapheme": 64`
  reported only `[unlimited-string]: no max length` for the other field and said nothing
  about the typo. The test replicates goat faithfully and has the same blind spot; the comment
  in the test says "top-level" so nobody relies on it for nested typos.
- `lexlint` has no `info`-level rules, so failing on every level equals goat's default
  `--lint-level warn`.
- `lexlint.LintSchemaFile` on the profile lexicon reports nothing, matching goat.

### What was tricky

`lexicon.SchemaFile.FinishParse` is not idempotent. `AddSchemaFile` calls it internally, and
my first version of `readSchemaFiles` had already called it, so unions would get their
`fullRefs` appended twice and `AddSchemaFile` would fail with `union refs were not expanded`.
It passed only because the profile has no union. Both reviewers found it independently; I
reproduced it in a scratch module (`FinishParse` then `AddSchemaFile`: `union refs were not
expanded`; `AddSchemaFile` alone: `<nil>`). Fix: the helper only unmarshals; the lint subtest
calls `FinishParse` on its own copy, and the catalog test lets `AddSchemaFile` do it. The
helper's doc comment records the contract.

### What warrants review

- `/lexicons/lexicons_test.go`: `readSchemaFiles` fails fast on the first unparseable file
  (both tests then fail with the same error) rather than reporting it per file like goat's
  `schema-json-parse` rule. Both reviewers called it acceptable; flagging in case per-file
  reporting is preferred.
- Subtests are named by NSID (`should lint com.audioadastra.actor.profile without issues`)
  rather than path, so `-run` does not split on `/`.
- Validate: `go test -tags sqlite_fts5,sqlite_math_functions -shuffle on ./lexicons/...` and
  `make lint` (still fails on the pre-existing `html/common.go` unused-function issue, which is
  out of scope).

### Future work

If nested-typo detection is ever wanted, it needs a recursive walk of the raw JSON decoding
each typed node strictly; goat does not do this today either.

## Step 4: address review feedback on PR #2

**Author:** profile-lexicon-builder

### Prompt Context

**Verbatim prompt:** three review comments on the PR: "maybe remove this line? Irrelevant to
the test case?" (on `displayName` in the missing-`createdAt` fixture), "Check that this is the
newest version" (on the indigo pseudo-version in `go.mod`), and "Do we still even need this now
that it's in the test itself?" (on the `goat lex lint` line in the Makefile).

**Interpretation:** trim the fixture to the minimum that exercises the case, confirm the indigo
pin is current, and drop the Makefile line since `TestLexiconSchemas` runs the same rules.

**Inferred intent:** keep fixtures focused on one defect each, and avoid two mechanisms that
check the same thing.

### What I did

- `/lexicons/testdata/com/audioadastra/actor/profile/missing-created-at-invalid.json` is now
  only `{"$type": "com.audioadastra.actor.profile"}`.
- Makefile `lint` target reverted to exactly what it is on `main`: `golangci-lint run`.
- Verified the indigo pin: `main` HEAD is `41278964ec8e`, which is the pinned pseudo-version
  `v0.0.0-20260903211445-41278964ec8e`. No change.

### Why

`TestLexiconSchemas` runs `lexlint.LintSchemaFile` plus the strict unknown-field decode over
every schema, which is what `goat lex lint` did, and it runs in CI where goat is not
installed. A fixture with a second, irrelevant field muddies what the case is about.

### What worked

Small, mechanical changes; tests and lint stayed green.

### What didn't work

Nothing failed.

### What I learned

The Makefile line was scaffolding that outlived its purpose within the same PR. Once the Go
test carried the rules, the shell line only duplicated them on developer machines.

### What was tricky

Nothing.

### What warrants review

The Makefile diff against `main` should now be empty.

### Future work

None beyond the publish procedure recorded in Step 2.
