---
version: 1
slug: "html-home-go"
primary_target: "html/home.go"
related_targets: []
---

# Front page

Scope: the front page (`/`). Visitor mode: Persuade.

Audience: musicians and listeners arriving before any audio feature exists. Action: follow @audioadastra.com on Bluesky. Log in stays in the header, secondary. Content: pure statement, minimal; no demo tracks, artists, or numbers. Constraints: no logo in the header (removed at the owner's request); tagline "Listen to the stars of the open sky." (chosen by the owner); pink is kept; Inter is replaceable; fun, slightly quirky, Tailwind default colors.

## Direction contract

THESIS: The name, taken literally: sound sent up into the stars. The sky is a score, and the visitor can play it. Refuses the category's dark hero with a waveform mock and a headline over a button row.

OWN-WORLD: A drenched pink-600 sky, the owner's pink and the same pink as the chrome on the other pages; flat, no glow halos and no gradient light. Stars are crisp white flat points and four-point sparkles, varying only in size, joined by white hairline constellation lines like a star chart; the lines dim to pink-300 while the sky plays (the owner rejected dark pink-950 lines as reading black, pink-300 dots as reading grey, and serif wedges on star points as reading cut off); the brightest sparkles throw a long thin flare at the peak of their glint; "stars" means music stars, real stars, and the sparkle of glamour and party, so the brightest stars are sparkles that glint. Violet-950 is reserved for live state: the playhead, the star currently ringing and the pressed Play button; each ringing star also bursts into a white sparkle. The owner rejected yellow, and asked for pink to dominate again after a violet sky. Bluu Next Bold (display: tagline, wordmark, headings; never on UI controls; chosen by the owner, who likes that its sharp, pointed serifs read like a constellation themselves; also Typewolf's suggested Inter pairing) and Inter (body), self-hosted with their OFL texts.

STORY: The visitor sees the name written as a constellation, presses play, and hears the sky: a playhead sweeps left to right, every star rings a note and bursts into a sparkle (higher stars higher pitch), and the lines it has swept light up white behind it, so the constellation is performed. They read a few plain sentences about what Audio Ad Astra will be, then follow on Bluesky.

FIRST VIEWPORT: Header with only Log in, right-aligned, on the pink field. The constellation wordmark inside the H1, two lines on desktop and three on phone. On phones it fills the width. On desktop its height is capped (48dvh) so the owner-added tagline, the paragraph and both buttons stay in the first viewport, which leaves open sky to its right. Under it, left-aligned: the tagline in Bluu Next at display size, a short paragraph in Inter at large body size, then "Follow on Bluesky" (white pill with pink-700 text, 5.6:1) and "Play the sky" (outlined, toggles sound).

FORM: Sound Constellation, position 1 on the third ordered list (the pick), seed key f1532881. Signature interaction: Play the sky (Web Audio pentatonic, sound off until pressed). Motion grammar: constellation lines draw in once on load, stars twinkle quietly, the playhead sweep is the only large motion, and prefers-reduced-motion keeps the lines static.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
