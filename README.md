# Audio Ad Astra

[![Docs](https://pkg.go.dev/badge/maragu.dev/audioadastra)](https://pkg.go.dev/maragu.dev/audioadastra)
[![CI](https://github.com/maragudk/audioadastra/actions/workflows/ci.yml/badge.svg)](https://github.com/maragudk/audioadastra/actions/workflows/ci.yml)
[![CD](https://github.com/maragudk/audioadastra/actions/workflows/cd.yml/badge.svg)](https://github.com/maragudk/audioadastra/actions/workflows/cd.yml)

Music into the atmosphere!

## Local atproto network

Manual end-to-end login runs against a PLC directory and a PDS in docker compose on this machine. Nothing in it touches the real atproto network, and the automated tests do not need it; they use in-process fakes.

`docker-compose.yml` defines four services: `plc-db` (Postgres), `plc` (the PLC directory, `ghcr.io/bluesky-social/did-method-plc`), `pds` (the real PDS image `ghcr.io/bluesky-social/pds:0.4` in dev mode), and `caddy` (a TLS reverse proxy configured in `docker/caddy/Caddyfile`). Data persists under `local/`, which git ignores.

Caddy is there because OAuth clients only talk to auth servers over https with no port number. The PDS is reached as `https://pds.localhost` through Caddy on port 443, with a certificate from Caddy's own local CA; the root certificate lands at `local/caddy/caddy/pki/authorities/local/root.crt`. Ports 443 and 2582 on the host must be free; both are bound to 127.0.0.1 only. The PLC image is amd64 only, so on Apple silicon it runs under emulation, which is fine for a handful of requests.

Handles live under `.test` (for example `alice.test`), the one reserved TLD atproto allows for development. Nothing resolves `.test` on the host, so with `ATPROTO_LOCAL_HANDLE_SUFFIX=.test` the app dials `.test` hosts on loopback; handle login and handle display then work without editing `/etc/hosts`. Browsers resolve `pds.localhost` on their own.

1. Start the network. The first run pulls images.

   ```
   make atproto-up
   ```

2. Create the account `alice.test` on the PDS. Invite codes are off, and the admin password is `admin`. The command prints JSON including the account's `did`.

   ```
   make atproto-account HANDLE=alice PASSWORD=alice-password
   ```

3. Run the app with these settings, in `.env` or on the command line in front of `make watch`:

   ```
   BASE_URL=http://127.0.0.1:8080
   ATPROTO_PLC_URL=http://localhost:2582
   ATPROTO_CA_FILE=local/caddy/caddy/pki/authorities/local/root.crt
   ATPROTO_LOCAL_HANDLE_SUFFIX=.test
   ```

   A `BASE_URL` on localhost or 127.0.0.1 puts the app in localhost OAuth mode, which needs no client key. The OAuth callback is always on `127.0.0.1`, as the spec requires, so browse the app at `http://127.0.0.1:8080` rather than `http://localhost:8080` to keep the session cookie on one host.

4. Open `http://127.0.0.1:8080/login`, enter `alice.test` (or the DID), sign in on the PDS page with the password, and click Authorize. You are back on the front page, logged in. "Log out" revokes the tokens at the PDS and clears the session.

5. Stop the containers with `make atproto-down`. Delete `local/` to start from scratch.

Gotchas:

- The browser must trust Caddy's local root certificate to show the PDS sign-in page. Import `root.crt` into the browser or OS trust store, or start the browser with a flag that ignores certificate errors for testing.
- `curl` on macOS refuses the `*.test` wildcard certificate for handle hostnames even with `--cacert`; Go accepts it. Test handle resolution through the app, not curl.
- The PLC root URL redirects browsers to the public PLC web UI. That is only a redirect, not a network dependency.

Outside localhost mode the app is a confidential OAuth client and refuses to start without `OAUTH_PRIVATE_KEY` (a P-256 private key in multibase encoding; generate one with `goat key generate -t P-256 --terse`) and `OAUTH_KEY_ID` (any short name for the key). The client metadata is served at `/oauth/client-metadata.json` and the public key at `/oauth/jwks.json`.

Made with ✨sparkles✨ by [maragu](https://www.maragu.dev/): independent software consulting for cloud-native Go apps & AI engineering.

[Contact me at markus@maragu.dk](mailto:markus@maragu.dk) for consulting work, or perhaps an invoice to support this project?
