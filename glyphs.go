// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/go-opentype/opentype"
)

// NewFont wraps a font another component has already parsed, so that a
// program which laid its text out with an *opentype.Font — a layout engine
// measuring with the very faces it will print — embeds that same font
// without keeping its bytes or parsing it a second time.
//
// The font must carry a 'cmap', as for LoadFont. A Font made this way has no
// whole-font fallback: embedding always goes through go-opentype's subsetter,
// which can only fail on a glyph id the font does not have, and ShowGlyphs
// refuses those up front.
func NewFont(ot *opentype.Font) (*Font, error) {
	if ot == nil {
		return nil, errors.New("pdfkit: NewFont: nil font")
	}
	if _, ok := ot.Table("cmap"); !ok {
		return nil, fmt.Errorf("pdfkit: NewFont: no cmap table, the font maps no characters")
	}
	name := ""
	if nameTable, ok := ot.Table("name"); ok {
		name = readPSName(nameTable)
	}
	if name == "" {
		name = "PDFKitFont"
	}
	return &Font{ot: ot, face: ot.NewFace(ot.UnitsPerEm()), baseName: name, isCFF: fontIsCFF(ot)}, nil
}

// Glyph is one glyph of a run whose layout the caller has already done: the
// glyph to draw, where its origin goes and the text it stands for.
//
// X and Y are the glyph origin's offset from the run origin, in points, Y
// upwards as everywhere in the page's user space. Text is what copying the
// glyph out of the PDF should give (the /ToUnicode mapping, ISO 32000-1
// §9.10.3): one character for most glyphs, several for a ligature, empty for
// a glyph that continues a cluster an earlier glyph already spoke for.
type Glyph struct {
	ID   opentype.GlyphIndex
	X, Y float64
	Text string
}

// ShowGlyphs draws glyphs, already chosen and positioned by the caller, with
// the run origin at (x, y) in the current font and size.
//
// It is the entry point for a layout engine that shapes and measures text
// itself: the PDF then shows the glyphs at exactly the positions the layout
// computed instead of re-deriving them from the font, which is what keeps a
// justified line, a letter-spaced heading or a tab stop where the layout put
// it. Each baseline segment is one TJ array (ISO 32000-1 §9.4.3): the viewer
// advances by each glyph's /W width, and a number is written only where the
// caller's position differs from that by at least a thousandth of an em, so
// plain unkerned text carries no numbers at all and the pen never drifts. A
// change of Y starts a new segment.
//
// It reports errNoFont without a font, and an error — before writing
// anything — for a glyph id the font does not have or a position that is not
// a finite number.
func (p *Page) ShowGlyphs(x, y float64, glyphs []Glyph) error {
	f := p.curFont
	if f == nil {
		return errNoFont
	}
	if len(glyphs) == 0 {
		return nil
	}
	n := opentype.GlyphIndex(f.ot.NumGlyphs())
	for i, g := range glyphs {
		if g.ID >= n {
			return fmt.Errorf("pdfkit: ShowGlyphs: glyph %d is id %d, the font has %d glyphs", i, g.ID, n)
		}
		if !finite(g.X) || !finite(g.Y) {
			return fmt.Errorf("pdfkit: ShowGlyphs: glyph %d is at a non-finite position", i)
		}
	}
	if !finite(x) || !finite(y) {
		return errors.New("pdfkit: ShowGlyphs: non-finite run origin")
	}
	if p.fontSize == 0 {
		return errors.New("pdfkit: ShowGlyphs: font size is zero")
	}
	use := p.doc.use[f]
	toMil := 1000 / p.fontSize // points -> thousandths of an em

	p.op("", "BT")
	p.emitTextState()
	var arr strings.Builder
	flush := func() {
		if arr.Len() > 0 {
			p.op("["+arr.String()+"]", "TJ")
			arr.Reset()
		}
	}
	open := false
	segX, segY := 0.0, 0.0 // the current segment's origin, relative to the run
	viewerPen := 0.0       // thousandths of an em past segX, where the viewer's pen is
	for _, g := range glyphs {
		if g.Text != "" {
			use.mark(g.ID, []rune(g.Text))
		} else {
			use.mark(g.ID, nil)
		}
		if !open || g.Y != segY {
			flush()
			p.op(nums(1, 0, 0, 1, x+g.X, y+g.Y), "Tm")
			segX, segY, viewerPen, open = g.X, g.Y, 0, true
		}
		want := (g.X - segX) * toMil
		if adj := viewerPen - want; math.Abs(adj) >= tjTolerance {
			arr.WriteString(" " + ftoa(adj) + " ")
			viewerPen = want
		}
		arr.WriteString(hexGlyph(g.ID))
		viewerPen += float64(f.glyphWidth1000(g.ID)) + p.charSpace*toMil
	}
	flush()
	p.op("", "ET")
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
