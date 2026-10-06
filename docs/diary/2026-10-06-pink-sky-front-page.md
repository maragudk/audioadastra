# Diary: Pink sky front page and visual identity

The front page was a bare `<h1>`, and the rest of the site was a stock white panel under a pink header bar. The goal was a front page with a point of view, a visual identity for the whole site, and the product and design context recorded in `/PRODUCT.md` and `/DESIGN.md`, so later work starts from them instead of from scratch.

## Step 1: Product context and a direction

**Author:** main

### Prompt Context

**Verbatim prompt:** Let's do the front page first, I'm not ready for a player. By the way, I like the current pink color, but I'm not deadset on Inter font, and I actively dislike the logo.
**Interpretation:** Capture product context first, then design and build the front page, keeping the pink and treating the font and logo as open.
**Inferred intent:** A front page that says what Audio Ad Astra is before any audio features exist, and a look the rest of the app can grow into.

### What I did

I interviewed Markus for `/PRODUCT.md`: users (musicians sharing their work and listeners discovering it), positioning (your audio in your own repo, a portable social graph, open lexicons, no algorithm or ads), and constraints (server-rendered, light JS, a persistent player later, playful voice, bold Tailwind colours). For the front page the answers were: follow on Bluesky as the action, a pure statement with no demo content, and the logo left alone for now.

Three rounds of directions followed. The first (a pirate radio dial, a riso gig poster, a record-sleeve style) and the second (a 70s space-disco sleeve, the Voyager Golden Record) were rejected as "too retro / referential". The third round was steered towards the name, and Markus picked "Sound Constellation": the name drawn as a constellation that plays as music. Mid-build he added: "I like the sparkly stuff. I think the 'stars' means both music stars, actual stars, and the sparkle of the industry (and glamour and party)."

### Why

The page had nothing to show yet, so it had to carry the product's idea itself. Taking the name literally ("to the stars") through sound made the page a small demonstration rather than a claim.

### What worked

Asking what was missing after two rejected rounds. "Too retro" pointed the third round at contemporary sources, and the pick came from that round.

### What didn't work

The first two rounds, as above. Building the first round's decision page also failed, because the design tool's decision page refused to start without a local browser: `serve-question: no browser detected in this environment (CI/headless/remote); use the structured question tool instead.` The directions went through plain multiple-choice questions instead.

### What I learned

Markus judges type and colour by seeing them, not by reading descriptions. Rendered comparisons got decisions where prose didn't.

### What was tricky

Keeping hard product facts (no fake tracks, no invented numbers) apart from an ambitious page. The answer was a page that demonstrates the name instead of the product.

### What warrants review

`/PRODUCT.md` is the product record future design work reads first. Check that it says what Audio Ad Astra is.

### Future work

None from this step.

## Step 2: The constellation and Play the sky

**Author:** main

### Prompt Context

**Verbatim prompt:** (continuation of Step 1)
**Interpretation:** Build the chosen direction.
**Inferred intent:** A memorable first viewport that works without JavaScript and plays with it.

### What I did

`/html/sky.go` draws the wordmark as server-rendered SVG. Each letter is a small set of strokes on a 4 by 6 grid, its points get a deterministic jitter (seeded `math/rand/v2` PCG, so every render is identical), and every point gets a star: every third a four-point sparkle, the rest round stars. A second SVG scatters a background field of stars. There are two constellations, a two-line one for wide screens and a three-line one for phones, swapped with CSS.

`/public/scripts/sky.js` is the only page script. "Play the sky" builds a small Web Audio graph (sine and triangle partials into a soft feedback echo), sweeps a playhead across each row, and rings a note per star: x is time, height is pitch on an A minor pentatonic scale. It also hides background stars that would land on text, using `[data-sky-avoid]` blocks. Without JavaScript the button stays hidden and the page is static.

### Why

Server-rendered SVG keeps the page fast, accessible and readable without JavaScript. The heading is real text for screen readers, and the SVGs are `aria-hidden`.

### What worked

