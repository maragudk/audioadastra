---
name: Audio Ad Astra
description: Listen to the stars of the open sky.
colors:
  sky-pink: "oklch(59.2% 0.249 0.584)"
  deep-pink-ink: "oklch(52.5% 0.223 3.958)"
  dim-pink: "oklch(82.3% 0.12 346.018)"
  dusk-pink: "oklch(40.8% 0.153 2.432)"
  blush: "oklch(97.1% 0.014 343.198)"
  petal: "oklch(94.8% 0.028 342.258)"
  starlight-white: "#fff"
  live-violet: "oklch(28.3% 0.141 291.089)"
  live-violet-hover: "oklch(38% 0.189 293.745)"
  sheet-ink: "oklch(13% 0.028 261.692)"
  sheet-text: "oklch(21% 0.034 264.665)"
  sheet-muted: "oklch(44.6% 0.03 256.802)"
  sheet-hint: "oklch(55.1% 0.027 264.364)"
  field-stroke: "oklch(87.2% 0.01 258.338)"
  error-wash: "oklch(97.1% 0.013 17.38)"
  error-ink: "oklch(50.5% 0.213 27.518)"
typography:
  display:
    fontFamily: "Bluu Next, ui-serif, Georgia, serif"
    fontSize: "2.25rem"
    fontWeight: 700
    lineHeight: 1.25
  display-wide:
    fontFamily: "Bluu Next, ui-serif, Georgia, serif"
    fontSize: "3rem"
    fontWeight: 700
    lineHeight: 1.25
  page-title:
    fontFamily: "Bluu Next, ui-serif, Georgia, serif"
    fontSize: "3rem"
    fontWeight: 700
    lineHeight: 1.25
  page-title-wide:
    fontFamily: "Bluu Next, ui-serif, Georgia, serif"
    fontSize: "3.75rem"
    fontWeight: 700
    lineHeight: 1.25
  sheet-title:
    fontFamily: "Bluu Next, ui-serif, Georgia, serif"
    fontSize: "2.25rem"
    fontWeight: 700
    lineHeight: 1.25
  handle:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.875rem"
    fontWeight: 700
    lineHeight: 1.25
    letterSpacing: "-0.025em"
  wordmark:
    fontFamily: "Bluu Next, ui-serif, Georgia, serif"
    fontSize: "1.5rem"
    fontWeight: 700
    lineHeight: "2.5rem"
  body-large:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.1875rem"
    fontWeight: 700
    lineHeight: "1.75rem"
  body-large-wide:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.5rem"
    fontWeight: 400
    lineHeight: "2.5rem"
  body-small:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 400
    lineHeight: "1.5rem"
  label-button:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.25rem"
    fontWeight: 700
  label-button-sheet:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.125rem"
    fontWeight: 700
  label-nav:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1rem"
    fontWeight: 600
  label-field:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.75rem"
    fontWeight: 500
rounded:
  focus: "0.25rem"
  chip: "0.5rem"
  alert: "0.75rem"
  sheet: "1.5rem"
  pill: "9999px"
spacing:
  gutter-phone: "16px"
  gutter-tablet: "24px"
  gutter-desktop: "32px"
  cluster: "16px"
  stack: "24px"
  section: "32px"
  section-wide: "40px"
  sheet-inset: "32px"
  sheet-inset-wide: "40px"
