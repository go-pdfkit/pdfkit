// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import "testing"

func TestLoadFontErrors(t *testing.T) {
	if _, err := LoadFont([]byte("not a font")); err == nil {
		t.Error("expected sfnt parse error")
	}
	// A valid sfnt container missing cmap: parseSFNT accepts it but opentype
	// (which requires cmap) rejects it, covering the opentype-error branch.
	if _, err := LoadFont(synthTTF(synthOpts{noCmap: true})); err == nil {
		t.Error("expected opentype parse error for missing cmap")
	}
}

func TestFontGetters(t *testing.T) {
	f, err := LoadFont(synthTTF(defaultSynth()))
	if err != nil {
		t.Fatal(err)
	}
	if f.IsCFF() {
		t.Error("synth is TrueType")
	}
	if f.UnitsPerEm() != 1000 {
		t.Errorf("unitsPerEm = %d", f.UnitsPerEm())
	}
	if f.NumGlyphs() != 4 {
		t.Errorf("numGlyphs = %d", f.NumGlyphs())
	}
	if f.BaseName() != "TestFont" {
		t.Errorf("baseName = %q", f.BaseName())
	}
	// 'H' has advance 700 at 1000 upem -> 700 in glyph space.
	gid, _ := f.ot.GlyphIndex('H')
	if w := f.glyphWidth1000(gid); w != 700 {
		t.Errorf("width = %d, want 700", w)
	}
}

func TestFontNoNameUsesDefault(t *testing.T) {
	f, err := LoadFont(synthTTF(synthOpts{}))
	if err != nil {
		t.Fatal(err)
	}
	if f.BaseName() != "PDFKitFont" {
		t.Errorf("baseName = %q, want PDFKitFont", f.BaseName())
	}
}

func TestFontUse(t *testing.T) {
	u := newFontUse()
	if !u.gids[0] {
		t.Error(".notdef not pre-registered")
	}
	u.mark(5, []rune{'x'})
	u.mark(5, []rune{'y'}) // existing gid keeps first mapping
	u.mark(3, nil)         // no runes: recorded as used, no toUni entry
	if string(u.toUni[5]) != "x" {
		t.Errorf("toUni[5] = %q", string(u.toUni[5]))
	}
	if _, ok := u.toUni[3]; ok {
		t.Error("gid 3 should have no toUni entry")
	}
	got := u.sortedGIDs()
	if len(got) != 3 || got[0] != 0 || got[1] != 3 || got[2] != 5 {
		t.Errorf("sortedGIDs = %v", got)
	}
}

func TestReadPSNameEdgeCases(t *testing.T) {
	// No name table -> empty.
	if s := readPSName(&sfntFont{tables: map[string][]byte{}}); s != "" {
		t.Errorf("no-name = %q", s)
	}
	// Header too short.
	if s := readPSName(&sfntFont{tables: map[string][]byte{"name": {0, 0}}}); s != "" {
		t.Errorf("short header = %q", s)
	}

	// Build a name table with: a nameID!=6 record, an out-of-range record, and
	// a platform-0 (Unicode) nameID-6 record.
	w := &bw{}
	w.u16(0)             // format
	w.u16(3)             // count (claims 3 records)
	w.u16(uint16(6 + 3*12)) // storage offset
	// record 0: nameID 1 (skipped)
	w.u16(3); w.u16(1); w.u16(0); w.u16(1); w.u16(4); w.u16(0)
	// record 1: nameID 6, platform 0 (Unicode UTF-16BE) "Uni"
	w.u16(0); w.u16(3); w.u16(0); w.u16(6); w.u16(6); w.u16(0)
	// record 2: nameID 6 but offset/length out of range (skipped)
	w.u16(1); w.u16(0); w.u16(0); w.u16(6); w.u16(0xffff); w.u16(0xffff)
	storage := []byte{0, 'U', 0, 'n', 0, 'i'}
	w.b = append(w.b, storage...)
	if s := readPSName(&sfntFont{tables: map[string][]byte{"name": w.b}}); s != "Uni" {
		t.Errorf("platform-0 name = %q, want Uni", s)
	}

	// A name table whose count exceeds the byte length triggers the bounds break.
	short := &bw{}
	short.u16(0)
	short.u16(10) // 10 records but no record bytes follow
	short.u16(6)
	if s := readPSName(&sfntFont{tables: map[string][]byte{"name": short.b}}); s != "" {
		t.Errorf("truncated records = %q", s)
	}
}

func TestDecodeNameMacintosh(t *testing.T) {
	// Platform 1 (Macintosh) single-byte, filtering a control byte.
	if s := decodeName([]byte{'A', 0x01, 'B'}, 1); s != "AB" {
		t.Errorf("mac decode = %q", s)
	}
}
