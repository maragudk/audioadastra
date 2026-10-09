# Diary: `make publish-lexicons`

A Makefile target that is the only way to publish `com.audioadastra.*` lexicons to the atproto
network, with safeguards. Prompted by the first attempt to publish `com.audioadastra.track`
going wrong twice in a row.

## Step 1: requirements

**Author:** main

### Prompt Context

**Verbatim prompt:** "We should have a Makefile target to do this, with proper safeguards, and only ever use that"

**Interpretation:** wrap `goat lex publish` in a Makefile target that cannot publish the wrong
files, from the wrong account or from unreviewed code, and make it the documented single path.

**Inferred intent:** publishing is permanent and network-visible, so it must not depend on
whoever happens to type the command or on goat's ambient state.

### What I did

After PR #30 merged, Markus ran the publish by hand. Two failures:

1. `goat lex publish` with no path failed with `error: unsupported lexicon language version: 0`.
   With no argument goat reads all of `lexicons/`, including `lexicons/testdata/`, whose record
   fixtures are not schemas. This was already recorded in
   `/docs/diary/2026-09-14-actor-profile-lexicon.md`, but that entry's publish procedure itself
   says `goat lex publish lexicons/`, which is wrong; it must be `lexicons/com`.
2. `goat lex publish lexicons/com` printed ` 🟠 com.audioadastra.actor.profile` and
   ` ⭕ com.audioadastra.track` and published nothing. From goat's `lex_publish.go`: 🟠 means a
   remote schema already exists and `--update` was not given; ⭕ means the NSID group's DNS
   resolves to a DID other than the logged-in account's. goat's saved session
   (`~/Library/Application Support/goat/auth-session.json`) was for `maragumusic.com`
   (`did:plc:5gupes45i7yehsjajrzdi37s`), while `_lexicon.audioadastra.com` and
   `_lexicon.actor.audioadastra.com` point at `audioadastra.com`
   (`did:plc:xj4bpglaht36jqc4dopoh3va`).

Agreed requirements with Markus:

1. Only from an up-to-date, clean `main`.
2. Lexicon tests run first; stop on failure.
3. Always `--username audioadastra.com`; app password asked for interactively with hidden input,
   never stored, never taken from goat's saved session.
4. Always `lexicons/com`.
5. `goat lex check-dns lexicons/com` must pass.
6. Print `goat lex status`, then ask "Publish? [y/N]".
7. Run `goat lex status` afterwards and fail unless everything is green.
8. A line in `CLAUDE.md`: lexicons are only ever published with this target.

New lexicons only: no `--update`. Updating published schemas (needed when an optional field such
as `lossless` is added to the track) comes later, together with a breaking-change check against
the published version.

### Why

Both failures were silent footguns: the first a confusing error, the second a "success" that
published nothing. A wrong account with matching DNS would have published under the wrong
identity.

### What worked

Reading goat's source for the status symbols, rather than guessing, explained the ⭕ at once.

### What didn't work

The two commands above, with their verbatim output.

### What I learned

goat's `lex publish` and `lex status` print emoji with no legend. 🟠 in `publish` means "exists
remotely, not updated"; in `status` it means out of sync with the network.

### What was tricky

Nothing beyond the above.

### What warrants review

That the safeguards hold when run for real: wrong branch, dirty tree, failing tests, declined
confirmation.

### Future work

Update path with a breaking-change check, when the first field addition comes.

## Step 2: build the target and break every safeguard on purpose

**Author:** builder

### Prompt Context

**Verbatim prompt:** "Add a `make publish-lexicons` target to Audio Ad Astra: the only sanctioned way to publish `com.audioadastra.*` lexicons to the atproto network. Then open a PR." (The full brief listed the nine requirements from Step 1, under a hard constraint: publish nothing, log in to nothing, never run `goat lex publish` without `--help`, and leave goat's saved session alone.)

**Interpretation:** a `.PHONY` Makefile target calling a `set -euo pipefail` bash script that runs
the checks in order, with every claim about goat's behaviour verified against its source rather
than assumed.

**Inferred intent:** make the boring path the only path, and make each way it can go wrong fail
loudly before anything permanent happens.

### What I did

Read goat's source at the installed revision (`goat version v0.2.5-rev-53ba4f9`, cloned to the
scratchpad): `lex_publish.go`, `lex_status.go`, `lex_check_dns.go`, `lex_util.go`,
`lex_util_files.go`, `auth.go` and `main.go`. Then wrote `/scripts/publish-lexicons.sh` and the
`publish-lexicons` target in `/Makefile` (between `lint` and `tailwindcss`, same comment-plus-
`.PHONY` style), and added the line to `/AGENTS.md` (`/CLAUDE.md` is a symlink to it).

