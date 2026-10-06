# Diary: Fix the flaky login browser test

`TestLogin` in `/cmd/app/login_test.go` drives a real Chrome through the login against the local atproto network. It failed once in CI on an unrelated change, and passed on a rerun with no code change. This task makes it reliable.

## Step 1: Diagnose the CI failure

**Author:** main

### Prompt Context

**Verbatim prompt:** Fix it in a separate PR
**Interpretation:** Fix the flaky click in `TestLogin` that failed CI on PR #21, on its own branch and in its own PR.
**Inferred intent:** CI should fail only for real problems, so a red run can be trusted.

### What I did

I read the failed job log of CI run 37454017402 on PR #21, which only moved the OAuth client metadata path. The failure was in the subtest "should log in through the PDS, write the profile, show the handle, and log out":

```
login_test.go:51: open the profile: Could not find node with given id (-32000)
```

The "log in" step ends with `chromedp.WaitVisible(`a[href="/profile"]`)` after clicking Authorize on the PDS consent page. The next step, "open the profile", starts with `chromedp.Click(`a[href="/profile"]`)`. The failure screenshot was taken at the app's front page. I reran the failed job with `gh run rerun 37454017402 --failed`, and it passed.

The other lines in the log are expected: a PAR answered with `use_dpop_nonce`, which the client retries, and a `RecordNotFound` from `getRecord` before the profile is created.

### Why

PR #21 changed only the confidential client ID, and this test uses the localhost dev client, so the change could not have caused the failure. A pass on rerun confirmed it was a flake.

### What worked

The failure message and screenshot path in the test's step helper made the failing step obvious without reproducing locally.

### What didn't work

The first CI run of PR #21 failed as quoted above.

### What I learned

Chrome's DevTools protocol answers "Could not find node with given id" when an action targets a DOM node from a document that has since been replaced. A node found visible can still be gone a moment later if the page navigates.

### What was tricky

The likely race sits at the end of a chain of redirects from the PDS through `/oauth/callback` to the front page. Waiting for a selector to be visible does not guarantee the navigation has settled, so the node chromedp holds may belong to a document that is about to be replaced.

### What warrants review

Whether the fix addresses the race itself, rather than adding sleeps or retries that hide it.

### Future work

None from this step.

## Step 2: Find the root cause and wait for the document to load

**Author:** builder

### Prompt Context

**Verbatim prompt:** Make the chromedp browser test `TestLogin` in /cmd/app/login_test.go reliable. It failed once in CI and passed on a rerun with no code change. [...] The working hypothesis is a race at the end of the redirect chain from the PDS through `/oauth/callback` (a 303) to the front page: the node found visible belongs to a document that is then replaced. Confirm or correct this before fixing. [...] Fix the cause, not the symptom. No sleeps. No blind retries of the click. [...] Check the other browser tests in /cmd/app for the same pattern [...] Keep the change small.
**Interpretation:** Find out why the profile link node disappears between `WaitVisible` and `Click`, fix that in the test with a wait on a real condition, apply it to every similar spot, and show with repeated runs that it holds.
**Inferred intent:** A red browser test in CI should mean a real regression, never a timing accident.

### What I did

First I ran the test as it is, `go test -tags sqlite_fts5,sqlite_math_functions -run TestLogin -count 30 ./cmd/app/`, against the stack from `make test-up`. All 30 runs passed, so the flake doesn't show up locally on its own.

Then I wrote a throwaway test next to `TestLogin` that listens to the browser's page, DOM and network events with `chromedp.ListenTarget` and logs them with timestamps through the whole login. The end of the chain after Authorize looked like this (milliseconds since start):

```
1744ms doc request GET https://pds.localhost/oauth/authorize/redirect?...
1752ms doc request GET http://127.0.0.1:60129/oauth/callback?...   (redirected from status 303)
1797ms doc request GET http://127.0.0.1:60129/                     (redirected from status 303)
1804ms frameNavigated http://127.0.0.1:60129/
1810ms documentUpdated
1839ms == profile link visible
1843ms documentUpdated
1843ms domContentEventFired
1843ms loadEventFired
```

The front page is requested once, and nothing re-renders the nav: the only Datastar attribute on the page is a `data-init` that logs to the console. The node is not replaced by a new document. Instead, Chrome sends a second `DOM.documentUpdated` for the same document at DOMContentLoaded. The profile link was visible 4 ms before that.

That explains the error. Chrome's DOM agent forgets all node IDs and pushes the document again when DOMContentLoaded fires. chromedp handles `DOM.documentUpdated` by throwing away its node map and fetching a new root (`documentUpdated` in chromedp's `target.go`). A chromedp query that hits a stale ID while it looks for and checks the node just retries. But `chromedp.Click` runs the click itself (`ScrollIntoViewIfNeeded`, then the content quads) as the query's "after" function, and an error there is final. So if DOMContentLoaded lands between the visibility check and the click, the click fails with `Could not find node with given id (-32000)`. `chromedp.Navigate` waits for the load event, which is why the steps after a `Navigate` are safe. A `Click` that navigates doesn't wait. The redirect chain and the cross-origin hop don't matter, except that they make it a click-started navigation.

