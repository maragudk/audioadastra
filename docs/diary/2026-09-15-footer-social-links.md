# Diary: Footer links to GitHub and Bluesky

Add GitHub and Bluesky links with SVG icons to the site footer, modelled on the footer of maragu.dev, the author's public website. The footer currently renders nothing visible: an empty centered flex row carrying only the Datastar `data-init` smoke-test attribute.

## Step 1: Refine requirements and brief the builder

**Author:** main

### Prompt Context

**Verbatim prompt:** Add footer links with SVG icons to github and bluesky, like on ../maragu.dev

**Interpretation:** Put two icon links in the footer of every page, using the same GitHub and Bluesky SVG icons and the same accessibility treatment (`title`, `sr-only` label, `aria-hidden` on the SVG) as the reference site.

**Inferred intent:** Give visitors an obvious way to find the project's source and its Bluesky presence, in the same visual language as the author's other sites.

### What I did

Delegated exploration of both repos to a sub-agent. Found that maragu.dev builds its footer as one `Rawf` HTML blob, while this project uses pure gomponents with dot imports and no `Raw` anywhere. Asked Markus where the links should point: GitHub goes to the project repo `https://github.com/maragudk/audioadastra`, Bluesky to `https://bsky.app/profile/audioadastra.com`. Wrote requirements and spawned a builder.

### Why

The two URLs are product decisions, not something a builder should guess; the reference site links to the author's personal accounts, which is the wrong choice for a project site.

### What worked

One exploration sub-agent covered both repos and answered every question needed for the brief.

### What didn't work

Nothing failed at this stage.

### What I learned

The existing `TestPage` in `/html/common_test.go` asserts on the Datastar `data-init` attribute in the footer, so the builder must keep that attribute somewhere in the page.

### What was tricky

Deciding not to copy the raw-HTML approach. The reference implementation works, but it would be the only `Raw` in this codebase; small gomponents icon functions fit better.

### What warrants review

The builder's step below.

### Future work

None identified.

## Step 2: Build the footer icon links

**Author:** footer-builder

### Prompt Context

