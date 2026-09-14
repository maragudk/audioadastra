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
