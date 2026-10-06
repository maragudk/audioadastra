# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Two primary groups, of equal weight:

- Musicians sharing their own work: independent artists and producers who publish tracks, demos and sets, and want listeners to follow them.
- Listeners discovering music: people who come to listen, browse and follow artists.

Both log in with an existing atproto account (for example the one they use for Bluesky).

## Product Purpose

Audio Ad Astra is a web app for sharing audio and music in the ATmosphere ("Music into the atmosphere!"). It lets musicians publish audio and lets listeners find and play it, with all data living on the open AT Protocol network.

## Positioning

- Audio lives in your own repo: tracks are blobs and records in the user's own PDS, so they own the data and can take it anywhere.
- The social graph is portable: identity and follows come from the user's existing atproto account; there is no new account to create.
- Open app view, any client: the `com.audioadastra.*` lexicons are public, so other apps can read and play the same audio. Audio Ad Astra is one client among many.
- No algorithmic feed and no ads: feeds are chronological or user-chosen, not engagement-optimized.

## Operating Context

- Login is atproto OAuth only; the user's DID is their identity. First login writes a `com.audioadastra.actor.profile` record to the user's repo.
- Users can be on any PDS, including Bluesky's.
- Lexicons are published from the `audioadastra.com` account.

## Capabilities and Constraints

- Shipped so far: front page, atproto OAuth login, profile page with logout. Audio upload, playback, feeds and following are not built yet.
- Stack: Go, server-rendered HTML with gomponents, Datastar for interactivity, Tailwind CSS. No SPA framework; keep JavaScript light.
- Playback must continue across page navigation. A persistent player is a requirement, not a nice-to-have.
- Profile record fields: display name, description, website.
- The project is open source on GitHub.

## Brand Commitments

- Name: Audio Ad Astra.
- Voice: playful, as in the README ("Made with sparkles"). Tagline: "Listen to the stars of the open sky." ("Music into the atmosphere" was temporary.)
- "Stars" means three things at once: music stars, real stars, and the sparkle of glamour and party. Sparkly is welcome.
- Feel: fun and slightly quirky, with neon/bold colors chosen from the Tailwind CSS default color palette (binding, set by the owner).
- Type: the owner wants a face with visible personality (a plain sans reads as "any other sans serif"), but not a handwriting style.
- Logo: a white hand-cut five-point star on a pink-600 square (`assets/logo.svg`, chosen by the owner from a reference), used for the favicons and web app icons. The old ear logo is retired. The header carries no logo.
- Official links: Bluesky `@audioadastra.com`, GitHub `maragudk/audioadastra`.

## Evidence on Hand

- No users, tracks, testimonials, or usage numbers exist yet. Do not fabricate artists, tracks, play counts, or quotes.

## Product Principles

1. The user owns their audio: every feature keeps data in the user's repo, readable by other clients.
2. Listening is uninterrupted: navigation never stops the music.
3. No manipulation: no ads and no engagement-optimizing algorithm; the user chooses what they see.
4. Fun is a feature: the product should feel playful, not corporate.
5. Light and fast: server-rendered pages with minimal JavaScript.
