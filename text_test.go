// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"os"
	"strings"
	"testing"
)

func loadSynth(t *testing.T) (*Document, *Page, *Font) {
	t.Helper()
	f, err := LoadFont(synthTTF(defaultSynth()))
	if err != nil {
		t.Fatal(err)
	}
	doc := New(Options{})
	return doc, doc.AddPage(A4), f
}

func TestTextNoFont(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	if err := p.Text(0, 0, "x"); err != errNoFont {
		t.Errorf("Text err = %v", err)
	}
	if err := p.TextLines(0, 0, []string{"x"}); err != errNoFont {
		t.Errorf("TextLines err = %v", err)
	}
	if err := p.TextShaped(0, 0, "x"); err != errNoFont {
		t.Errorf("TextShaped err = %v", err)
	}
	if w := p.TextWidth("x"); w != 0 {
		t.Errorf("TextWidth = %v, want 0", w)
	}
	if p.WrapText("x", 100) != nil {
		t.Error("WrapText should be nil without a font")
	}
}

func TestTextStateAndShow(t *testing.T) {
	doc, p, f := loadSynth(t)
	p.SetFont(f, 12)
	p.SetCharSpacing(1)
	p.SetWordSpacing(2)
	p.SetLeading(14)
	p.SetRenderMode(RenderStroke)
	if err := p.Text(72, 700, "Hi"); err != nil {
		t.Fatal(err)
	}
	c := content(p)
	for _, want := range []string{"BT\n", "/F0 12 Tf", "1 Tc", "2 Tw", "1 Tr", "72 700 Td", "<00010002> Tj", "ET\n"} {
		if !strings.Contains(c, want) {
			t.Errorf("content missing %q\n%s", want, c)
		}
	}
	_ = doc
}

func TestTextLinesAndWrap(t *testing.T) {
	doc, p, f := loadSynth(t)
	_ = doc
	p.SetFont(f, 10)
	p.SetLeading(12)
	if err := p.TextLines(50, 500, []string{"Hi", "iH", "HH"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content(p), "T*\n") {
		t.Error("TextLines missing T* between lines")
	}

	// Width of 'H' at size 10 is 7 points; pick a max width that forces wrapping
	// and also make one "word" a single glyph so it lands alone.
	lines := p.WrapText("H H H", p.TextWidth("H H")+0.1)
	if len(lines) < 2 {
		t.Errorf("expected wrapping, got %v", lines)
	}
	// A single word wider than the limit occupies its own line.
	solo := p.WrapText("HH", 1)
	if len(solo) != 1 || solo[0] != "HH" {
		t.Errorf("overflow word = %v", solo)
	}
}

func TestEncodeUnmappedGlyph(t *testing.T) {
	doc, p, f := loadSynth(t)
	_ = doc
	p.SetFont(f, 12)
	// 'Z' is not in the synth cmap, so it maps to glyph 0 (.notdef).
	if err := p.Text(0, 0, "Z"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content(p), "<0000> Tj") {
		t.Errorf("unmapped rune not encoded as .notdef\n%s", content(p))
	}
}

func TestTextShapedAligned(t *testing.T) {
	doc, p, f := loadSynth(t)
	_ = doc
	p.SetFont(f, 20)
	if err := p.TextShaped(100, 100, "HiA"); err != nil {
		t.Fatal(err)
	}
	c := content(p)
	if !strings.Contains(c, "Tm") || !strings.Contains(c, "Tj") {
		t.Errorf("shaped output missing Tm/Tj\n%s", c)
	}
}

func TestTextShapedLigature(t *testing.T) {
	otf, err := os.ReadFile("testdata/SourceSerif4-Regular.otf")
	if err != nil {
		t.Fatal(err)
	}
	f, err := LoadFont(otf)
	if err != nil {
		t.Fatal(err)
	}
	doc := New(Options{})
	p := doc.AddPage(A4)
	p.SetFont(f, 18)
	// "office" + liga collapses to fewer glyphs than runes, exercising the
	// unaligned ToUnicode attribution (first glyph carries the whole run).
	if err := p.TextShaped(72, 700, "office", "liga"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content(p), "Tm") {
		t.Error("shaped ligature output missing Tm")
	}
}
