// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"os"
	"testing"

	"github.com/go-opentype/opentype"
)

// TestDescriptorCapHeightFallback covers both cap-height branches of
// buildDescriptor: a font with an OS/2 cap height uses it, one without falls back
// to the ascender.
func TestDescriptorCapHeightFallback(t *testing.T) {
	// With OS/2: cap height 700 (scaled by 1000/1000 = 700).
	withOS2, err := LoadFont(synthTTF(defaultSynth()))
	if err != nil {
		t.Fatal(err)
	}
	doc := New(Options{})
	p := doc.AddPage(A4)
	p.SetFont(withOS2, 12)
	if err := p.Text(72, 700, "H"); err != nil {
		t.Fatal(err)
	}
	r := reopen(t, doc)
	if got := firstFontDict(r).Key("DescendantFonts").Index(0).
		Key("FontDescriptor").Key("CapHeight").Int64(); got != 700 {
		t.Errorf("CapHeight with OS/2 = %d, want 700", got)
	}

	// Without OS/2 or post: cap height is zero upstream, so pdfkit substitutes the
	// ascender (800).
	noOS2, err := LoadFont(synthTTF(synthOpts{}))
	if err != nil {
		t.Fatal(err)
	}
	doc2 := New(Options{})
	p2 := doc2.AddPage(A4)
	p2.SetFont(noOS2, 12)
	if err := p2.Text(72, 700, "H"); err != nil {
		t.Fatal(err)
	}
	r2 := reopen(t, doc2)
	if got := firstFontDict(r2).Key("DescendantFonts").Index(0).
		Key("FontDescriptor").Key("CapHeight").Int64(); got != 800 {
		t.Errorf("CapHeight without OS/2 = %d, want ascender 800", got)
	}
}

// TestTrueTypeSubsetFallback drives the embedProgram fallback: an out-of-range
// glyph id makes SubsetTrueType error, so pdfkit embeds the whole font program
// with an Identity /CIDToGIDMap instead of a compact subset.
func TestTrueTypeSubsetFallback(t *testing.T) {
	f, err := LoadFont(synthTTF(defaultSynth()))
	if err != nil {
		t.Fatal(err)
	}
	doc := New(Options{})
	p := doc.AddPage(A4)
	p.SetFont(f, 12)
	if err := p.Text(72, 700, "H"); err != nil {
		t.Fatal(err)
	}
	// Force an out-of-range glyph into the used set so SubsetTrueType fails.
	doc.use[f].mark(opentype.GlyphIndex(f.NumGlyphs()+50), nil)

	r := reopen(t, doc)
	df := firstFontDict(r).Key("DescendantFonts").Index(0)
	// The fallback embeds the whole font, so the map is the Identity name again.
	if got := df.Key("CIDToGIDMap").Name(); got != "Identity" {
		t.Errorf("fallback CIDToGIDMap = %q, want Identity", got)
	}
	prog := readStream(t, df.Key("FontDescriptor").Key("FontFile2"))
	if len(prog) != len(f.data) {
		t.Errorf("fallback FontFile2 len = %d, want whole font %d", len(prog), len(f.data))
	}
}

// TestCFFSubsetFallbackWholeTable drives embedCFF's fallback via an out-of-range
// glyph id (SubsetCFF rejects it), which embeds the whole 'CFF ' table.
func TestCFFSubsetFallbackWholeTable(t *testing.T) {
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
	p.SetFont(f, 12)
	if err := p.Text(72, 700, "H"); err != nil {
		t.Fatal(err)
	}
	doc.use[f].mark(opentype.GlyphIndex(f.NumGlyphs()+50), nil)

	r := reopen(t, doc)
	ff := firstFontDict(r).Key("DescendantFonts").Index(0).
		Key("FontDescriptor").Key("FontFile3")
	whole, _ := f.ot.Table("CFF ")
	if got := readStream(t, ff); len(got) != len(whole) {
		t.Errorf("fallback FontFile3 len = %d, want whole CFF %d", len(got), len(whole))
	}
}

// TestCFF2WholeTableFallback loads a synthetic CFF2 (variable) font, which the
// preserve-numbering CFF subsetter cannot handle, and checks it is recognised as
// CFF and embedded whole via the 'CFF2' branch of wholeCFF.
func TestCFF2WholeTableFallback(t *testing.T) {
	f, err := LoadFont(synthCFF2())
	if err != nil {
		t.Fatalf("parse synthetic CFF2: %v", err)
	}
	if !f.IsCFF() {
		t.Fatal("CFF2 font should report IsCFF")
	}
	doc := New(Options{})
	p := doc.AddPage(A4)
	p.SetFont(f, 12)
	if err := p.Text(72, 700, "A"); err != nil {
		t.Fatal(err)
	}
	r := reopen(t, doc)
	df := firstFontDict(r).Key("DescendantFonts").Index(0)
	if got := df.Key("Subtype").Name(); got != "CIDFontType0" {
		t.Errorf("CFF2 descendant Subtype = %q", got)
	}
	ff := df.Key("FontDescriptor").Key("FontFile3")
	whole, _ := f.ot.Table("CFF2")
	if got := readStream(t, ff); len(got) != len(whole) {
		t.Errorf("CFF2 FontFile3 len = %d, want whole CFF2 %d", len(got), len(whole))
	}
}