Seeding the randomness, which made the layout stable across renders and reviews.

### What didn't work

- The first compile failed on a missing SVG group helper and a wrongly called attribute: `html/sky.go:85:33: undefined: G` and `html/home.go:42:47: not enough arguments in call to Hidden`. gomponents has no `G`, so I added a local `g` helper. `Hidden` takes a value, so it became `Hidden("hidden")`.
- Ringing stars did not change colour at first, because Tailwind's `fill-*` utilities outranked the sky rules in `@layer components`. Moving the sky CSS out of any layer fixed it.
- A Playwright full-page screenshot showed the constellation without lines, which looked like a bug. Viewport screenshots and the computed `stroke-dashoffset` (`0px`) showed the lines drawn, so it was a capture artifact.

### What I learned

`var()` in SVG presentation attributes is unreliable, so all colours go through classes. The CSP allows no inline `style` attributes, but setting `element.style` from script works.

### What was tricky

The heading must not double-announce. The `<h1>` holds an `sr-only` name and both SVGs are hidden from assistive technology.

### What warrants review

`/html/sky.go` (glyph table, layout, field) and `/public/scripts/sky.js` (audio graph, sweep loop, `clearField`). Press "Play the sky" and check that sound only starts on click.

### Future work

None from this step.

## Step 3: Colour, type and tagline by iteration

**Author:** main

### Prompt Context

**Verbatim prompt:** It's weird that the chrome color is pink on the login page but not on the front. Pink isn't the dominant color anymore, makes me a bit sad.
**Interpretation:** The violet night sky lost the brand pink; bring pink back to the lead.
**Inferred intent:** One recognisable colour identity across the site.

### What I did

The sky went from violet-950 to drenched pink-600. Violet survives only as the live state (playhead, the star being played, the pressed button). Yellow was tried for the live state first and rejected. Lines went from pink-950 (read as black) to pink-300 (dots read as grey) to white, dimming to pink-300 only while playing so the white trail behind the playhead shows.

Type took many rounds of rendered comparisons: Funnel ("too plain"), Ojuju ("regretted"), no handwriting styles, and then Bluu Next for display with Inter for the body and all UI. Markus picked Bluu Next because it "looks a bit like a constellation itself". It ships as a lossless, unsubsetted WOFF2 conversion of the author's OTF, with its OFL text next to it. Markus wrote the tagline: "Listen to the stars of the open sky." The ear logo was removed from the header.

### Why

Each change answered a direct reaction to the page in Markus's browser.

### What worked

Rendering font candidates into the real page with the real copy, instead of specimen sheets.

### What didn't work

- A Playwright screenshot of a local `file://` specimen silently captured the app instead, and I sent that image before noticing. Loading the HTML with `page.setContent` fixed it.
- Serving the specimen over a local web server was denied by the session's sandbox, so all comparisons went out as screenshots.

### What I learned

Bluu Next's license reserves the font name "BluuNext", and a subset counts as a modified version that may not keep the name. A webfont subset from a CDN would have been a license problem. The OFL FAQ treats lossless WOFF2 wrapping of the full font as fine.

### What was tricky

Contrast on pink-600. White on pink-600 is about 4.37:1, below AA for normal text. Body text on the sky is therefore 24px regular, or bold and at least 18.66px below `sm`. Anything smaller goes on a white sheet.

### What warrants review

`/tailwind.css` (fonts, theme, sky CSS) and `/public/styles/BluuNext-OFL.txt`. Check the phone paragraph against the contrast rule.

### Future work

None from this step.

## Step 4: Reviews and upgrades

**Author:** main

### Prompt Context

**Verbatim prompt:** Add the upgrades
**Interpretation:** Build the three optional upgrades the design review suggested.
**Inferred intent:** More glamour, and playback that reads as a performance.

### What I did

Independent design reviews ran several times. Their fixes covered constellation alignment with the text column, the Follow button landing in the first phone screen, reduced motion, browser surfaces (scrollbar, focus rings, selection), and a muddy colour fade on released stars.