components:
  button-primary:
    backgroundColor: "{colors.starlight-white}"
    textColor: "{colors.deep-pink-ink}"
    typography: "{typography.label-button}"
    rounded: "{rounded.pill}"
    padding: "14px 28px"
  button-primary-hover:
    backgroundColor: "{colors.deep-pink-ink}"
    textColor: "{colors.starlight-white}"
  button-secondary:
    backgroundColor: "transparent"
    textColor: "{colors.starlight-white}"
    rounded: "{rounded.pill}"
    padding: "12px 24px"
  button-secondary-hover:
    backgroundColor: "{colors.starlight-white}"
    textColor: "{colors.deep-pink-ink}"
  button-secondary-playing:
    backgroundColor: "{colors.live-violet}"
    textColor: "{colors.starlight-white}"
    rounded: "{rounded.pill}"
  button-secondary-playing-hover:
    backgroundColor: "{colors.live-violet-hover}"
    textColor: "{colors.starlight-white}"
  sheet:
    backgroundColor: "{colors.starlight-white}"
    textColor: "{colors.sheet-text}"
    rounded: "{rounded.sheet}"
    padding: "{spacing.sheet-inset}"
    width: "28rem"
  button-sheet-primary:
    backgroundColor: "{colors.sky-pink}"
    textColor: "{colors.starlight-white}"
    typography: "{typography.label-button-sheet}"
    rounded: "{rounded.pill}"
    padding: "12px 24px"
    width: "100%"
  button-sheet-primary-hover:
    backgroundColor: "{colors.deep-pink-ink}"
    textColor: "{colors.starlight-white}"
  button-sheet-secondary:
    backgroundColor: "transparent"
    textColor: "{colors.deep-pink-ink}"
    rounded: "{rounded.pill}"
    padding: "10px 24px"
  button-sheet-secondary-hover:
    backgroundColor: "{colors.sky-pink}"
    textColor: "{colors.starlight-white}"
  text-field:
    backgroundColor: "{colors.starlight-white}"
    textColor: "{colors.sheet-text}"
    rounded: "{rounded.pill}"
    padding: "12px 16px"
  error-alert:
    backgroundColor: "{colors.error-wash}"
    textColor: "{colors.error-ink}"
    typography: "{typography.body-small}"
    rounded: "{rounded.alert}"
    padding: "16px"
  nav-link:
    textColor: "{colors.starlight-white}"
    typography: "{typography.label-nav}"
    rounded: "{rounded.focus}"
---

# Design System: Audio Ad Astra

## Overview

**Creative North Star: "The Playable Sky"**

The site takes its name literally: sound sent up into the stars. Every page is a drenched, flat pink sky scattered with white stars. On the front page the name is written across it as a star chart, and the visitor can press Play to hear it. A playhead sweeps each row, every star it passes rings a note, each ringing star bursts into a white sparkle, and the lines it has swept light up white behind it. "Stars" means three things at once (music stars, real stars, and the sparkle of glamour and party), so the brightest stars are four-point sparkles that glint.

The world is flat and graphic. There are no glow halos, no gradient light and no shadows; depth comes only from the scatter of a quiet background star field behind the content. The palette is Tailwind's default pink with white as the one star ink: every star, sparkle and line is white at rest. One deep violet is held back entirely for whatever is playing right now, and while the sky plays the lines dim to a light pink so the playhead can light them white again. The display face, Bluu Next Bold, was chosen because its sharp, pointed serifs make it look a bit like a constellation itself; the constellation lettering stays plain stars and lines. Inter carries everything a person reads at length or clicks.

The sky carries every page, not just the front. Statements sit directly on the pink in white; forms and small print sit on a white sheet, a large-radius card that floats flat on the sky, because small text cannot meet contrast on pink. Inside a sheet the same character holds: Bluu Next titles, full-pill controls, pink as the action colour and the sparkle as the icon.

**Key Characteristics:**
- One drenched pink sky with a white star field behind every page.
- Every star and line is white at rest; stars vary by size only.
- Deep violet means "live" and nothing else.
- Display serif for the site's own statements and titles; Inter for everything interactive and for all user content.
- Statements on the sky, small print on a white sheet.
- Full pills for every control and field.
- One large motion (the playhead sweep); everything else is small, staggered and quiet.

## Colors

Tailwind default pinks on a single saturated ground, with one off-family violet reserved for live state and Tailwind greys only inside white sheets. The frontmatter values are the compiled Tailwind v4 OKLCH values; the source names (`pink-600`, `violet-950` and so on) are given in parens. `primary-*` in the theme is an alias for the whole pink scale.

