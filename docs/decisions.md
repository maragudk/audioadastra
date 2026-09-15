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