The upgrades:
- long-arm flares on the brightest sparkles;
- a white trail behind the playhead, done as a per-row `<clipPath>` rect widened from script;
- pointed serif wedges on the letters.

Markus said some stars looked "cut off", so the wedges came out again. Button hovers got stronger: an inverted fill, a 2px lift, and a sparkle icon that turns on hover.

### Why

The reviews checked the build against the agreed direction with fresh eyes. The upgrades answered the "glamour and party" meaning of stars.

### What worked

The clip-path trail. It needs no extra animation state; script only sets one width per row each frame.

### What didn't work

- A Python `str.replace` inserted the flare and serif CSS twice, because the anchor `.sky-star.is-ringing {` occurred twice (base rule and reduced-motion rule). The duplicate sat harmlessly inside `@media (prefers-reduced-motion: reduce)` until the design documenter noticed it. It is removed now.
- Fading released stars with a `fill` transition passed through a grey-lavender. The fill now changes instantly and only the scale animates.

### What I learned

At this scale, any partial shape touching a star reads as a broken star.

### What was tricky

Avoiding colours that interpolate through mud on pink: opacity fades of white or yellow, and fill transitions from violet.

### What warrants review

Press "Play the sky": lines dim, the trail lights them white, and stars burst into white sparkles. Then check with reduced motion turned on.

### Future work

None from this step.

## Step 5: Every page on the sky

**Author:** main

### Prompt Context

**Verbatim prompt:** Can we carry some of the character across to other pages?
**Interpretation:** Apply the sky world to login, profile and the error pages, not just the buttons. A follow-up said "Not just for the button, but every page."
**Inferred intent:** One coherent site.

### What I did

`html.Page` is now always the sky layout: pink ground, background stars, white header with the Bluu Next wordmark (left out on the front page), white footer icons, and `sky.js` on every page. The white panel layout and its dark-mode variants are gone.

Login and profile sit on a white `sheet` with full-pill fields and buttons, because small text cannot meet contrast directly on pink. The handle field has an overlapping label and an "@" add-on, after a Tailwind Plus pattern. The profile handle is set in Inter, because Markus didn't want user content in the display face.

glue renders the error and not-found pages itself, so `cmd/app` now passes `html.GluePage`. It turns those two pages, recognised by the titles glue gives them, into short messages on the sky with a way back to the front page.

### Why

Markus could see the front page and the login page side by side, and the mismatch was the complaint.

### What worked

The sheet as the single answer to "small print on pink".

### What didn't work

- The new layout put the page container in a flex column without a width, so `mx-auto` shrank it to its content and the front-page constellation became much smaller. Adding `w-full` (with `grow`) to `container` fixed it, and also centred the login sheet.
- Overriding glue's not-found handler from the app's routes, with `r.NotFound(...)` inside the injected router group, had no effect. The handler lands on the nearest group mux, not on the root mux where glue sets its own. Passing a page function to glue instead (`HTMLPage: html.GluePage`) worked.
- In dev, Markus can't log in from his own laptop: the dev OAuth callback is `http://127.0.0.1:8080/oauth/callback`, which only resolves on the machine running the app. The profile and error pages were reviewed from screenshots rendered directly from the page code.

### What I learned

glue's pages are only reachable through the `PageFunc` the app hands it, so styling them means wrapping that function.

### What was tricky

`html.GluePage` recognises glue's pages by their titles ("Not found", "Something went wrong"). If glue changes those strings, the pages quietly fall back to a styled bare heading on the sky, which degrades gracefully but would go unnoticed.

### What warrants review

`/html/common.go` (layout, `sheet`, `container`), `/html/login.go`, `/html/profile.go`, `/html/glue.go` and `/cmd/app/main.go`. Visit `/nope` for the not-found page.

### Future work

- The footer's Datastar `data-init` smoke test logs a CSP error in dev (no `'unsafe-eval'`). It predates this work.
- The favicons and web app icons still use the old ear logo.
