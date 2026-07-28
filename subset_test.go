// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import "testing"

// craftGlyfFont builds a bare sfntFont whose glyphData is driven by the given
// glyph blobs, for testing composite-component walking in isolation.
func craftGlyfFont(glyphs [][]byte) *sfntFont {
	f := &sfntFont{numGlyphs: len(glyphs), tables: map[string][]byte{}}
	var glyf []byte
	f.loca = make([]uint32, len(glyphs)+1)
	for i, g := range glyphs {
		f.loca[i] = uint32(len(glyf))
		glyf = append(glyf, g...)
		for len(glyf)%2 != 0 {
			glyf = append(glyf, 0)
		}
	}
	f.loca[len(glyphs)] = uint32(len(glyf))
	f.tables["glyf"] = glyf
	return f
}

// compositeRef1 builds a one-component composite referencing glyph 1 with the
// given extra transform flag (0, haveScale, haveXYScale or have2x2).
func compositeRef1(extraFlag uint16) []byte {
	w := &bw{}
	w.i16(-1) // composite
	w.i16(0)
	w.i16(0)
	w.i16(10)
	w.i16(10)
	w.u16(0x0002 | extraFlag) // ARGS_ARE_XY_VALUES + transform, no MORE, byte args
	w.u16(1)                  // glyphIndex
	w.u8(0)                   // arg1 (byte)
	w.u8(0)                   // arg2 (byte)
	switch extraFlag {
	case haveScale:
		w.i16(0x4000) // one F2Dot14 scale
	case haveXYScale:
		w.i16(0x4000)
		w.i16(0x4000)
	case have2x2:
		w.i16(0x4000)
		w.i16(0)
		w.i16(0)
		w.i16(0x4000)
	}
	return w.b
}

func TestCollectComponents(t *testing.T) {
	simple := simpleBox(0, 0, 10, 10)
	for _, flag := range []uint16{0, haveScale, haveXYScale, have2x2} {
		f := craftGlyfFont([][]byte{nil, simple, compositeRef1(flag)})
		need := map[int]bool{}
		collectComponents(f, 2, need)
		if !need[1] || !need[2] {
			t.Errorf("flag %#x: components not collected: %v", flag, need)
		}
	}

	// Guard branches: out-of-range and already-visited return without effect.
	f := craftGlyfFont([][]byte{nil, simpleBox(0, 0, 10, 10)})
	need := map[int]bool{}
	collectComponents(f, -1, need)  // negative
	collectComponents(f, 99, need)  // >= numGlyphs
	collectComponents(f, 0, need)   // empty glyph (len(d) < 10)
	collectComponents(f, 1, need)   // simple glyph returns early
	before := len(need)
	collectComponents(f, 1, need)   // already visited: no change
	if len(need) != before {
		t.Error("already-visited glyph mutated the set")
	}
}

func TestAssembleSFNTNoHead(t *testing.T) {
	// Exercises the branch where no head table is present (no checksum patch).
	out := assembleSFNT(0x00010000, map[string][]byte{"test": {1, 2, 3, 4, 5}})
	if len(out) == 0 {
		t.Fatal("empty sfnt")
	}
	// Directory should advertise one table.
	if u16(out, 4) != 1 {
		t.Errorf("numTables = %d", u16(out, 4))
	}
}

func TestTableChecksumPadding(t *testing.T) {
	// A length that is not a multiple of four exercises the tail padding.
	if got := tableChecksum([]byte{0, 0, 0, 1, 2}); got != 1+0x02000000 {
		t.Errorf("checksum = %#x", got)
	}
}
