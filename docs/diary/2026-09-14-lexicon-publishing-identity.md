# Diary: choosing the lexicon publishing account

Follow-up to `/docs/diary/2026-09-14-actor-profile-lexicon.md`. The lexicon is merged but not on
the network; this entry records who publishes it and the manual procedure.

## Step 1: pick the publishing account

**Author:** main

### Prompt Context

**Verbatim prompt:** "What account should the lexicons be published under?" then "NOT lognship",
"Let's use my own PDS. See @atproto.dk handle", and "1: new account / 2: Yes, mine".

**Interpretation:** decide which DID becomes the `com.audioadastra.*` namespace authority, and
where that account lives.

**Inferred intent:** keep the project's identity separate from Markus personally and from
lognship, on infrastructure he controls.

### What I did

Recorded the decision in `/docs/decisions.md`: a new `audioadastra.com` account on
`https://pds.atproto.dk` (the PDS behind `@atproto.dk`, `did:plc:7mrqxsbqyxhi3av7j3fccg3r`).
Nothing was created or published; that is Markus's manual procedure:

1. Create the account on `pds.atproto.dk` and set its handle to `audioadastra.com`
   (`_atproto.audioadastra.com` TXT `did=<new DID>`, or the `/.well-known/atproto-did` route).
2. Add TXT records `_lexicon.audioadastra.com` and `_lexicon.actor.audioadastra.com`, both
   `did=<new DID>`. `goat lex check-dns` prints the exact records and goes green when they resolve.
3. `goat account login` as the new account (goat was logged out of the lognship session and the saved
   `goat-profiles` were removed, so there is no other session to collide with), then `goat lex publish lexicons/` and
   `goat lex status` to confirm local and network copies match.

### Why

The DID in the `_lexicon` TXT record is the namespace authority; moving it later means repointing
DNS and re-publishing, so it should be a durable, project-owned identity from the start.

### What worked

Reading goat's `record.go` settled a misconception: goat sends `validate=true` on record writes
unless `-n` is given, which is why lognship saw `Unknown lexicon type` from a Bluesky PDS. The
XRPC default (unset) accepts unknown record types, so users on `bsky.social` are unaffected.

### What didn't work

Nothing failed; this step was discussion and lookups only.

### What I learned

`createRecord`/`putRecord` `validate` has three modes: `true` requires a known lexicon, `false`
skips validation, unset validates known lexicons only. Our app must therefore validate records
against the lexicon catalog itself before writing.

### What was tricky

Not over-generalising from lognship's goat experience to how the app will behave.

### What warrants review

The decision entry; the procedure above is untested until Markus runs it.

### Future work

Run the procedure. When the app writes records, validate them with the indigo lexicon catalog
before calling the PDS.

## Step 2: create the account and publish

**Author:** main

### Prompt Context

**Verbatim prompt:** "Help me create an account in the PDS", then "Success! DID:
did:plc:xj4bpglaht36jqc4dopoh3va Handle: audioadastra.atproto.dk", "Logged in, please verify", and
Markus ran `goat lex publish lexicons/com` himself.

**Interpretation:** walk the procedure from Step 1 for real, with Markus running anything that
needs an invite code or password.

**Inferred intent:** get `com.audioadastra.actor.profile` onto the network under the project's own
identity.

### What I did

`com.atproto.server.describeServer` on `https://pds.atproto.dk` showed `inviteCodeRequired: true`
and only `.atproto.dk` handles at signup, so the account was created as `audioadastra.atproto.dk`
(invite code from `docker exec pds pdsadmin create-invite-code` on the VPS; `goat account create`
run by Markus). Markus added three TXT records at Cloudflare, all `did=did:plc:xj4bpglaht36jqc4dopoh3va`:
`_atproto.audioadastra.com`, `_lexicon.audioadastra.com`, `_lexicon.actor.audioadastra.com`. I
verified them with `dig @1.1.1.1`, ran `goat account update-handle audioadastra.com` (the DID
document's `alsoKnownAs` is now `at://audioadastra.com`), and `goat lex check-dns lexicons/com`
reported all NSIDs resolved. Markus ran `goat lex publish lexicons/com` (green), and
`goat lex status lexicons/com` plus `goat lex resolve com.audioadastra.actor.profile` confirmed the
network copy matches `/lexicons/com/audioadastra/actor/profile.json`.

### Why

See Step 1 and `/docs/decisions.md`.

### What worked

Adding all three TXT records in one go meant a single propagation wait. Cloudflare propagated
within a minute.

### What didn't work

Bare `goat lex check-dns` failed with `error: unsupported lexicon language version: 0`. It defaults
to the whole `lexicons/` tree and tried to parse the record fixtures under `/lexicons/testdata/`
as schemas. Passing `lexicons/com` explicitly fixed it; the same applies to `goat lex publish` and
`goat lex status`.

### What I learned

The PDS only offers its own subdomain handles at signup; a custom-domain handle is a second step
via `update-handle` once `_atproto.<domain>` resolves. Your own PDS's `resolveHandle` reflects the
change immediately.

### What was tricky

Keeping the password out of the transcript: `goat account create` and `goat account login` were
run by Markus in his own shell; goat's session file is then shared with my shell.

### What warrants review

Nothing in code. On the network: `goat lex resolve com.audioadastra.actor.profile` should show the
same schema as the repo.

### Future work

Either move the record fixtures out of `lexicons/` or always pass `lexicons/com` to goat; a
Makefile target would remove the footgun. Any future lexicon under a new NSID group (e.g.
`com.audioadastra.feed.*`) needs its own `_lexicon.<group>.audioadastra.com` TXT record before
publishing.
