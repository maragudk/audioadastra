# Diary: Fix govulncheck findings (issue #27)

The `Security` workflow's govulncheck run failed (https://github.com/maragudk/audioadastra/issues/27). The goal is the smallest possible fix: upgrade only the dependencies (or Go toolchain version) that govulncheck reports as vulnerable, and nothing else.

## Step 1: Bump `golang.org/x/net` to v0.60.0

**Author:** vulnfix

### Prompt Context

**Verbatim prompt:** Task: fix GitHub issue https://github.com/maragudk/audioadastra/issues/27 with the SMALLEST POSSIBLE FIX. The `Security` CI workflow (govulncheck) failed: https://github.com/maragudk/audioadastra/actions/runs/37899875502
**Interpretation:** Find what govulncheck flags in CI and upgrade only the affected module to the minimum fixed version.
**Inferred intent:** Get the `Security` workflow green again with no unrelated churn.

### What I did

Read `/.github/workflows/security.yml`, which calls a shared reusable workflow that installs Go with `go-version: stable` and `check-latest: true`, then runs `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`. The failed CI log shows Go 1.27.2 and five findings, all in `golang.org/x/net@v0.58.0`, all fixed in v0.60.0: GO-2026-6617, GO-2026-6612, GO-2026-6611, GO-2026-6610, GO-2026-6603. I ran `go get golang.org/x/net@v0.60.0` and `go mod tidy`, which changed `/go.mod` and `/go.sum`.

### Why

The findings are in a dependency, not the standard library, so the fix is a module bump, not a `go` directive change. CI always uses the latest Go, so the directive does not matter here.

### What worked

With `GOTOOLCHAIN=go1.27.2` (matching CI), govulncheck reports "Your code is affected by 0 vulnerabilities". `go build ./...` passes.

### What didn't work

My local toolchain is go1.27.1, and the first local govulncheck run also reported 12 standard library findings (for example GO-2026-6613, GO-2026-6600), all fixed in go1.27.2. That is a local artifact; CI already runs 1.27.2. I did not touch the `go` directive.

`go test -shuffle on ./...` fails in `/cmd/app` `TestLogin` with `local atproto network is not up, run make test-up: copying the proxy root certificate: exit status 1: no container found for service "caddy"`. That test needs the Docker test network from `make test-up`, which is not running here. All other packages pass.

### What I learned

`go get golang.org/x/net@v0.60.0` also pulls `golang.org/x/crypto` (v0.57.0), `golang.org/x/sys` (v0.48.0), `golang.org/x/text` (v0.42.0) and `golang.org/x/sync` (v0.23.0) up, because v0.60.0 requires them. These are minimum-version-selection side effects, not extra upgrades.

### What was tricky

Telling the CI findings apart from local-only ones: the local toolchain was one patch release behind CI.

### What warrants review

The `/go.mod` and `/go.sum` diff: x/net plus the four forced companion bumps. Three more vulnerabilities in required modules remain that govulncheck says the code does not call; they are outside the CI failure and left alone.

### Future work

None.
