// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"errors"
	"strconv"
	"strings"
)

// errNoFont is returned by the text methods when no font has been selected.
var errNoFont = errors.New("pdfkit: no font set (call SetFont first)")

// Text render modes for SetRenderMode (a subset of PDF's Tr values).
const (
	RenderFill        = 0 // fill glyphs
	RenderStroke      = 1 // stroke glyph outlines
	RenderFillStroke  = 2 // fill then stroke
	RenderInvisible   = 3 // neither (useful for OCR text layers)
	RenderFillClip    = 4 // fill and add to clip
	RenderStrokeClip  = 5 // stroke and add to clip
	RenderFSClip      = 6 // fill, stroke and add to clip
	RenderClip        = 7 // add to clip only
)

// SetFont selects font f at the given size in points for subsequent text. The
// font is registered with the document for embedding on the first use.
func (p *Page) SetFont(f *Font, size float64) {
	p.curName = p.doc.registerFont(f)
	p.curFont = f
	p.fontSize = size
	p.usedFonts[f] = true
}

// SetCharSpacing sets additional spacing between glyphs, in points (Tc).
func (p *Page) SetCharSpacing(v float64) { p.charSpace = v }

// SetWordSpacing sets additional spacing at space characters, in points (Tw).
// It has no visible effect on composite (Type0) fonts and is provided for
// completeness.
func (p *Page) SetWordSpacing(v float64) { p.wordSpace = v }

// SetLeading sets the line leading (baseline-to-baseline distance) used by
// TextLines, in points (TL).
func (p *Page) SetLeading(v float64) { p.leading = v }

// SetRenderMode sets the text rendering mode (Tr); see the Render constants.
func (p *Page) SetRenderMode(mode int) { p.renderMode = mode }

// emitTextState writes the current text-state operators inside a text object.
func (p *Page) emitTextState() {
	p.op("/"+p.curName+" "+ftoa(p.fontSize), "Tf")
	if p.charSpace != 0 {
		p.op(ftoa(p.charSpace), "Tc")
	}
	if p.wordSpace != 0 {
		p.op(ftoa(p.wordSpace), "Tw")
	}
	if p.renderMode != 0 {
		p.op(strconv.Itoa(p.renderMode), "Tr")
	}
}

// Text draws s with its baseline origin at (x, y) using the current font. It
// returns errNoFont if no font is set.
func (p *Page) Text(x, y float64, s string) error {
	if p.curFont == nil {
		return errNoFont
	}
	hex := p.encodeGlyphs(s)
	p.op("", "BT")
	p.emitTextState()
	p.op(nums(x, y), "Td")
	p.op(hex, "Tj")
	p.op("", "ET")
	return nil
}

// TextLines draws consecutive lines starting with the first baseline at (x, y),
// advancing by the current leading between lines.
func (p *Page) TextLines(x, y float64, lines []string) error {
	if p.curFont == nil {
		return errNoFont
	}
	p.op("", "BT")
	p.emitTextState()
	p.op(ftoa(p.leading), "TL")
	p.op(nums(x, y), "Td")
	for i, line := range lines {
		if i > 0 {
			p.op("", "T*")
		}
		p.op(p.encodeGlyphs(line), "Tj")
	}
	p.op("", "ET")
	return nil
}

// encodeGlyphs maps s to the current font's glyphs, records usage, and returns
// the run as a hex string of 2-byte Identity-H codes ("<....>").
func (p *Page) encodeGlyphs(s string) string {
	use := p.doc.use[p.curFont]
	var b strings.Builder
	b.WriteByte('<')
	for _, r := range s {
		gid, ok := p.curFont.ot.GlyphIndex(r)
		if !ok {
			gid = 0
		}
		use.mark(gid, []rune{r})
		writeHex16(&b, uint16(gid))
	}
	b.WriteByte('>')
	return b.String()
}

// writeHex16 appends v as four uppercase hex digits.
func writeHex16(b *strings.Builder, v uint16) {
	const hex = "0123456789ABCDEF"
	b.WriteByte(hex[(v>>12)&0xf])
	b.WriteByte(hex[(v>>8)&0xf])
	b.WriteByte(hex[(v>>4)&0xf])
	b.WriteByte(hex[v&0xf])
}

// TextWidth returns the width of s in points at the current font and size. It
// returns 0 when no font is set.
func (p *Page) TextWidth(s string) float64 {
	if p.curFont == nil {
		return 0
	}
	return p.curFont.measure(s) * p.fontSize
}

// measure returns the advance width of s in em units (glyph-space width / 1000).
func (f *Font) measure(s string) float64 {
	total := 0.0
	for _, r := range s {
		gid, ok := f.ot.GlyphIndex(r)
		if !ok {
			gid = 0
		}
		total += float64(f.glyphWidth1000(gid)) / 1000
	}
	return total
}

// WrapText greedily breaks s into lines no wider than maxWidth points at the
// current font and size, splitting on spaces. A single word wider than maxWidth
// occupies its own line. It returns nil if no font is set.
func (p *Page) WrapText(s string, maxWidth float64) []string {
	if p.curFont == nil {
		return nil
	}
	words := strings.Fields(s)
	var lines []string
	var cur string
	for _, w := range words {
		if cur == "" {
			cur = w
			continue
		}
		if p.TextWidth(cur+" "+w) <= maxWidth {
			cur += " " + w
		} else {
			lines = append(lines, cur)
			cur = w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// TextShaped draws s with complex-script shaping (GSUB substitution and GPOS
// positioning) via the go-opentype shaper, placing the run's origin at (x, y).
// The default Text path stays a simple left-to-right cmap mapping; use this for
// Arabic, Indic, CJK and any text needing ligatures, marks or kerning. features
// names OpenType feature tags to enable (e.g. "liga").
func (p *Page) TextShaped(x, y float64, s string, features ...string) error {
	if p.curFont == nil {
		return errNoFont
	}
	f := p.curFont
	use := p.doc.use[f]
	// A face sized to unitsPerEm has scale 1, so ShapePositioned reports offsets
	// and advances directly in font units.
	face := f.ot.NewFace(f.ot.UnitsPerEm())
	run := face.ShapePositioned(s, features...)
	runes := []rune(s)
	aligned := len(run) == len(runes)

	scale := p.fontSize / float64(f.ot.UnitsPerEm())
	p.op("", "BT")
	p.emitTextState()
	pen := 0
	for i, g := range run {
		var rs []rune
		switch {
		case aligned:
			rs = []rune{runes[i]}
		case i == 0:
			rs = runes // best effort: attribute the whole run to the first glyph
		}
		use.mark(g.Glyph, rs)

		tx := x + float64(pen+g.XOffset)*scale
		ty := y + float64(g.YOffset)*scale
		p.op(nums(1, 0, 0, 1, tx, ty), "Tm")
		var b strings.Builder
		b.WriteByte('<')
		writeHex16(&b, uint16(g.Glyph))
		b.WriteByte('>')
		p.op(b.String(), "Tj")
		pen += g.XAdvance
	}
	p.op("", "ET")
	return nil
}