The script, in order: requires goat and an interactive terminal; checks git state (branch `main`,
`origin` is `github.com/maragudk/audioadastra`, `git status --porcelain --untracked-files=all`
empty, no ignored files under `lexicons/`, no skip-worktree or assume-unchanged files under
`lexicons/`, `git fetch origin main` then `HEAD` equals `origin/main`); runs
`go test -tags sqlite_fts5,sqlite_math_functions -count 1 ./lexicons/...`; runs
`goat lex check-dns lexicons/com` and requires its exact success line; prints
`goat lex status lexicons/com` and allows only 🟢 and 🟠 lines (exits 0 with "Nothing to publish"
if there is no 🟠); resolves `audioadastra.com` and every listed NSID with `goat resolve --did` and
`goat lex resolve --did` and requires the same DID; asks `Publish? [y/N]`; asks for the app
password with echo off; re-checks the git state and that `HEAD` has not moved; runs
`GOAT_PASSWORD=... goat lex publish --username audioadastra.com lexicons/com`; fails on any ⭕ in
its output; and finally requires every `goat lex status lexicons/com` line to be 🟢.

To test without publishing, I used a fake `goat` first on `PATH` (in the scratchpad) that passes
`lex status`, `lex check-dns`, `lex resolve` and `resolve` through to the real goat, prints its
argv and the length of `GOAT_PASSWORD` for `lex publish`, and refuses everything else. Copies of
the script with the repo-state checks disabled, or with the branch name and origin ref swapped for
this branch, were driven through a scratch makefile under `expect` (for a real pty). The real
`goat lex publish` was never run.

Safeguards exercised, each with the failure it produced:

- Not a terminal (`make publish-lexicons` from a non-tty): "must be run interactively, from a terminal".
- Wrong branch (real target, under `script` for a pty): "must be on branch main, not 'worktree-publish-lexicons-target'".
- Dirty tree: lists `M AGENTS.md`, `?? scripts/publish-lexicons.sh` and so on, then "working tree is not clean".
- Ignored file: `lexicons/com/test-x.db.json`, matched by the root `.gitignore`'s unanchored `test-*.db*`: "ignored files under lexicons/ would be published".
- Skip-worktree: `git update-index --skip-worktree lexicons/com/audioadastra/track.json` gave "files under lexicons/ are marked skip-worktree or assume-unchanged" (undone with `--no-skip-worktree`).
- Not equal to origin: "local main (388ea6f) is not origin/main (6866c1b)".
- Failing tests: a broken `track/minimal-valid.json` fixture gave `FAIL app/lexicons` and "lexicon tests failed".
- Unresolved DNS: a temporary `lexicons/com/audioadastra/nope/thing.json` gave goat's "Some lexicon NSIDs did not resolve via DNS" text and "not all lexicon NSIDs resolve via DNS".
- Changed published lexicon: an edited description in `actor/profile.json` gave ` 🟣 com.audioadastra.actor.profile` and a refusal before the prompt.
- DNS pointing at another account (fake account DID): "com.audioadastra.actor.profile resolves to did:plc:xj4bpglaht36jqc4dopoh3va, not to audioadastra.com (did:plc:5gupes45i7yehsjajrzdi37s)".
- Declined confirmation with `n`, empty input and `yes`: "aborted, nothing published".
- Empty password: "no app password given, nothing published"; the fake publish never ran.
- Ctrl-C at the password prompt: the `EXIT` trap ran and the terminal had `echo` back on.
- Tree dirtied while the password prompt waited (`expect` created `lexicons/com/audioadastra/sneaky.json`): the re-check failed with "working tree is not clean" before publishing.
- Fake publish: argv was `lex publish --username audioadastra.com lexicons/com`, `GOAT_PASSWORD` was set with the right length (17 for a password with leading and trailing spaces), and the password was not in argv.
- Fake publish printing ⭕: "some lexicons were skipped because their DNS does not point at audioadastra.com".
- Fake publish exiting 1 after printing a 🟢 line: the line is shown, then "goat lex publish failed, check the lines above for anything already published".
- After the fake publish, the real status still showed ` 🟠 com.audioadastra.track`: "not every lexicon is in sync after publishing".

All runs were under macOS `/bin/bash` 3.2.57, which is also the `bash` that `make` finds here.

### Why

Every check maps to one of the Step 1 requirements, plus the gaps that reading goat's source
exposed (below). Testing through a fake goat was the only way to exercise the steps after the
confirmation without touching the network or goat's session.

### What worked

Reading goat's source answered every behavioural question at once. Running the read-only commands
against a temporary unresolvable lexicon confirmed the exit codes: `goat lex check-dns` printed
"Some lexicon NSIDs did not resolve via DNS" and exited 0; `goat lex status` printed
` 🟠 com.audioadastra.nope.thing` plus `WARN skipping NSID pattern which did not resolve` on
stderr and exited 0. `goat lex resolve --did com.audioadastra.nope.thing` does fail properly
("error: NSID not associated with a DID", exit 255).

