// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"testing"

	"github.com/go-opentype/opentype"
)

// validHead returns a minimal 54-byte head with unitsPerEm 1000 and short loca.
func validHead() []byte {
	b := make([]byte, 54)
	u16w(b, 18, 1000) // unitsPerEm
	return b
}

// validMaxp returns a 6-byte maxp declaring n glyphs.
func validMaxp(n int) []byte {
	b := make([]byte, 6)
	u16w(b, 4, uint16(n))
	return b
}

// validHhea returns a 36-byte hhea declaring numberOfHMetrics.
func validHhea(m int) []byte {
	b := make([]byte, 36)
	u16w(b, 34, uint16(m))
	return b
}

func u16w(b []byte, i int, v uint16) {
	b[i] = byte(v >> 8)
	b[i+1] = byte(v)
}

// TestParseSFNTSubParserErrors drives each sub-parser error return inside
// parseSFNT by assembling a container with exactly one broken table.
func TestParseSFNTSubParserErrors(t *testing.T) {
	cases := map[string]map[string][]byte{
		"bad head": {"head": make([]byte, 20)}, // >=12 so assembly can patch, <54 so parseHead fails
		"bad maxp": {"head": validHead(), "maxp": make([]byte, 2)},
		"bad hhea": {"head": validHead(), "maxp": validMaxp(2), "hhea": make([]byte, 4)},
		"bad hmtx": {"head": validHead(), "maxp": validMaxp(2), "hhea": validHhea(2), "hmtx": make([]byte, 2)},
		"no loca": {
			"head": validHead(), "maxp": validMaxp(2), "hhea": validHhea(2),
			"hmtx": make([]byte, 8),
		},
	}
	for name, tables := range cases {
		if _, err := parseSFNT(assembleSFNT(0x00010000, tables)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

// TestParseSFNTCFF2 covers the CFF2 outline-flavour branch: a container with a
// CFF2 table is treated as CFF (isCFF true) and needs no loca/glyf.
func TestParseSFNTCFF2(t *testing.T) {
	tables := map[string][]byte{
		"head": validHead(),
		"maxp": validMaxp(2),
		"hhea": validHhea(2),
		"hmtx": make([]byte, 8),
		"CFF2": {1, 2, 3, 4},
	}
	sf, err := parseSFNT(assembleSFNT(0x00010000, tables))
	if err != nil {
		t.Fatal(err)
	}
	if !sf.isCFF {
		t.Error("CFF2 font should be marked CFF")
	}
}

// TestSubsetOddGlyphPadding covers the 2-byte alignment padding of an
// odd-length glyph in subsetTrueType.
func TestSubsetOddGlyphPadding(t *testing.T) {
	sf := &sfntFont{
		numGlyphs: 2,
		unitsPerEm: 1000,
		loca:      []uint32{0, 3, 3}, // glyph 1 is three bytes long (odd)
		tables: map[string][]byte{
			"head": validHead(),
			"hhea": validHhea(2),
			"maxp": validMaxp(2),
			"hmtx": make([]byte, 8),
			"glyf": {0, 0, 0}, // three bytes
		},
	}
	out := subsetTrueType(sf, []opentype.GlyphIndex{1})
	if len(out) == 0 {
		t.Fatal("empty subset")
	}
}