The page has three deferred scripts in the head (Datastar, `app.js`, and the Fathom script from `cdn.usefathom.com`). They hold DOMContentLoaded back until they have loaded and run, while the parsed nav is already visible. In CI the Fathom script comes over the internet, so the gap and its jitter are different from a local run.

To reproduce it on purpose, the throwaway test intercepted the Fathom script with the Fetch domain, held it for a random 15 to 29 ms, and then failed it, so nothing reached the internet. With the test's original steps, that failed with the exact CI error in 2 of 60 runs, and again in 3 of 100 runs. A third batch of 100 runs logged the delay per run, and failed at 17, 21 and 24 ms, so the window comes from jitter and not from one particular delay. With the fix, the same test passed 100 of 100 and then 200 of 200 runs.

The fix is a helper in `/cmd/app/login_test.go`:

```go
func waitLoaded(sel string) chromedp.Action {
	return chromedp.Tasks{
		chromedp.WaitVisible(sel),
		chromedp.Poll(`document.readyState === "complete"`, nil),
	}
}
```

It replaces `chromedp.WaitVisible` in every spot where a click navigates to a new document and a node action follows: the password field on the PDS sign-in page (in both subtests), the profile link back on the front page, `#logout` on the profile page before `chromedp.Text("h1")`, and the alert on the login page before `chromedp.Text` after denying consent. `chromedp.Text` uses the node in its "after" function too, so it has the same race. The final `WaitVisible` after logging out stays as it is, because no node action follows it. The consent page's buttons stay too, since "Sign in" only changes the URL fragment in the PDS app and loads no new document. The test still clicks the profile link.

After removing the throwaway test, `TestLogin` passed 30 of 30 runs with `-count 30`. `go test -tags sqlite_fts5,sqlite_math_functions -shuffle on ./cmd/app/...` passed, and `golangci-lint run ./cmd/...` reported 0 issues.

A self-review with the code-review skill found no issues. It checked that `chromedp.Poll` accepts a nil result and times out after 30 seconds, and that each spot touched by a click-started navigation now uses `waitLoaded`. It also checked that none of the selectors exists on the page being left, so no wait can match an element on the old page and then poll the wrong document.

### Why

The wait has to mean "the document is done changing node IDs", and not "some time has passed". `document.readyState` becomes `"complete"` only after DOMContentLoaded. So once the poll returns true, Chrome has already sent its last `documentUpdated` for that document, and over the same ordered connection. chromedp might not have handled that event yet when the next action starts. But until it does, the next query runs against the old root ID, which fails and retries, and doesn't reach the click.

### What worked

The event log answered the question in one run. It ruled out the guesses in the brief (a double load, a Datastar re-render, a stale cache across origins), and pointed at DOMContentLoaded.

Holding a deferred script through the Fetch domain gave a repro that used no sleeps in the code under test and reached no real network. A random delay across the window worked better than any fixed one.

### What didn't work

Holding the Fathom script for a fixed 300 ms, and then for a random 0 to 39 ms, gave 0 failures in 10 and 40 runs. With a long delay, the click finishes long before DOMContentLoaded, so it never straddles the event. The failure needs DOMContentLoaded to land within a few milliseconds after the visibility check.

A first try at the throwaway test failed before it ran, because the sandbox refused the shell command that wrote it with a heredoc and patched it with `sed`: "this command is too complex to verify that it stays inside the worktree". Writing the file with the editor tool worked.

### What I learned

- Chrome sends `DOM.documentUpdated` twice for one navigation: once at commit and once at DOMContentLoaded. The second one throws away every node ID, even though the document is the same.
- `document.readyState` is `"interactive"` before deferred scripts run and before DOMContentLoaded, so waiting for `"interactive"` is not enough. `"complete"` is.
- `chromedp.Poll` doesn't survive a navigation, so it must start after something has proved the new document is current. Hence `WaitVisible` comes first in `waitLoaded`.
- In chromedp, an error from a query's "after" function (`Click`, `Text`, `SendKeys`) is final, while errors while finding and checking the node are retried.

### What was tricky

The window is a few milliseconds wide and its position comes from jitter, so the flake was invisible in 30 plain local runs. It also looked like a cross-origin problem, because it showed up at the end of the redirect chain, but any click-started navigation to a page with deferred scripts has it.

### What warrants review

- `waitLoaded` in `/cmd/app/login_test.go`, and whether every spot where a click starts a navigation and a node action follows now uses it.
- Whether polling for `"complete"` is the right condition. It waits for the load event, which also waits for images and the Fathom script, so the wait can be longer in CI. It is still bounded by the 60-second browser timeout.
- To check it yourself: start the stack with `make test-up` and run `go test -tags sqlite_fts5,sqlite_math_functions -run TestLogin -count 30 ./cmd/app/`. The throwaway repro test is not in the repository. Its method is described above.

### Future work

The pages load the Fathom script from the internet in tests too. If it ever becomes a problem in CI, the test app could leave it out. That's not needed for this fix.