`expect` with `-ex` patterns was a reliable way to drive the prompts in a real pty.

### What didn't work

The first version used `read -r -s -p "App password ..."`. Driven by `expect`, the password showed
up in the output (`App password for audioadastra.com: fake-secret-123`). With a one-second delay
before sending, it did not. bash prints the `-p` prompt before it turns echo off, so input that
arrives as the prompt appears is echoed. The fix turns echo off with `stty -echo` (and an `EXIT`
trap to restore it) before printing the prompt.

Expect's first attempt hung at the prompt: `"Publish? \[y/N\] "` is a glob, so `?` and `[y/N]`
were wildcards. `-ex` (exact match) fixed it.

Piping answers through `script` (`printf 'Y\n\n' | script -q /dev/null make ...`) aborted at the
confirmation even for `Y`; the input did not reach `read` reliably. I switched to `expect`.

### What I learned

- goat exit codes: `lex check-dns` and `lex status` exit 0 whatever they find; `lex publish` exits
  0 when it skips schemas (🟠 or ⭕). Only real errors (bad schema files, network, auth) exit
  non-zero (255). So all three are parsed.
- Symbols in `lex status`: 🟢 identical to published, 🟠 not published yet, 🟣 differs from
  published, ⭕ published but no local file. In `lex publish`: 🟢 newly published, 🟣 updated
  (only with `--update`), 🟠 exists remotely and skipped (printed for every existing schema,
  including identical ones, so it cannot be treated as an error), ⭕ group DNS points at another
  DID and skipped.
- Session handling (`auth.go`): when both `--username` and the password are set,
  `loginOrLoadAuthClient` calls `atclient.LoginWithPassword` with a nil refresh callback, so the
  session is ephemeral and `auth-session.json` is neither read nor written. But if the password
  is empty, it silently falls back to the saved session, which here belongs to another account.
  The script refuses an empty password for that reason.
- goat autoloads `.env` from the working directory (`github.com/joho/godotenv/autoload` in
  `main.go`), without overriding variables that are already set. The script exports
  `ATP_PLC_HOST=https://plc.directory` and unsets the username and password variables, so the
  app's `.env` cannot steer goat. The `GOAT_PASSWORD` prefix wins over an `ATP_PASSWORD` from
  `.env` because goat checks `GOAT_PASSWORD` first.
- `goat lex check-dns` only checks that each NSID group resolves to some DID, not to the account
  that publishes. That gap is what produced the ⭕ in Step 1.

### What was tricky

`goat lex publish` handles each NSID separately: with one group's DNS pointing at the right
account and another at the wrong one, it publishes the first and skips the second, still exiting
0. That partial publish cannot be undone, so the DID comparison has to happen before the prompt,
not only in the status check afterwards.

The session's worktree isolation refused some shell constructs (`sed -f`, `bash file.sh`, and
`git` inside complex commands), so the test variants were edited with the Edit tool and run
through a scratch makefile.

### What warrants review

Self-review (`/code-review` at high effort) found nine issues. I fixed seven:

1. The DNS preflight did not compare each group's DID with the account's DID, which made a partial
   publish possible. Fixed with the `goat resolve --did` and `goat lex resolve --did` comparison.
2. The tree could change between the first checks and the publish. Fixed by re-running the
   git-state checks and requiring `HEAD` to be unchanged right before `goat lex publish`.
3. Captured publish output was dropped when goat failed partway through. It is now printed.
4. Skip-worktree and assume-unchanged files hid local edits from `git status`. Now refused.
5. `read -r password` trimmed surrounding whitespace. Now `IFS= read -r`.
6. `origin` was trusted blindly. Now it must be `github.com/maragudk/audioadastra` (ssh or https).
7. A failing `git fetch` gave no `publish-lexicons:` message. Now it does.

Two I left as they are. Publishing from a `git archive` snapshot of the verified commit would
remove the whole class of working-tree drift, but the tests run in the working tree too, and the
re-check right before publishing closes the gap in practice. The duplicated build tags
(`sqlite_fts5,sqlite_math_functions`) are already copied between the `test` and `benchmark`
targets, so I followed that.

To validate: `make publish-lexicons` on this branch should fail at the branch check. On a clean,
up-to-date `main` today, it should print ` 🟢 com.audioadastra.actor.profile` and
` 🟠 com.audioadastra.track`, show both NSIDs resolving to `did:plc:xj4bpglaht36jqc4dopoh3va`,
and then ask `Publish? [y/N]`.

### Future work

The first real run, by Markus, publishes `com.audioadastra.track`. The update path with a
breaking-change check (Step 1) is still to come; when it lands, the 🟣 refusal before the prompt
is where `--update` would be allowed.