### Primary
- **Sky Pink** (`pink-600`, `primary-600`): the ground of every page, edge to edge. It is the owner's pink and dominates every viewport. Inside a sheet it is the action colour: the submit pill's fill, the outlined pill's border and hover fill, the focus ring and the focused field outline.
- **Deep Pink Ink** (`pink-700`): pink text on white (the primary pill's label, the secondary pill's label on hover, the outlined sheet pill's label) and the text-selection highlight. It is also the hover fill of the white primary pill and of the sheet submit pill, under white text.

### Secondary
- **Dim Pink** (`pink-300`): playback only. While the sky plays, the constellation lines dim to it over 0.4s, so the white lit trail behind the playhead reads; on stop they return to white. It is never a resting ink and never fills a star. (The owner rejected pink-950 on the sky as reading black, and pink-300 stars as reading grey.)
- **Dusk Pink** (`pink-900`): the scrollbar thumb, over a Sky Pink track.

### Tertiary
- **Live Violet** (`violet-950`): reserved for live state. It colours the playhead line, the star currently ringing, and the Play button while it is pressed. It appears nowhere at rest.
- **Live Violet Hover** (`violet-900`): the hover fill of the Play button while it is playing; still live state only.

### Neutral
- **Starlight White** (`#fff`): the only star ink (every round star and sparkle in the wordmark and the field, every flare and burst, the constellation lines at rest and the lit trail of swept lines), all text set directly on the sky, the header links, the sheet's ground, the white primary pill's fill, and the focus ring on the sky.
- **Blush** (`pink-50`): body copy on the sky from `sm` up, where it is set at 24px.
- **Petal** (`pink-100`): the footer icons; they turn white on hover.
- **Sheet Ink** (`gray-950`): headings inside a sheet.
- **Sheet Text** (`gray-900`): text and field values inside a sheet, and the field label.
- **Sheet Muted** (`gray-600`): the subtitle under a sheet heading.
- **Sheet Hint** (`gray-500`): the "@" add-on in the handle field; placeholders are `gray-400`.
- **Field Stroke** (`gray-300`): the 1px resting outline of a text field.
- **Error Wash / Error Ink** (`red-50` / `red-700`): the error alert inside a sheet.

### Named Rules
**The Live Violet Rule.** Violet-950 means "this is playing now". It is used for the playhead, the ringing star and the pressed Play button (violet-900 when that button is hovered), and for nothing at rest.

**The Pink Ground Rule.** Every page is Sky Pink. The owner tried a violet sky and asked for pink to dominate again; a yellow live state was also tried and rejected.

**The White Stars Rule.** Every star, sparkle, flare, burst and line on the sky is white. Stars vary by size only, never by colour. The one exception is playback: lines dim to `pink-300` so the playhead can light them white again.

**The Greys Stay On The Sheet Rule.** Tailwind greys appear only inside a white sheet. Nothing set directly on the sky is grey.

## Typography

**Display Font:** Bluu Next Bold (with ui-serif, Georgia, serif), self-hosted from `public/styles/` with its OFL text.
**Body Font:** Inter variable, 100 to 900, roman and italic (with ui-sans-serif, system-ui, sans-serif), self-hosted with its OFL text.

**Character:** A sharp, high-contrast serif with pointed, star-like terminals for statements, against a neutral, highly legible sans for reading and controls. The serif gives the brand its personality; the sans keeps the interface plain.

### Hierarchy
- **Display** (700, 2.25rem on phones, Display Wide 3rem from `sm`, line-height 1.25, balanced wrapping): the tagline on the front page, in white.
- **Page Title** (700, 3rem on phones, Page Title Wide 3.75rem from `sm`, line-height 1.25, balanced wrapping): a heading set directly on the sky, as on the not-found and error messages. A bare H1 given to a page gets this style automatically.
- **Sheet Title** (700, 2.25rem, line-height 1.25, Sheet Ink): the heading inside a sheet, when it is a title ("Log in").
- **Handle** (Inter 700, 1.875rem, line-height 1.25, letter-spacing -0.025em, Sheet Ink, breaks anywhere if long): a person's handle as the heading of the profile sheet. User content gets Inter, not the display face.
- **Wordmark** (700, 1.5rem on a 2.5rem line): "Audio Ad Astra" in the header of every page except the front page, where the constellation already spells the name.
- **Body Large** (700, 1.1875rem/1.75rem, white, below `sm`; Body Large Wide 400, 1.5rem/2.5rem, Blush, from `sm`; pretty wrapping): every paragraph set directly on the sky, such as the front-page statement (max width 48rem) and the one-line explanation on a message page (max width 42rem). Phones get the bold white setting so the text counts as WCAG large text on Sky Pink.
- **Body Small** (400, 0.875rem/1.5rem): small print inside a sheet, such as the subtitle and the error alert.
- **Label** (Inter 700 at 1.25rem on pills on the sky, 700 at 1.125rem on the sheet submit pill, 600 at 1rem on the outlined sheet pill and header links, 500 at 0.75rem on the field label chip): every control.

### Named Rules
**The Statements Only Rule.** Bluu Next is for the site's own titles: the tagline, the wordmark, sheet titles and message-page headings. It never sets a button, link, label, form field or any other UI control, and never sets user content such as handles, names or track titles, even when that content is the page heading (the owner rejected the handle in the display face).

**The Large Enough Rule.** On Sky Pink, body text must be WCAG large text: 24px or more at regular weight, or bold and at least 18.66px. Below 24px regular, set it bold, at 18.66px or larger. Anything smaller belongs on a sheet.

**The No Script Rule.** No handwriting or script faces, anywhere. Personality comes from the display serif, not from a hand-drawn style.

## Layout

One layout for every page. The pink sky runs edge to edge behind the header, content and footer, and the background star field is an absolutely positioned SVG that covers the content area (sliced, centred), sits behind the content and ignores pointer events. The header row is at least 56px tall: the Bluu Next wordmark on the left and the account link on the right, or the account link alone, right-aligned, on the front page. The footer centres the Bluesky and GitHub icons with 24px vertical padding. The document uses the dark colour scheme throughout; a sheet switches back to the light scheme inside itself.

The container is centred, max width 80rem (`max-w-7xl`), full width below that, and grows to fill the space between header and footer as a column. Side gutters are 16px on phones, 24px from `sm` and 32px from `lg`; page content gets 16px of vertical padding (32px from `md`). The breakpoints are Tailwind's defaults: phones below `sm`, tablets at `md`, sideways tablets at `lg`, laptops and desktops at `xl`.

There are three kinds of page:
- **Front page:** full-bleed, with no wordmark in the header. Content is left-aligned in one column: the constellation wordmark inside the H1 (two lines from `sm`, three on phones; full width on phones, capped at 48dvh tall from `sm` so the tagline, paragraph and both buttons stay in the first viewport), then a block up to 56rem wide holding the tagline, the paragraph and a wrapping row of buttons. Vertical rhythm: 32px between the H1 and the copy block (40px from `sm`), 24px inside the copy block, 16px between buttons.
- **Sheet pages** (login, profile): one sheet, centred horizontally and vertically on the sky, with 24px of vertical breathing room (48px from `sm`).
- **Message pages** (not found, server error): a left-aligned column, centred vertically, with 24px between the Page Title heading, one Body Large line and a white pill back to the front page.

**The Clear Text Rule.** Background stars never sit on text, on any page. On load, on resize and when fonts finish loading, field stars within 16px of any block marked for avoidance (every text block on the sky, every button row and every sheet), or within 12px of a constellation letter, are hidden.

## Elevation & Depth

Everything is flat. There are no shadows, glows, halos or gradients; depth is suggested only by the background field, whose small white round stars vary in size alone (radius roughly 0.25 to 2.4 units, about two in five drawn smaller), with the occasional sparkle. The constellation wordmark, the copy and the sheets all sit in front of it on the same flat plane: a sheet is separated from the sky by its white fill alone, with no shadow or border.

### Named Rules
**The Flat Sky Rule.** Nothing on the sky casts or emits light. A star is a crisp, flat point or a four-point sparkle; it never gets a glow, and a sheet never gets a shadow.

## Shapes

Two shapes carry the world. The **round star** is a flat circle. The **four-point sparkle** is a star whose four arms are drawn as quadratic curves pinched to about 22% of the arm length at the waist. The sparkle is the brand's recurring glyph: it is every third star in the constellation, the occasional field star, the white burst of a ringing star, and the icon on the Play button and the Log in button.

**Constellation lines** are hairlines (0.07 units on a letter grid 6 units tall) with round joins, the way a star chart draws them. Letters are built from straight segments between jittered points, so the wordmark reads as hand-plotted rather than typeset.

**Flares** are long, thin, straight-edged four-point flashes (arms 2.2 units, waist 0.07) behind a sparkle, the glamour glint of a photographed star.

The **logo star** is the one five-point star in the system: hand-cut, tilted and uneven (see Logo). It is not used as an ornament on the sky.

The rest is soft and round. Every button and every text field is a **full pill** (9999px). A **sheet** has generous 1.5rem corners; the error alert inside it 0.75rem; the field label chip 0.5rem. Focusable links use a small 0.25rem radius so their focus ring has softened corners.

## Components

### Buttons
Fully round, generous and confident. On the sky they are white; on a sheet they are pink.
- **Shape:** full pill (9999px), icon and label separated by 12px (10px on the sheet submit pill).
- **Primary on the sky:** white fill with Deep Pink Ink text, Inter 700 at 1.25rem, padding 14px by 28px. Used once per view, for the main action ("Follow on Bluesky" with the Bluesky butterfly on the front page; "Back to the front page" on message pages).
- **Secondary on the sky:** transparent with a 2px white border at 70% opacity and white text, Inter 600 at 1.25rem, padding 12px by 24px ("Play the sky").
- **Primary on a sheet:** full-width Sky Pink fill with white text, Inter 700 at 1.125rem, padding 12px by 24px, with a sparkle icon ("Log in").
- **Secondary on a sheet:** transparent with a 2px Sky Pink border and Deep Pink Ink text, Inter 600 at 1rem, padding 10px by 24px ("Log out").
- **Hover:** every pill changes fill and lifts 2px. The white primary turns Deep Pink Ink with white text and a 2px white ring inset at its edge; the secondary on the sky fills white with Deep Pink Ink text; the sheet primary deepens to Deep Pink Ink; the sheet secondary fills Sky Pink with white text. Sparkle icons on buttons turn 90 degrees over 300ms. Transitions take 200ms with an ease-out. Under reduced motion the pills do not lift, and icons turn without animating.
- **Pressed / playing:** while the sky plays, the Play button fills with Live Violet, its border turns Live Violet, its sparkle icon becomes a rounded stop square, and its label reads "Stop the sky". On hover it deepens to Live Violet Hover (`violet-900`) and keeps white text.
- **Focus:** a 2px outline at 4px offset: white on the sky, Sky Pink on a sheet.

### Sheet (signature)
A flat white card on the sky for forms and small print: up to 28rem wide, centred, 1.5rem corners, 32px padding (40px from `sm`), light colour scheme, no shadow and no border. Field stars clear around it. It holds a centred heading (a Sheet Title, or a Handle when the heading is user content), an optional Body Small subtitle in Sheet Muted 8px below, then its content 32px below that. Use it whenever text would be smaller than the Large Enough Rule allows on pink.

### Inputs / Fields
- **Style:** a full-pill white field with a 1px Field Stroke outline, padding 12px by 16px, Inter at 1rem. The handle field carries a leading inline "@" add-on in Sheet Hint, hidden from assistive technology; placeholders are `gray-400`.
- **Label:** overlaps the field's top border, 16px in from the left: Inter 500 at 0.75rem in Sheet Text, on a white chip with 4px side padding and 0.5rem corners (the Tailwind Plus "overlapping label" pattern, combined with "input with inline add-on").
- **Focus:** the outline becomes 2px Sky Pink, drawn on the whole pill while its input has focus.
- **Error:** an alert above the form, Error Ink text on Error Wash, Body Small, 16px padding, 0.75rem corners, announced as an alert.

### Navigation
- **Header:** the Bluu Next wordmark (white, linking to the front page) on the left, on every page except the front page; Inter 600 at 1rem, white, for the single account link ("Log in" or "Profile") on the right, which underlines on hover.
- **Footer:** centred Bluesky and GitHub icon links at 24px, 16px apart (32px from `sm`), in Petal, turning white on hover.
- **Focus:** every link uses a 2px current-colour outline at 4px offset with a 0.25rem radius.

### Message page
How the site answers a missing page or a server error, on the sky: the heading in Page Title, one plain sentence of explanation in Body Large, and a white primary pill back to the front page. The tone stays in the world: "Nothing up here. Maybe the link has drifted." and "Try again in a moment. If it keeps happening, tell us on Bluesky."

### Logo (signature)
A white, hand-cut five-point star on a Sky Pink (`#e60076`) square: tilted slightly clockwise, with uneven points (the right and lower-right points are the longest), softly rounded tips, small rounded inner corners and edges bowed slightly inwards. It reads as cut from paper by hand, not drawn with a ruler. The source is `assets/logo.svg` (512 viewBox), with a 1024px `assets/logo.png`. It is every favicon and web app icon in `public/`: `favicon.svg`, `favicon.ico` (16, 32, 48), the 96px favicon, the 180px Apple touch icon and the 192 and 512 maskable manifest icons, with the star inside the maskable safe zone. The manifest's theme and background colours are the same pink. The owner chose it after rejecting a constellation "A", a plain geometric star (it looked like WordArt) and several concept sketches.

**The Icon Only Rule.** The star logo lives in icons and app chrome only. The site header carries no logo: the Bluu Next wordmark names the site on inner pages, and the constellation does it on the front page.

### Constellation Wordmark (signature)
The name "AUDIO AD ASTRA" drawn as a constellation in server-rendered SVG, left-aligned, decorative to assistive technology (the H1 carries the name as visually hidden text). Each letter is a set of white hairlines through jittered star points; every third point is a white sparkle, the rest are white round stars of varying size. Every second sparkle carries a flare. The layout is seeded, so the sky is identical on every load. Each constellation takes an id so that several can share a page (the wide two-row and narrow three-row versions do) without clashing clip paths.

- **Draw-in:** on load each letter's lines draw in once over 1.6s with a strong ease-out (`cubic-bezier(0.16, 1, 0.3, 1)`), staggered 150ms per letter.
- **Glint:** sparkles swell to 145% scale briefly on four staggered cycles of 5.3s to 9.1s. At the peak of each glint, the sparkles that carry a flare flash it: it scales from 0 to full size with a 12-degree turn and back, in sync with that sparkle's glint cycle.
- **Play the sky:** Web Audio, silent until the visitor presses Play. A Live Violet playhead sweeps each row left to right; each star it passes rings a note on a pentatonic scale (higher stars ring higher, sparkles ring brighter than round stars), turns Live Violet, scales to 220% and releases a white sparkle burst that flares to 190% and collapses over 0.7s. When playback starts, the visible constellation's lines dim to Dim Pink over 0.4s. Behind the playhead the row's lines light up white again (a slightly heavier white copy, revealed by a widening clip), so the played part of the name reads as lit; the trail clears on stop and when the loop starts again, and on stop the lines return to white. The button only appears when Web Audio is available.

### Star Field (signature)
160 seeded background stars behind the content of every page: mostly white round stars varying only in size, about 6% sparkles. Three in five twinkle (opacity down to 25%) on three staggered cycles of 3.2s, 4.7s and 6.1s. Stars that would touch text, a sheet or a letter are hidden (see the Clear Text Rule).

### Page chrome
Every page uses the dark colour scheme, text selection is white on Deep Pink Ink, and the scrollbar is Dusk Pink on Sky Pink.

### Named Rules
**The One Big Motion Rule.** The playhead sweep is the only large motion. Everything else (draw-in, glint, twinkle, the 2px hover lift) is small, staggered and quiet.

**The Reduced Motion Rule.** Under `prefers-reduced-motion`, constellation lines are static, sparkles do not glint, flares never show, field stars do not twinkle, ringing stars change colour without scaling, no bursts are drawn, and buttons do not lift. Sound, the playhead and the lit trail still work.

## Do's and Don'ts

### Do:
- **Do** put every page on Sky Pink (`pink-600`) edge to edge, with the white star field behind it.
- **Do** draw every star and constellation line on the sky in white, varying stars by size only; dim lines to `pink-300` only while the sky plays.
- **Do** set body text on Sky Pink at 24px regular or larger; below that, set it bold at 18.66px or more. Put anything smaller on a white sheet.
- **Do** put forms and small print on a sheet: white, 1.5rem corners, flat, centred on the sky.
- **Do** keep Live Violet (`violet-950`) for live state: the playhead, the ringing star and the pressed Play button.
- **Do** use the four-point sparkle as the brand's star glyph, including as an icon where a control needs one.
- **Do** set the tagline, the wordmark and the site's own titles in Bluu Next Bold, and every control and all user content (handles included) in Inter.
- **Do** make every button and text field a full pill. On the sky, the primary is white with Deep Pink Ink text and the secondary a white outline; on a sheet, the primary is Sky Pink with white text and the secondary a pink outline. On hover every pill changes fill and lifts 2px, except under reduced motion.
- **Do** give every focusable element a 2px outline at 4px offset.
- **Do** keep background stars off text and sheets.
- **Do** honour reduced motion: static lines, no scale, no bursts, no lift.
- **Do** take every colour from the Tailwind default palette.
- **Do** use the hand-cut star logo, white on Sky Pink, for every favicon and app icon, kept inside the maskable safe zone.

### Don't:
- **Don't** use violet-950 for anything at rest, and don't set the sky itself in violet (tried and rejected by the owner).
- **Don't** use yellow for live state (tried and rejected by the owner).
- **Don't** add glows, halos, gradient light or shadows, on the sky or on a sheet.
- **Don't** use pink-950 on the sky, for lines or a button hover alike; it reads as black (rejected by the owner). Don't colour stars at all either (pink-300 dots read as grey).
- **Don't** put wedge serifs or other partial shapes on star points; they read as clipped stars (tried and removed at the owner's request).
- **Don't** set grey text directly on the sky; greys live inside sheets.
- **Don't** set buttons, links, labels, form fields or user content such as handles in Bluu Next.
- **Don't** use handwriting or script faces.
- **Don't** put a logo in the header, the star included; the star is for favicons and app icons, and the header names the site in type.
- **Don't** redraw the logo as a regular geometric star; its hand-cut irregularity is the point (a plain star was rejected as looking like WordArt).
- **Don't** add a second large motion alongside the playhead sweep.
- **Don't** start sound before the visitor presses Play.
