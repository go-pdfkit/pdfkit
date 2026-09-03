// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"math"
	"strconv"
)

// Page is a single page's content. It accumulates a content stream as drawing
// methods are called; the operators mirror PDF's imaging model. The default
// user space has its origin at the lower-left corner with y increasing upward.
type Page struct {
	doc    *Document
	width  float64
	height float64
	buf    bytes.Buffer

	usedFonts  map[*Font]bool
	usedImages map[*imageXObject]bool

	curFont   *Font
	curName   string
	fontSize  float64
	lineWidth float64

	// text state, emitted inside each text object.
	charSpace  float64
	wordSpace  float64
	leading    float64
	renderMode int

	// extGStates records the transparency graphics states this page uses, in
	// registration order; each becomes a /GS<i> resource.
	extGStates []extGState

	// links are the clickable link annotations on this page, in the order added;
	// each becomes a /Link annotation in the page's /Annots array.
	links []linkAnnot

	// dests are the named destinations anchored on this page; each becomes an entry
	// in the document's /Dests name tree, jumped to by an internal GoTo link.
	dests []namedDest
}

// linkAnnot is a clickable rectangle. When dest is empty it opens uri (an external
// URI action); otherwise it jumps to the named destination dest (a GoTo action).
type linkAnnot struct {
	rect Rect
	uri  string
	dest string
}

// namedDest is a named jump target anchored at (x, y) in the page's user space.
type namedDest struct {
	name string
	x, y float64
}

// AddLink adds a borderless clickable link over rect — in the same PDF user-space
// coordinates the drawing methods use — that opens uri when activated. It is how a
// typeset hyperref link (\href/\url) becomes navigable in the PDF, matching the
// <a href> the SVG output already emits.
func (p *Page) AddLink(rect Rect, uri string) {
	p.links = append(p.links, linkAnnot{rect: rect, uri: uri})
}

// AddNamedDest anchors the named destination name at (x, y) on this page, so an
// internal link can jump to it. The point (x, y) is the top-left the viewer scrolls
// to, in the page's user space.
func (p *Page) AddNamedDest(name string, x, y float64) {
	p.dests = append(p.dests, namedDest{name: name, x: x, y: y})
}

// AddNamedLink adds a borderless clickable link over rect that jumps to the named
// destination dest in the same document — the in-PDF counterpart of the SVG output's
// <a href="#name"> for \hyperlink.
func (p *Page) AddNamedLink(rect Rect, dest string) {
	p.links = append(p.links, linkAnnot{rect: rect, dest: dest})
}

// Width returns the page width in points.
func (p *Page) Width() float64 { return p.width }

// Height returns the page height in points.
func (p *Page) Height() float64 { return p.height }

// op writes pre-formatted operands (already space-joined) followed by the
// operator and a newline.
func (p *Page) op(operands, operator string) {
	if operands != "" {
		p.buf.WriteString(operands)
		p.buf.WriteByte(' ')
	}
	p.buf.WriteString(operator)
	p.buf.WriteByte('\n')
}

// num formats a coordinate for the content stream.
func num(v float64) string { return ftoa(v) }

// nums joins several coordinates with spaces.
func nums(vs ...float64) string {
	b := make([]byte, 0, 16)
	for i, v := range vs {
		if i > 0 {
			b = append(b, ' ')
		}
		b = append(b, ftoa(v)...)
	}
	return string(b)
}

// Save pushes the current graphics state (q).
func (p *Page) Save() { p.op("", "q") }

// Restore pops the graphics state (Q).
func (p *Page) Restore() { p.op("", "Q") }

// Transform concatenates the affine matrix [a b c d e f] onto the current
// transformation matrix (cm). Points map as x' = a*x + c*y + e and
// y' = b*x + d*y + f.
func (p *Page) Transform(a, b, c, d, e, f float64) {
	p.op(nums(a, b, c, d, e, f), "cm")
}

// Translate shifts the coordinate system by (tx, ty).
func (p *Page) Translate(tx, ty float64) { p.Transform(1, 0, 0, 1, tx, ty) }

// Scale scales the coordinate system by (sx, sy).
func (p *Page) Scale(sx, sy float64) { p.Transform(sx, 0, 0, sy, 0, 0) }

// Rotate rotates the coordinate system counter-clockwise by deg degrees about
// the origin.
func (p *Page) Rotate(deg float64) {
	r := deg * math.Pi / 180
	c, s := math.Cos(r), math.Sin(r)
	p.Transform(c, s, -s, c, 0, 0)
}

// Skew shears the coordinate system by the given x and y angles in degrees.
func (p *Page) Skew(axDeg, ayDeg float64) {
	p.Transform(1, math.Tan(ayDeg*math.Pi/180), math.Tan(axDeg*math.Pi/180), 1, 0, 0)
}

// MoveTo begins a new subpath at (x, y) (m).
func (p *Page) MoveTo(x, y float64) { p.op(nums(x, y), "m") }

// LineTo adds a straight segment to (x, y) (l).
func (p *Page) LineTo(x, y float64) { p.op(nums(x, y), "l") }

// CurveTo adds a cubic Bézier segment to (x3, y3) with control points
// (x1, y1) and (x2, y2) (c).
func (p *Page) CurveTo(x1, y1, x2, y2, x3, y3 float64) {
	p.op(nums(x1, y1, x2, y2, x3, y3), "c")
}

// Rectangle adds a rectangle subpath (re).
func (p *Page) Rectangle(r Rect) {
	p.op(nums(r.X, r.Y, r.Width, r.Height), "re")
}

