// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"encoding/binary"
	"testing"
)

func TestParseSFNTContainerErrors(t *testing.T) {
	cases := map[string][]byte{
		"short header": {0, 0, 0},
		"bad version":  {0xDE, 0xAD, 0xBE, 0xEF, 0, 0, 0, 0, 0, 0, 0, 0},
	}
	for name, data := range cases {
		if _, err := parseSFNT(data); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}

	// A directory that claims more tables than the bytes hold.
	big := make([]byte, 12)
	binary.BigEndian.PutUint32(big[0:], 0x00010000)
	binary.BigEndian.PutUint16(big[4:], 100)
	if _, err := parseSFNT(big); err == nil {
		t.Error("truncated directory: expected error")
	}

	// One record whose offset+length runs past the data.
	bad := make([]byte, 12+16)
	binary.BigEndian.PutUint32(bad[0:], 0x00010000)
	binary.BigEndian.PutUint16(bad[4:], 1)
	copy(bad[12:], "head")
	binary.BigEndian.PutUint32(bad[12+8:], 12+16) // offset
	binary.BigEndian.PutUint32(bad[12+12:], 9999) // length
	if _, err := parseSFNT(bad); err == nil {
		t.Error("out-of-range table: expected error")
	}
}

// tbl builds a single-table sfnt around one table so a sub-parser error can be
// provoked without hand-assembling a whole font.
func sfntWith(tables map[string][]byte) *sfntFont {
	return &sfntFont{tables: tables}
}

func TestParseHeadErrors(t *testing.T) {
	if err := sfntWith(nil).parseHead(); err == nil {
		t.Error("missing head")
	}
	h := make([]byte, 54)
	binary.BigEndian.PutUint16(h[18:], 0) // unitsPerEm zero
	if err := sfntWith(map[string][]byte{"head": h}).parseHead(); err == nil {
		t.Error("zero unitsPerEm")
	}
}

func TestParseMaxpErrors(t *testing.T) {
	if err := sfntWith(nil).parseMaxp(); err == nil {
		t.Error("missing maxp")
	}
	m := make([]byte, 6) // numGlyphs zero
	if err := sfntWith(map[string][]byte{"maxp": m}).parseMaxp(); err == nil {
		t.Error("zero numGlyphs")
	}
}

func TestParseHheaErrors(t *testing.T) {
	if err := sfntWith(nil).parseHhea(); err == nil {
		t.Error("missing hhea")
	}
	h := make([]byte, 36) // numberOfHMetrics zero
	if err := sfntWith(map[string][]byte{"hhea": h}).parseHhea(); err == nil {
		t.Error("zero numberOfHMetrics")
	}
}

func TestParseHmtxError(t *testing.T) {
	f := sfntWith(map[string][]byte{"hmtx": {0, 0}})
	f.numHMetrics = 4
	if err := f.parseHmtx(); err == nil {
		t.Error("short hmtx")
	}
}

func TestParseLocaErrors(t *testing.T) {
	// Missing loca table.
	f := sfntWith(map[string][]byte{})
	f.numGlyphs = 2
	if err := f.parseLoca(); err == nil {
		t.Error("missing loca")
	}
	// Short short-format loca.
	f = sfntWith(map[string][]byte{"loca": {0, 0}})
	f.numGlyphs = 4
	f.indexToLocFormat = 0
	if err := f.parseLoca(); err == nil {
		t.Error("short short-loca")
	}
	// Short long-format loca.
	f = sfntWith(map[string][]byte{"loca": {0, 0, 0, 0}})
	f.numGlyphs = 4
	f.indexToLocFormat = 1
	if err := f.parseLoca(); err == nil {
		t.Error("short long-loca")
	}
	// Well-formed loca but missing glyf.
	loca := make([]byte, (3)*2)
	f = sfntWith(map[string][]byte{"loca": loca})
	f.numGlyphs = 2
	f.indexToLocFormat = 0
	if err := f.parseLoca(); err == nil {
		t.Error("missing glyf")
	}
}

func TestParseLocaLongFormatAndVariants(t *testing.T) {
	// A long-format loca font round-trips through LoadFont and embeds.
	f, err := LoadFont(synthTTF(synthOpts{longLoca: true, withName: true, withPost: true, withOS2: true, capHeight: 700}))
	if err != nil {
		t.Fatal(err)
	}
	if f.sf.indexToLocFormat != 1 {
		t.Errorf("indexToLocFormat = %d, want 1", f.sf.indexToLocFormat)
	}
}

func TestParseOptionalVariants(t *testing.T) {
	// Italic + fixed pitch post, no OS/2: capHeight falls back to the ascender.
	f, err := LoadFont(synthTTF(synthOpts{italicAngle: -12, fixedPitch: true, withPost: true}))
	if err != nil {
		t.Fatal(err)
	}
	if f.sf.italicAngle != -12 {
		t.Errorf("italicAngle = %v", f.sf.italicAngle)
	}
	if f.sf.flags&1 == 0 || f.sf.flags&64 == 0 {
		t.Errorf("flags = %d, want fixed-pitch and italic bits", f.sf.flags)
	}
	if f.sf.capHeight != f.sf.ascender {
		t.Errorf("capHeight = %d, want ascender %d", f.sf.capHeight, f.sf.ascender)
	}

	// No post table at all: italic angle stays zero.
	f2, err := LoadFont(synthTTF(synthOpts{withOS2: true, capHeight: 650}))
	if err != nil {
		t.Fatal(err)
	}
	if f2.sf.italicAngle != 0 || f2.sf.capHeight != 650 {
		t.Errorf("no-post font: italic=%v cap=%d", f2.sf.italicAngle, f2.sf.capHeight)
	}
}

func TestGlyphDataBounds(t *testing.T) {
	f, err := LoadFont(synthTTF(defaultSynth()))
	if err != nil {
		t.Fatal(err)
	}
	if d := f.sf.glyphData(-1); d != nil {
		t.Error("negative gid should be nil")
	}
	if d := f.sf.glyphData(999); d != nil {
		t.Error("out-of-range gid should be nil")
	}
	if d := f.sf.glyphData(0); d != nil {
		t.Error(".notdef has no outline, want nil")
	}
	if d := f.sf.glyphData(1); d == nil {
		t.Error("glyph 1 should have outline data")
	}
}