**Verbatim prompt:** Add two icon links to the site footer: GitHub and Bluesky. (Followed by the lead's written requirements: the two URLs, the exact SVG path data for both icons, the accessibility treatment of `sr-only` span plus `aria-hidden` SVG, a note to build the icons as gomponents functions rather than a `Raw` blob, and instructions to keep the Datastar `data-init` attribute, add test assertions for both hrefs, and verify the result in a browser.)

**Interpretation:** Put a Bluesky link and a GitHub link inside the existing empty centered flex row in `footer()`, each an anchor holding a visually hidden name and an inline SVG icon, styled to read correctly in both light and dark mode.

**Inferred intent:** Make the footer actually do something for visitors, without introducing the first `Raw` HTML or the first icon dependency into a codebase that has deliberately avoided both.

### What I did

Red first: added two failing subtests to `TestPage` in `/html/common_test.go` asserting on both hrefs, confirmed they failed for the right reason (`no Bluesky link`, `no GitHub link`), then wrote the footer.

In `/html/common.go` the flex row gained `text-gray-500 dark:text-gray-400` and two anchors, each carrying `hover:text-gray-900 dark:hover:text-white`, a `title`, a `<span class="sr-only">` with the link name, and an icon. The Bluesky anchor also carries `rel="me"`. The icons live in two new package-private functions, `blueskyIcon()` and `githubIcon()`, built from `SVG`, `El("path", ...)` and `Attr(...)`.

Verified in the browser: started the app with `make watch`, loaded it with playwright-cli, and screenshotted the footer in light mode and (with `emulateMedia({colorScheme: 'dark'})` plus a hover on the Bluesky link) in dark mode. Both icons render, and the accessibility tree shows `link "Bluesky"` and `link "GitHub"` with the SVGs contributing nothing — exactly the intent. Also grepped the built `/public/styles/app.css` to confirm Tailwind emitted every new utility.

`make test`, `make lint` (0 issues) and `make fmt` are all green.

### Why

The icons are built from `SVG`/`El`/`Attr` rather than `Rawf` because this codebase has no `Raw` anywhere and the lead asked to keep it that way. The gomponents `html` package at v1.3.0 has no `Path` or `ViewBox` helper, so `El("path", ...)` with `Attr` is the available route; `Aria("hidden", "true")` does exist and renders `aria-hidden="true"`.

### What didn't work

No `.env` existed, only `.env.example`, so `make watch` had nothing to read and the app was not running. Copying `.env.example` to `.env` (it is gitignored) was enough to get it up on `http://localhost:8080`.

The browser console showed two pre-existing errors, unrelated to this change: the Datastar smoke test needs `unsafe-eval`, which `.env.example` sets to `false`, so Datastar logs `Error: GenerateExpression ... Evaluating a string as JavaScript violates the following Content Security Policy directive`. The `data-init` attribute still renders and is still asserted on; only its evaluation in the browser is blocked under the example config.

My first cut of the anchors went through a `socialLink(href, name string, icon Node, extra ...Node)` helper that appended the extra nodes *after* the child nodes and relied on gomponents hoisting attribute nodes into the opening tag. It rendered correctly, but self-review killed it — see below.

### What I learned

gomponents renders attribute nodes into the opening tag regardless of where they sit among an element's children, because `El` makes two passes over the child list. That makes `A(..., children..., Rel("me"))` produce valid HTML, which is convenient and also a trap: the same slot silently accepts a non-attribute node and turns it into trailing content.

A `[Rel]` doclink does not resolve here. Go doc links only resolve package-level identifiers of the current package or `pkg.Name` for a named import; `Rel` arrives via a dot import and is invisible to link resolution. `[html.Rel]` would be worse, because in this file `html` already means `maragu.dev/glue/html`.

### What was tricky

Deciding how much abstraction two links deserve. The helper looked tidy but needed a doc comment whose only job was to explain a gomponents rendering rule — a sign the shape was wrong rather than the comment inadequate.

### What warrants review

Whether `title` alongside the `sr-only` span is wanted. Both are specified in the requirements and both match the existing header logo, but they give the anchor an accessible name and an identical accessible description, so some screen readers announce "Bluesky, link, Bluesky".

### Self-review

Two competing reviewers went over the diff. Four findings had consensus, and all four are fixed:

The `socialLink` helper was cut entirely and replaced with two explicit anchors written inline, matching how `header()` already writes its one anchor. This removed the awkward variadic slot, the reliance on attribute hoisting, and the doc comment that existed only to explain it.

The broken `[Rel]` doclink went away with that comment.

The test assertions were too weak: `strings.Contains(link, "<svg")` passes for any SVG at all and would not have caught an icon missing `aria-hidden`, which is the whole reason the `sr-only` span works as the accessible name. The two near-identical subtests are now one table-driven loop over `{name, href, rel}` that additionally asserts the `title`, the `aria-hidden="true"` on the SVG opening tag specifically, and the presence of a `<path`. Mutation-checked by deleting `Aria("hidden", "true")` and confirming both cases fail with `Bluesky icon is not hidden: <svg viewBox="0 0 600 530" fill="currentColor" class="h-6 w-6">`.

Both reviewers flagged that Step 1 of this diary named maragu.dev, suspecting a private repository. The lead reverted that redaction: maragu.dev is the author's public website, and the diary skill requires the verbatim prompt to stay verbatim.

Findings I did not act on: switching `h-6 w-6` to Tailwind v4's `size-6`, and dropping `title` in favour of the `sr-only` span alone. Both were single-reviewer nitpicks against explicit requirements, so they belong to the lead, not to me.

### Future work

The 24x24px touch targets meet WCAG 2.5.8 AA exactly rather than comfortably; `-m-2 p-2` on each anchor would grow them to about 40px without moving anything visually. Worth doing if more links join the row.

The pre-existing CSP/Datastar console error under `.env.example` is unrelated to this change but will greet anyone who boots the app from the example config.
