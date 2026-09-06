// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"strings"
	"testing"

	"github.com/go-opentype/opentype"
)

func TestFtoaFourDecimalsNoTrailingZerosNoNegativeZero(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want string
	}{
		{72, "72"},
		{100.0, "100"},
		{0.5, "0.5"},
		{100.10000, "100.1"},
		{6.588336614173228, "6.5883"},
		{841.8897637795275, "841.8898"},
		{-0.00001, "0"},
		{-0.00005, "-0.0001"},
		{-1.5, "-1.5"},
		{0, "0"},
	} {
		if got := ftoa(tc.in); got != tc.want {
			t.Errorf("ftoa(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRedundantFontAndColourOperatorsAreNotRewritten(t *testing.T) {
	_, p, f := loadSynth(t)
	p.SetFont(f, 12)
	p.SetFillColor(RGB{1, 0, 0})
	p.SetFillColor(RGB{1, 0, 0})
	p.SetStrokeColor(Gray{0.5})
	p.SetStrokeColor(Gray{0.5})
	if err := p.Text(0, 0, "Hi"); err != nil {
		t.Fatal(err)
	}
	if err := p.Text(0, 20, "Hi"); err != nil {
		t.Fatal(err)
	}
	c := content(p)
	if n := strings.Count(c, " rg\n"); n != 1 {
		t.Errorf("same fill colour set twice wrote %d rg, want 1\n%s", n, c)
	}
	if n := strings.Count(c, " G\n"); n != 1 {
		t.Errorf("same stroke colour set twice wrote %d G, want 1\n%s", n, c)
	}
	if n := strings.Count(c, " Tf\n"); n != 1 {
		t.Errorf("two text objects in one font wrote %d Tf, want 1\n%s", n, c)
	}

	// A different size is a different text state, and Restore forgets all of
	// them so the same values must be written again in the restored state.
	p.SetFont(f, 14)
	p.Text(0, 40, "Hi")
	p.Save()
	p.Restore()
	p.SetFillColor(RGB{1, 0, 0})
	p.SetStrokeColor(Gray{0.5})
	p.Text(0, 60, "Hi")
	c = content(p)
	if n := strings.Count(c, " Tf\n"); n != 3 {
		t.Errorf("size change + restore should give 3 Tf, got %d\n%s", n, c)
	}
	if n := strings.Count(c, " rg\n"); n != 2 {
		t.Errorf("fill colour after Restore should be rewritten: %d rg, want 2\n%s", n, c)
	}
	if n := strings.Count(c, " G\n"); n != 2 {
		t.Errorf("stroke colour after Restore should be rewritten: %d G, want 2\n%s", n, c)
	}
}

func TestEmitShapedRunPlainTextIsOneTJWithNoNumbers(t *testing.T) {
	// Advances exactly as the font's own: the viewer's /W pen already lands
	// every glyph, so the array is glyphs only.
	_, p, f := loadSynth(t)
	p.SetFont(f, 10)
	adv := func(g opentype.GlyphIndex) int { return int(f.face.AdvanceIndexUnits(g)) }
	run := []opentype.PositionedGlyph{
		{Glyph: 1, XAdvance: adv(1)},
		{Glyph: 2, XAdvance: adv(2)},
		{Glyph: 1, XAdvance: adv(1)},
	}
	p.emitShapedRun(50, 60, run, []rune("HiH"))
	c := content(p)
	want := "1 0 0 1 50 60 Tm\n[<0001><0002><0001>] TJ\nET\n"
	if !strings.HasSuffix(c, want) {
		t.Errorf("plain run:\n%s\nwant suffix:\n%s", c, want)
	}
}

func TestEmitShapedRunWritesKerningAsOneCorrection(t *testing.T) {
	// The second glyph's advance is 100 font units wider than /W says: the
	// third glyph needs a single negative correction (moving right) of
	// 100 units in thousandths of an em, and nothing else does.
	_, p, f := loadSynth(t)
	p.SetFont(f, 10)
	adv := func(g opentype.GlyphIndex) int { return int(f.face.AdvanceIndexUnits(g)) }
	toMil := 1000 / float64(f.ot.UnitsPerEm())
	run := []opentype.PositionedGlyph{
		{Glyph: 1, XAdvance: adv(1)},
		{Glyph: 2, XAdvance: adv(2) + 100},
		{Glyph: 1, XAdvance: adv(1)},
	}
	p.emitShapedRun(0, 0, run, []rune("HiH"))
	c := content(p)
	want := "[<0001><0002> " + ftoa(-100*toMil) + " <0001>] TJ\n"
	if !strings.Contains(c, want) {
		t.Errorf("kerned run should carry one correction:\n%s\nwant %q", c, want)
	}
}

func TestEmitShapedRunOffsetsAndMarks(t *testing.T) {
	// A horizontal offset moves one glyph without moving the pen after it;
	// a vertical offset (a positioned mark) breaks the segment: its own Tm +
	// Tj at the raised baseline, then a fresh Tm for what follows. A run that
	// is not aligned with its runes (a ligature) attributes the text to the
	// first glyph only.
	_, p, f := loadSynth(t)
	p.SetFont(f, 10)
	adv := func(g opentype.GlyphIndex) int { return int(f.face.AdvanceIndexUnits(g)) }
	upem := float64(f.ot.UnitsPerEm())
	toMil := 1000 / upem
	scale := 10 / upem
	run := []opentype.PositionedGlyph{
		{Glyph: 1, XAdvance: adv(1)},
		{Glyph: 2, XOffset: -200, XAdvance: adv(2)}, // nudged left, pen unaffected
		{Glyph: 1, YOffset: 300, XOffset: 50, XAdvance: adv(1)},
		{Glyph: 2, XAdvance: adv(2)},
	}
	p.emitShapedRun(10, 20, run, []rune("HiHi!")) // 5 runes for 4 glyphs: unaligned
	c := content(p)

	// Segment 1: glyph 1, then glyph 2 pulled left by 200 units.
	seg1 := "1 0 0 1 10 20 Tm\n[<0001> " + ftoa(200*toMil) + " <0002>] TJ\n"
	if !strings.Contains(c, seg1) {
		t.Errorf("first segment:\n%s\nwant %q", c, seg1)
	}
	// The mark: its own Tm at the raised baseline, shifted by its x offset.
	penAfter2 := float64(adv(1) + adv(2))
	mark := nums(1, 0, 0, 1, 10+(penAfter2+50)*scale, 20+300*scale) + " Tm\n<0001> Tj\n"
	if !strings.Contains(c, mark) {
		t.Errorf("mark glyph:\n%s\nwant %q", c, mark)
	}
	// Segment 2 resumes at the pen after the mark's advance, glyph only.
	seg2 := nums(1, 0, 0, 1, 10+(penAfter2+float64(adv(1)))*scale, 20) + " Tm\n[<0002>] TJ\nET\n"
	if !strings.HasSuffix(c, seg2) {
		t.Errorf("second segment:\n%s\nwant suffix %q", c, seg2)
	}
	if n := strings.Count(c, " Tm\n"); n != 3 {
		t.Errorf("expected 3 Tm (segment, mark, segment), got %d\n%s", n, c)
	}
}

func TestEmitShapedRunMarkLastNeedsNoTrailingSegment(t *testing.T) {
	// A run ending on a positioned mark flushes nothing after it — no empty
	// TJ, no dangling Tm.
	_, p, f := loadSynth(t)
	p.SetFont(f, 10)
	adv := func(g opentype.GlyphIndex) int { return int(f.face.AdvanceIndexUnits(g)) }
	run := []opentype.PositionedGlyph{
		{Glyph: 1, XAdvance: adv(1)},
		{Glyph: 2, YOffset: 100, XAdvance: adv(2)},
	}
	p.emitShapedRun(0, 0, run, []rune("Hi"))
	c := content(p)
	if !strings.HasSuffix(c, "<0002> Tj\nET\n") {
		t.Errorf("run ending on a mark:\n%s", c)
	}
	if strings.Contains(c, "[] TJ") {
		t.Errorf("empty TJ array written:\n%s", c)
	}
}
