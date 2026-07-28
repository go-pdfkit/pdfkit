// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

// This file synthesises a minimal, parseable CFF2 (variable) OpenType font so the
// tests can exercise the CFF-subset fallback: go-opentype's SubsetCFF rejects a
// CFF2 font (an instance must be baked first), so pdfkit falls back to embedding
// the whole 'CFF2' program. The font carries two empty charstrings, which is
// enough for opentype.Parse to accept it and for pdfkit to embed it; nothing
// renders it.

// cff2Index encodes a CFF2 INDEX (a 32-bit count, unlike CFF's 16-bit) of items.
func cff2Index(items [][]byte) []byte {
	w := &bw{}
	if len(items) == 0 {
		w.u32(0)
		return w.b
	}
	total := 1
	offs := []int{1}
	for _, it := range items {
		total += len(it)
		offs = append(offs, total)
	}
	offSize := 1
	for total > (1<<(8*offSize))-1 {
		offSize++
	}
	w.u32(uint32(len(items)))
	w.u8(uint8(offSize))
	for _, o := range offs {
		for k := offSize - 1; k >= 0; k-- {
			w.u8(byte(o >> (8 * k)))
		}
	}
	for _, it := range items {
		w.b = append(w.b, it...)
	}
	return w.b
}

// cff2DictLong encodes a DICT integer operand in the fixed-width 5-byte form.
func cff2DictLong(v int) []byte {
	return []byte{29, byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

// buildCFF2Table assembles a minimal CFF2 table: a 5-byte header, a Top DICT
// carrying only the CharStrings offset (operator 17), an empty Global Subr INDEX
// and the CharStrings INDEX.
func buildCFF2Table(charStrings [][]byte) []byte {
	gsubr := cff2Index(nil)
	cs := cff2Index(charStrings)
	top := func(csOff int) []byte { return append(cff2DictLong(csOff), 17) }
	topLen := len(top(0))
	csOff := 5 + topLen + len(gsubr)
	td := top(csOff)

	out := []byte{2, 0, 5, byte(len(td) >> 8), byte(len(td))} // header + topDictLength
	out = append(out, td...)
	out = append(out, gsubr...)
	out = append(out, cs...)
	return out
}

// synthCFF2 assembles a two-glyph CFF2 OpenType font (.notdef + 'A'), the minimal
// shape opentype.Parse accepts and pdfkit embeds through its whole-table fallback.
func synthCFF2() []byte {
	cff2 := buildCFF2Table([][]byte{{}, {}}) // two empty charstrings

	head := &bw{}
	head.u32(0x00010000)
	head.u32(0)
	head.u32(0)
	head.u32(0x5F0F3CF5)
	head.u16(0)
	head.u16(1000) // unitsPerEm (offset 18)
	head.u32(0)
	head.u32(0)
	head.u32(0)
	head.u32(0)
	head.i16(0)    // xMin
	head.i16(-200) // yMin
	head.i16(700)  // xMax
	head.i16(800)  // yMax
	head.u16(0)
	head.u16(8)
	head.i16(2)
	head.i16(0) // indexToLocFormat
	head.i16(0)

	maxp := &bw{}
	maxp.u32(0x00005000)
	maxp.u16(2)

	hhea := &bw{}
	hhea.u32(0x00010000)
	hhea.i16(800)
	hhea.i16(-200)
	hhea.i16(0)
	hhea.u16(1000)
	for i := 0; i < 10; i++ {
		hhea.i16(0)
	}
	hhea.i16(0)
	hhea.u16(2) // numberOfHMetrics

	hmtx := &bw{}
	hmtx.u16(500)
	hmtx.i16(0)
	hmtx.u16(500)
	hmtx.i16(0)

	cmap := buildCmap12(map[rune]uint16{'A': 1})

	return assembleSFNT(0x4F54544F, map[string][]byte{ // OTTO
		"head": head.b,
		"maxp": maxp.b,
		"hhea": hhea.b,
		"hmtx": hmtx.b,
		"cmap": cmap,
		"CFF2": cff2,
	})
}
