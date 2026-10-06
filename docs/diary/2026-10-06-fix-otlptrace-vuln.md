# Diary: Fix govulncheck failure for GO-2026-6505 in otlptrace exporters

The scheduled `Security` workflow (run 37430407135, tracked in issue #17) failed because govulncheck reported GO-2026-6505 in `go.opentelemetry.io/otel/exporters/otlp/otlptrace`, `.../otlptracegrpc`, and `.../otlptracehttp` at v1.44.0. All three are fixed in v1.45.0. Markus asked for the smallest possible change with no extra dependency bumps.

## Step 1: Scope the fix

**Author:** main

### Prompt Context

**Verbatim prompt:** Fix https://github.com/maragudk/audioadastra/actions/runs/37430407135 with the smallest possible change, no extra dep bumps
**Interpretation:** Make govulncheck pass by upgrading only the vulnerable modules, leaving every other dependency version untouched.
**Inferred intent:** Get the Security workflow green and close issue #17 without turning it into a general dependency refresh.

### What I did
I read the failed job log with `gh run view 37430407135 --log-failed`. It reported one vulnerability that the code calls: GO-2026-6505, found in the three `otlptrace` modules at v1.44.0 and fixed in v1.45.0. In `/go.mod`, all three are indirect requirements at v1.44.0. The core `otel`, `otel/sdk`, and `otel/trace` modules are already at v1.45.0. The `otlpmetric` exporters are still at v1.44.0, but govulncheck didn't flag them.

### Why
Upgrading only the three flagged modules is the smallest change that clears the finding.

### What worked
The job log names the vulnerable modules and the fixed versions directly, so I didn't need to run anything locally to scope the fix.

### What didn't work
My first attempt to create this diary file used a shell heredoc. The worktree isolation guard rejected it: "this command is too complex to verify that it stays inside the worktree". Writing the file with the Write tool worked.

### What I learned
The core otel modules were already at v1.45.0 while the exporters lagged at v1.44.0. Something pulls the exporters in indirectly and pins them to the older version.

### What was tricky
Minimal version selection may force other bumps if the v1.45.0 exporters require newer versions of shared dependencies such as `go.opentelemetry.io/proto/otlp` or `google.golang.org/grpc`. That would conflict with "no extra dep bumps", so the builder has to check the `go.mod` diff.

### What warrants review
The `/go.mod` diff should change only the three `otlptrace` lines.

### Future work
The `otlpmetric` exporters are still at v1.44.0. They aren't flagged, so we're leaving them alone on purpose.


## Step 2: Bump the `otlptrace` exporters to v1.45.0

**Author:** vulnfix

### Prompt Context

**Verbatim prompt:** Make the smallest possible change. Bump only those three modules to v1.45.0, for example with `go get <mod>@v1.45.0` for each and then `go mod tidy`. Do not bump any other dependencies. Leave the otlpmetric exporters at v1.44.0 because govulncheck didn't flag them.
**Interpretation:** Upgrade `otlptrace`, `otlptracegrpc` and `otlptracehttp` to v1.45.0, accept only the bumps that minimal version selection forces, and verify with govulncheck, build and tests.
**Inferred intent:** Turn the Security workflow green with the smallest dependency change.

### What I did
I ran these commands in the worktree root:

```
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace@v1.45.0
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.45.0
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@v1.45.0
go mod tidy
```

The three `otlptrace` lines in `/go.mod` moved from v1.44.0 to v1.45.0. Two other lines moved because the new exporter versions require them:

- `go.opentelemetry.io/proto/otlp` v1.10.0 to v1.11.0. Required by `otlptrace`, `otlptracegrpc` and `otlptracehttp` v1.45.0.
- `google.golang.org/genproto/googleapis/api` v0.0.0-20260526163538-3dc84a4a5aaa to v0.0.0-20260803160001-6ac0973c030d. Required by `otlptracegrpc` and `otlptracehttp` v1.45.0. (`go get` first went to 20260720211330-0afa2a65878a, then on to 20260803160001-6ac0973c030d for the last module.)

I confirmed both with `go mod graph`. The `otlpmetric` exporters stay at v1.44.0, and `google.golang.org/grpc` stays at v1.83.1. `/go.sum` changed to match.

### Why
These are the only way to reach v1.45.0 of the three flagged modules. Reverting the two extra bumps would break the build requirements of the exporters.

### What worked
- `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` exits 0. It reports "Your code is affected by 0 vulnerabilities" and still lists 1 vulnerability in an imported package that the code doesn't call.
- `go build ./...` succeeds and `go test ./...` passes in every package that has tests.

### What didn't work
The worktree isolation guard rejected two of my commands: a `for m in ...; do go get ... done` loop and a `go mod download -json` call inside a loop, with "too complex to verify". I ran the commands one by one instead. In zsh, `${PIPESTATUS[0]}` is empty, so I re-ran govulncheck without a pipe to read its exit code.

### What I learned
Bumping the exporters pulls two generated-proto modules forward with them. That is expected, because `go.opentelemetry.io/proto/otlp` and the `genproto` API module are tied to the exporter release.

### What was tricky
Nothing beyond the forced bumps. The "no extra dep bumps" requirement can't be met literally, so I kept the extra bumps to the two that `go mod graph` shows are required.

### What warrants review
Check `git diff /go.mod`: three `otlptrace` lines plus the two forced indirect bumps. Re-run govulncheck to confirm exit code 0.

### Future work
The `otlpmetric` exporters remain at v1.44.0 on purpose. A later general dependency refresh can bring them up.