// ClosePath closes the current subpath with a straight segment to its start
// (h).
func (p *Page) ClosePath() { p.op("", "h") }

// Stroke strokes the current path (S).
func (p *Page) Stroke() { p.op("", "S") }

// Fill fills the current path with the nonzero winding rule (f).
func (p *Page) Fill() { p.op("", "f") }

// FillEvenOdd fills the current path with the even-odd rule (f*).
func (p *Page) FillEvenOdd() { p.op("", "f*") }

// FillStroke fills (nonzero) then strokes the current path (B).
func (p *Page) FillStroke() { p.op("", "B") }

// FillStrokeEvenOdd fills (even-odd) then strokes the current path (B*).
func (p *Page) FillStrokeEvenOdd() { p.op("", "B*") }

// EndPath ends the path with no fill or stroke (n), used after a clip.
func (p *Page) EndPath() { p.op("", "n") }

// Clip intersects the clipping path with the current path using the nonzero
// rule (W). It must be followed by a path-painting or EndPath operator.
func (p *Page) Clip() { p.op("", "W") }

// ClipEvenOdd intersects the clipping path using the even-odd rule (W*).
func (p *Page) ClipEvenOdd() { p.op("", "W*") }

// SetLineWidth sets the stroke line width in user-space units (w).
func (p *Page) SetLineWidth(w float64) {
	p.lineWidth = w
	p.op(num(w), "w")
}

// Line-cap styles for SetLineCap.
const (
	CapButt   = 0
	CapRound  = 1
	CapSquare = 2
)

// SetLineCap sets the line-cap style (J).
func (p *Page) SetLineCap(style int) { p.op(strconv.Itoa(style), "J") }

// Line-join styles for SetLineJoin.
const (
	JoinMiter = 0
	JoinRound = 1
	JoinBevel = 2
)

// SetLineJoin sets the line-join style (j).
func (p *Page) SetLineJoin(style int) { p.op(strconv.Itoa(style), "j") }

// SetMiterLimit sets the miter limit (M).
func (p *Page) SetMiterLimit(limit float64) { p.op(num(limit), "M") }

// SetDash sets the line dash pattern and phase (d). An empty pattern restores a
// solid line.
func (p *Page) SetDash(pattern []float64, phase float64) {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, v := range pattern {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(ftoa(v))
	}
	b.WriteByte(']')
	b.WriteByte(' ')
	b.WriteString(ftoa(phase))
	p.op(b.String(), "d")
}

// SetFillColor selects the fill colour.
func (p *Page) SetFillColor(c Color) { p.op("", c.ops(false)) }

// SetStrokeColor selects the stroke colour.
func (p *Page) SetStrokeColor(c Color) { p.op("", c.ops(true)) }

// extGState is one transparency graphics-state parameter dictionary this page
// references: a constant fill alpha (ca) and stroke alpha (CA).
type extGState struct {
	fill   float64
	stroke float64
}

// SetAlpha sets the constant fill and stroke alpha (opacity) in [0,1] via an
// ExtGState resource (ca/CA).
func (p *Page) SetAlpha(fill, stroke float64) {
	gs := extGState{fill: fill, stroke: stroke}
	idx := -1
	for i, g := range p.extGStates {
		if g == gs {
			idx = i
			break
		}
	}
	if idx < 0 {
		idx = len(p.extGStates)
		p.extGStates = append(p.extGStates, gs)
	}
	p.op("/GS"+strconv.Itoa(idx), "gs")
}

// finishContent returns the accumulated content-stream bytes.
func (p *Page) finishContent() []byte { return p.buf.Bytes() }

// resources builds the page's /Resources dictionary from the fonts and images
// it used and any transparency states it declared.
func (p *Page) resources(fontRefs map[*Font]objRef, imageRefs map[*imageXObject]objRef) *pdfDict {
	res := newDict()
	res.set("ProcSet", pdfArray{pdfName("PDF"), pdfName("Text"), pdfName("ImageB"), pdfName("ImageC"), pdfName("ImageI")})

	if len(p.usedFonts) > 0 {
		fonts := newDict()
		for f := range p.usedFonts {
			fonts.set("F"+strconv.Itoa(p.doc.fontIx[f]), fontRefs[f])
		}
		res.set("Font", sortDict(fonts))
	}
	if len(p.usedImages) > 0 {
		xobj := newDict()
		for _, x := range p.doc.images {
			if p.usedImages[x] {
				xobj.set(x.name, imageRefs[x])
			}
		}
		res.set("XObject", xobj)
	}
	if len(p.extGStates) > 0 {
		egs := newDict()
		for i, gs := range p.extGStates {
			d := newDict()
			d.set("Type", pdfName("ExtGState"))
			d.set("ca", pdfReal(gs.fill))
			d.set("CA", pdfReal(gs.stroke))
			egs.set("GS"+strconv.Itoa(i), d)
		}
		res.set("ExtGState", egs)
	}
	return res
}

// sortDict returns a copy of d with keys in ascending order, so a resource
// dictionary built from map iteration serialises deterministically.
func sortDict(d *pdfDict) *pdfDict {
	type kv struct {
		k string
		v pdfValue
	}
	pairs := make([]kv, len(d.keys))
	for i := range d.keys {
		pairs[i] = kv{d.keys[i], d.vals[i]}
	}
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0 && pairs[j-1].k > pairs[j].k; j-- {
			pairs[j-1], pairs[j] = pairs[j], pairs[j-1]
		}
	}
	out := newDict()
	for _, p := range pairs {
		out.set(p.k, p.v)
	}
	return out
}
