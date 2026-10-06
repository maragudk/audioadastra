package html

import (
	"fmt"
	"math/rand/v2"
	"strings"

	. "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

// skyPoint is a position on the constellation grid, where a letter is 4 units wide and 6 units tall,
// with y growing downwards.
type skyPoint struct {
	X, Y float64
}

// skyGlyph is a letter drawn as a constellation: stars joined by lines.
type skyGlyph struct {
	Width float64
	// Strokes are polylines through the glyph's stars.
	Strokes [][]skyPoint
}

var skyGlyphs = map[rune]skyGlyph{
	'A': {Width: 4, Strokes: [][]skyPoint{
		{{0, 6}, {1, 3.6}, {2, 0}, {3, 3.6}, {4, 6}},
		{{1, 3.6}, {3, 3.6}},
	}},
	'D': {Width: 4, Strokes: [][]skyPoint{
		{{0, 0}, {2.6, 0.5}, {4, 3}, {2.6, 5.5}, {0, 6}, {0, 0}},
	}},
	'I': {Width: 1, Strokes: [][]skyPoint{
		{{0.4, 0}, {0.6, 3}, {0.5, 6}},
	}},
	'O': {Width: 4, Strokes: [][]skyPoint{
		{{2, 0}, {3.9, 1.8}, {3.6, 4.8}, {2, 6}, {0.4, 4.8}, {0.1, 1.8}, {2, 0}},
	}},
	'R': {Width: 4, Strokes: [][]skyPoint{
		{{0, 6}, {0, 0}, {2.8, 0.3}, {3.8, 1.6}, {2.8, 3}, {0, 3}},
		{{2.8, 3}, {4, 6}},
	}},
	'S': {Width: 4, Strokes: [][]skyPoint{
		{{3.8, 0.7}, {2, 0}, {0.2, 1.3}, {1, 2.8}, {3.1, 3.3}, {3.8, 4.8}, {2, 6}, {0.1, 5.3}},
	}},
	'T': {Width: 4, Strokes: [][]skyPoint{
		{{0, 0}, {2, 0}, {4, 0}},
		{{2, 0}, {2, 3.2}, {2, 6}},
	}},
	'U': {Width: 4, Strokes: [][]skyPoint{
		{{0, 0}, {0.3, 4.2}, {2, 6}, {3.7, 4.2}, {4, 0}},
	}},
}

const (
	skyLetterGap  = 1.7
	skyWordGap    = 3
	skyLineHeight = 9
	skyMargin     = 1.5
	// skyMarginX is one sparkle arm, so the leftmost star tips line up with the text below.
	skyMarginX = 0.6
)

// skyConstellation draws the given lines of text as one constellation, left-aligned.
// Letters are numbered across all lines for the staggered draw-in. Each row also carries a white copy
// of its lines, clipped to nothing, which the playhead reveals as it sweeps. The id keeps the clip
// paths of several constellations on one page apart.
func skyConstellation(id string, lines []string, classes string) Node {
	r := rand.New(rand.NewPCG(19, 77))

	var lineNodes []Node
	var width float64
	letter := 0
	for i, line := range lines {
		top := skyMargin + float64(i)*skyLineHeight
		var x float64
		var lettersLines, lettersStars, lit []Node
		for _, c := range line {
			if c == ' ' {
				x += skyWordGap
				continue
			}
			glyph := skyGlyphs[c]
			letter++
			l := skyLetter(glyph, skyMarginX+x, top, letter, r)
			lettersLines = append(lettersLines, l.lines)
			lettersStars = append(lettersStars, l.stars)
			lit = append(lit, l.lit...)
			x += glyph.Width + skyLetterGap
		}
		width = max(width, x-skyLetterGap)

		clip := fmt.Sprintf("sky-lit-%v-%v", id, i)
		lineNodes = append(lineNodes, g(Class("sky-row"), Data("top", f2(top)),
			Group(lettersLines),
			El("clipPath", ID(clip),
				El("rect", Class("sky-lit-sweep"), Attr("x", "0"), Attr("y", f2(top-1.5)), Attr("width", "0"), Attr("height", "9")),
			),
			g(Class("sky-lit"), Attr("clip-path", "url(#"+clip+")"), Group(lit)),
			Group(lettersStars),
		))
	}

	height := float64(len(lines)-1)*skyLineHeight + 6

	return SVG(
		Class("sky-constellation overflow-visible "+classes),
		Attr("viewBox", fmt.Sprintf("0 0 %v %v", f2(width+2*skyMarginX), f2(height+2*skyMargin))),
		Attr("preserveAspectRatio", "xMinYMid meet"),
		Aria("hidden", "true"),
		Group(lineNodes),
		El("line", Class("sky-playhead hidden"), Attr("stroke-width", "0.12"), Attr("stroke-linecap", "round")),
	)
}

// skyLetterNodes are the layers of one constellation letter, kept apart so the lit lines of a row can
// sit between its lines and its stars.
type skyLetterNodes struct {
	lines, stars Node
	lit          []Node
}

// skyLetter at the given offset: lines, a star on every distinct point, and white
// copies of the lines for the playhead to light. Every third star is a four-point sparkle, the rest
// round stars of varying brightness; every second sparkle flares when it glints.
func skyLetter(glyph skyGlyph, dx, dy float64, n int, r *rand.Rand) skyLetterNodes {
	jitter := func() float64 { return (r.Float64() - 0.5) * 0.35 }

	moved := map[skyPoint]skyPoint{}
	var points []skyPoint
	for _, s := range glyph.Strokes {
		for _, p := range s {
			if _, ok := moved[p]; ok {
				continue
			}
			moved[p] = skyPoint{X: dx + p.X + jitter(), Y: dy + p.Y + jitter()}
			points = append(points, p)
		}
	}

	var lines, lit []Node
	for _, s := range glyph.Strokes {
		var d strings.Builder
		for i, p := range s {
			m := moved[p]
			if i == 0 {
				fmt.Fprintf(&d, "M%v %v", f2(m.X), f2(m.Y))
				continue
			}
			fmt.Fprintf(&d, "L%v %v", f2(m.X), f2(m.Y))
		}
		lines = append(lines, El("path", Class("sky-line stroke-white"), Attr("d", d.String()), Attr("pathLength", "1"),
			Attr("fill", "none"), Attr("stroke-width", "0.07"),
			Attr("stroke-linejoin", "round")))
		lit = append(lit, El("path", Class("stroke-white"), Attr("d", d.String()), Attr("fill", "none"),
			Attr("stroke-width", "0.09"), Attr("stroke-linejoin", "round"), Attr("stroke-linecap", "round")))
	}

	var stars []Node
	for i, p := range points {
		m := moved[p]
		if i%3 == 0 {
			glints := []string{"a", "b", "c", "d"}
			arm := 0.55 + r.Float64()*0.2
			glint := glints[r.IntN(len(glints))]
			if i%6 == 0 {
				stars = append(stars, skyFlare(m, glint))
			}
			stars = append(stars, skySparkle(m, arm, "sky-glint-"+glint))
			continue
		}
		radius := 0.13
		if r.Float64() < 0.35 {
			radius = 0.1
		}
		stars = append(stars, El("circle", Class("sky-star fill-white"), Data("x", f2(m.X)), Data("y", f2(m.Y)),
			Attr("cx", f2(m.X)), Attr("cy", f2(m.Y)), Attr("r", f2(radius+r.Float64()*0.1))))
	}

	class := fmt.Sprintf("sky-letter-%v", n)
	return skyLetterNodes{
		lines: g(Class(class), Group(lines)),
		stars: g(Class(class), Group(stars)),
		lit:   lit,
	}
}

// skyFlare is a long, thin four-point flash behind a sparkle, shown only at the peak of its glint.
func skyFlare(p skyPoint, glint string) Node {
	const arm, w = 2.2, 0.07
	d := fmt.Sprintf("M%v %v L%v %v L%v %v L%v %v L%v %v L%v %v L%v %v L%v %vZ",
		f2(p.X), f2(p.Y-arm), f2(p.X+w), f2(p.Y-w), f2(p.X+arm), f2(p.Y), f2(p.X+w), f2(p.Y+w),
		f2(p.X), f2(p.Y+arm), f2(p.X-w), f2(p.Y+w), f2(p.X-arm), f2(p.Y), f2(p.X-w), f2(p.Y-w),
	)
	return El("path", Class("sky-flare fill-white sky-flare-"+glint), Attr("d", d))
}

// skySparkle is a four-point star centered on p, with arms of the given length.
// The glint class sets when it glints, if at all.
func skySparkle(p skyPoint, arm float64, glint string) Node {
	w := arm * 0.22
	d := fmt.Sprintf("M%v %v Q%v %v %v %v Q%v %v %v %v Q%v %v %v %v Q%v %v %v %vZ",
		f2(p.X), f2(p.Y-arm),
		f2(p.X+w), f2(p.Y-w), f2(p.X+arm), f2(p.Y),
		f2(p.X+w), f2(p.Y+w), f2(p.X), f2(p.Y+arm),
		f2(p.X-w), f2(p.Y+w), f2(p.X-arm), f2(p.Y),
		f2(p.X-w), f2(p.Y-w), f2(p.X), f2(p.Y-arm),
	)
	return El("path", Class("sky-star sky-sparkle fill-white "+glint), Data("x", f2(p.X)), Data("y", f2(p.Y)), Attr("d", d))
}

// skyField is the background: small stars and the odd sparkle scattered over the whole sky,
// some of them twinkling.
func skyField() Node {
	r := rand.New(rand.NewPCG(4, 2))
	twinkles := []string{"", "", "sky-twinkle-a", "sky-twinkle-b", "sky-twinkle-c"}

	var stars []Node
	for range 160 {
		p := skyPoint{X: r.Float64() * 1600, Y: r.Float64() * 1000}
		twinkle := twinkles[r.IntN(len(twinkles))]
		if r.Float64() < 0.06 {
			stars = append(stars, g(Class("sky-field-star "+twinkle), skySparkle(p, 3+r.Float64()*2.5, "")))
			continue
		}
		scale := 0.6
		if r.Float64() < 0.4 {
			scale = 0.4
		}
		radius := (0.6 + r.Float64()*1.4) * (scale + r.Float64()*0.6)
		stars = append(stars, g(Class("sky-field-star "+twinkle), El("circle", Class("fill-white"), Attr("cx", f2(p.X)), Attr("cy", f2(p.Y)),
			Attr("r", f2(radius)))))
	}

	return SVG(
		Class("pointer-events-none absolute inset-0 h-full w-full"),
		Attr("viewBox", "0 0 1600 1000"),
		Attr("preserveAspectRatio", "xMidYMid slice"),
		Aria("hidden", "true"),
		Group(stars),
	)
}

// f2 formats v with at most two decimals, for compact SVG coordinates.
func f2(v float64) string {
	s := strings.TrimRight(fmt.Sprintf("%.2f", v), "0")
	return strings.TrimSuffix(s, ".")
}

// g is an SVG group.
func g(children ...Node) Node {
	return El("g", children...)
}
