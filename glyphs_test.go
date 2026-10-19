// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/go-opentype/opentype"
	"rsc.io/pdf"
)

// advPt is glyph g's advance in points at size pt, as the viewer computes it
// from /W.
func advPt(f *Font, g opentype.GlyphIndex, pt float64) float64 {
	return float64(f.glyphWidth1000(g)) * pt / 1000
}

func TestShowGlyphsAtTheFontsOwnAdvancesWritesNoNumbers(t *testing.T) {
	_, p, f := loadSynth(t)
	p.SetFont(f, 10)
	x1 := advPt(f, 1, 10)
	x2 := x1 + advPt(f, 2, 10)
	err := p.ShowGlyphs(50, 60, []Glyph{{ID: 1, Text: "H"}, {ID: 2, X: x1, Text: "i"}, {ID: 1, X: x2, Text: "H"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "1 0 0 1 50 60 Tm\n[<0001><0002><0001>] TJ\nET\n"
	if c := content(p); !strings.HasSuffix(c, want) {
		t.Errorf("plain run:\n%s\nwant suffix:\n%s", c, want)
	}
}

// TestShowGlyphsWritesTheCallersPositions: a glyph the layout placed 2 pt
// further than the font's advance — letter-spacing, justification — gets one
// correction of -200 thousandths at 10 pt, and the glyph after it, placed at
// its own advance from there, gets none.
func TestShowGlyphsWritesTheCallersPositions(t *testing.T) {
	_, p, f := loadSynth(t)
	p.SetFont(f, 10)
	x1 := advPt(f, 1, 10) + 2
	x2 := x1 + advPt(f, 2, 10)
	if err := p.ShowGlyphs(0, 0, []Glyph{{ID: 1}, {ID: 2, X: x1}, {ID: 1, X: x2}}); err != nil {
		t.Fatal(err)
	}
	want := "[<0001> -200 <0002><0001>] TJ\n"
	if c := content(p); !strings.Contains(c, want) {
		t.Errorf("got:\n%s\nwant %q", c, want)
	}
}

// TestShowGlyphsNeverDrifts: many sub-tolerance differences must not add up.
// Every glyph is placed 0.3 thousandths of an em past the viewer's pen; the
// corrections are computed from absolute positions, so the pen is never a
// thousandth away and a correction appears only as the residue grows.
func TestShowGlyphsNeverDrifts(t *testing.T) {
	_, p, f := loadSynth(t)
	p.SetFont(f, 10)
	var gs []Glyph
	x := 0.0
	for range 40 {
		gs = append(gs, Glyph{ID: 1, X: x})
		x += advPt(f, 1, 10) + 0.003 // 0.3/1000 em at 10 pt
	}
	if err := p.ShowGlyphs(0, 0, gs); err != nil {
		t.Fatal(err)
	}
	// The residue reaches the tolerance every fourth glyph (4 × 0.3 = 1.2):
	// nine corrections of -1.2 put the last glyph 10.8 thousandths along, its
	// true place being 39 × 0.3 = 11.7, under one thousandth away.
	c := content(p)
	if n := strings.Count(c, " -1.2 "); n != 9 || strings.Count(c, "<0001>") != 40 {
		t.Errorf("want 9 corrections of -1.2 among 40 glyphs, got %d:\n%s", n, c)
	}
}

func TestShowGlyphsStartsASegmentWhereTheBaselineMoves(t *testing.T) {
	_, p, f := loadSynth(t)
	p.SetFont(f, 10)
	x1 := advPt(f, 1, 10)
	if err := p.ShowGlyphs(10, 20, []Glyph{{ID: 1}, {ID: 2, X: x1, Y: 3}}); err != nil {
		t.Fatal(err)
	}
	c := content(p)
	if !strings.Contains(c, "1 0 0 1 10 20 Tm\n[<0001>] TJ\n") || !strings.Contains(c, "1 0 0 1 "+ftoa(10+x1)+" 23 Tm\n[<0002>] TJ\n") {
		t.Errorf("a raised glyph needs its own Tm:\n%s", c)
	}
}

func TestShowGlyphsRecordsTheTextForToUnicode(t *testing.T) {
	doc, p, f := loadSynth(t)
	p.SetFont(f, 12)
	if err := p.ShowGlyphs(0, 0, []Glyph{{ID: 1, Text: "H"}, {ID: 2, X: 9, Text: "fi"}}); err != nil {
		t.Fatal(err)
	}
	u := doc.use[f]
	if string(u.toUni[1]) != "H" || string(u.toUni[2]) != "fi" {
		t.Errorf("toUni = %q, %q; want H and fi", string(u.toUni[1]), string(u.toUni[2]))
	}
}

func TestShowGlyphsRefusesBeforeWriting(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	if err := p.ShowGlyphs(0, 0, []Glyph{{ID: 1}}); err != errNoFont {
		t.Errorf("no font: err = %v", err)
	}
	_, p, f := loadSynth(t)
	p.SetFont(f, 10)
	before := content(p)
	for name, gs := range map[string][]Glyph{
		"glyph id past the font": {{ID: opentype.GlyphIndex(f.NumGlyphs())}},
		"NaN position":           {{ID: 1, X: math.NaN()}},
		"infinite position":      {{ID: 1, Y: math.Inf(1)}},
	} {
		if err := p.ShowGlyphs(0, 0, gs); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if err := p.ShowGlyphs(math.NaN(), 0, []Glyph{{ID: 1}}); err == nil {
		t.Error("NaN origin: no error")
	}
	if got := content(p); got != before {
		t.Errorf("a refused run wrote to the page:\n%s", got)
	}
	if err := p.ShowGlyphs(0, 0, nil); err != nil {
		t.Errorf("an empty run: %v", err)
	}
}

// TestNewFontEmbedsAParsedFont: a font handed over already parsed is
// subset-embedded like a loaded one, and an independent reader finds its
// program and its /ToUnicode.
func TestNewFontEmbedsAParsedFont(t *testing.T) {
	if _, err := NewFont(nil); err == nil {
		t.Error("NewFont(nil): no error")
	}
	ot, err := opentype.Parse(synthTTF(defaultSynth()))
	if err != nil {
		t.Fatal(err)
	}
	f, err := NewFont(ot)
	if err != nil {
		t.Fatal(err)
	}
	doc := New(Options{})
	p := doc.AddPage(A4)
	p.SetFont(f, 12)
	if err := p.ShowGlyphs(72, 700, []Glyph{{ID: 1, Text: "H"}, {ID: 2, X: advPt(f, 1, 12), Text: "i"}}); err != nil {
		t.Fatal(err)
	}
	r := reopen(t, doc)
	font := r.Page(1).V.Key("Resources").Key("Font").Key("F0")
	if font.Key("Subtype").Name() != "Type0" {
		t.Fatalf("font subtype = %q, want Type0", font.Key("Subtype").Name())
	}
	desc := font.Key("DescendantFonts").Index(0).Key("FontDescriptor")
	if desc.Key("FontFile2").Kind() == pdf.Null {
		t.Error("no /FontFile2: the parsed font was not embedded")
	}
	cmap := string(readStream(t, font.Key("ToUnicode")))
	if !strings.Contains(cmap, "<0048>") || !strings.Contains(cmap, "<0069>") {
		t.Errorf("ToUnicode does not map H and i:\n%s", cmap)
	}
}

func TestNewFontRefusesWhatLoadFontRefuses(t *testing.T) {
	ot, err := opentype.Parse(synthTTF(synthOpts{noCmap: true}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewFont(ot); err == nil {
		t.Error("a font with no cmap: no error")
	}
	ot, err = opentype.Parse(synthTTF(synthOpts{}))
	if err != nil {
		t.Fatal(err)
	}
	f, err := NewFont(ot)
	if err != nil {
		t.Fatal(err)
	}
	if f.BaseName() != "PDFKitFont" {
		t.Errorf("a font with no PostScript name: baseName = %q, want PDFKitFont", f.BaseName())
	}
}

func TestShowGlyphsRefusesAZeroSize(t *testing.T) {
	_, p, f := loadSynth(t)
	p.SetFont(f, 0)
	if err := p.ShowGlyphs(0, 0, []Glyph{{ID: 1}}); err == nil {
		t.Error("size 0: no error")
	}
}

func TestARadialShadingIsType3(t *testing.T) {
	r := shadeDoc(t, Shading{X0: 50, Y0: 50, R0: 0, X1: 50, Y1: 50, R1: 40, Radial: true,
		Stops: []Stop{{0, RGB8(255, 255, 255)}, {1, RGB8(0, 0, 255)}}, ExtendEnd: true})
	sh := r.Page(1).V.Key("Resources").Key("Shading").Key("Sh0")
	if got := sh.Key("ShadingType").Int64(); got != 3 {
		t.Errorf("ShadingType = %d, want 3 (radial)", got)
	}
	co := sh.Key("Coords")
	if co.Len() != 6 || co.Index(2).Float64() != 0 || co.Index(5).Float64() != 40 {
		t.Errorf("Coords = %v, want [50 50 0 50 50 40]", co)
	}
}

func TestARadialShadingRefusesRadiiPDFCannotPaint(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	stops := []Stop{{0, RGB8(0, 0, 0)}, {1, RGB8(255, 255, 255)}}
	for _, s := range []Shading{
		{Radial: true, R0: 0, R1: 0, Stops: stops},
		{Radial: true, R0: -1, R1: 4, Stops: stops},
		{Radial: true, R0: 0, R1: math.Inf(1), Stops: stops},
	} {
		if err := p.Shade(s); err == nil {
			t.Errorf("radii %v, %v: no error", s.R0, s.R1)
		}
	}
}

// TestDrawImageWritesStraightColourUnderASoftMask: a half-transparent red
// pixel is (128,0,0,128) premultiplied in an *image.RGBA; with no /Matte the
// PDF must carry it straight, (255,0,0), or a viewer paints it dark red.
func TestDrawImageWritesStraightColourUnderASoftMask(t *testing.T) {
	for name, img := range map[string]image.Image{
		"RGBA":  &image.RGBA{Pix: []byte{128, 0, 0, 128, 0, 0, 0, 0}, Stride: 8, Rect: image.Rect(0, 0, 2, 1)},
		"NRGBA": &image.NRGBA{Pix: []byte{255, 0, 0, 128, 0, 0, 0, 0}, Stride: 8, Rect: image.Rect(0, 0, 2, 1)},
	} {
		rgb, alpha, has := samples(img)
		if !has || alpha[0] != 128 || alpha[1] != 0 {
			t.Errorf("%s: alpha = %v (has %v)", name, alpha, has)
		}
		if !bytes.Equal(rgb, []byte{255, 0, 0, 0, 0, 0}) {
			t.Errorf("%s: rgb = %v, want straight [255 0 0 0 0 0]", name, rgb)
		}
	}
	// A malformed premultiplied pixel (colour above its alpha) saturates
	// rather than wrapping.
	bad := &image.RGBA{Pix: []byte{200, 0, 0, 100}, Stride: 4, Rect: image.Rect(0, 0, 1, 1)}
	if rgb, _, _ := samples(bad); rgb[0] != 255 {
		t.Errorf("colour above alpha: r = %d, want 255", rgb[0])
	}
	// Opaque pixels are untouched.
	op := image.NewRGBA(image.Rect(0, 0, 1, 1))
	op.Set(0, 0, color.RGBA{10, 20, 30, 255})
	if rgb, _, has := samples(op); has || !bytes.Equal(rgb, []byte{10, 20, 30}) {
		t.Errorf("opaque: rgb = %v, has alpha %v", rgb, has)
	}
}
