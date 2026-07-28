// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"encoding/binary"

	"github.com/go-opentype/opentype"
)

// TrueType composite-glyph flags relevant to walking component references.
const (
	argsAreWords   = 0x0001
	moreComponents = 0x0020
	haveScale      = 0x0008
	haveXYScale    = 0x0040
	have2x2        = 0x0080
)

// subsetTrueType returns a minimal but valid TrueType font program containing
// only the glyphs in gids (plus every glyph a composite among them references,
// found transitively). Glyph numbering is preserved, so the PDF font can use an
// Identity CIDToGIDMap; unused glyphs become empty entries. This subsetter is
// implemented in pdfkit because go-opentype exposes no glyf/loca export.
func subsetTrueType(sf *sfntFont, gids []opentype.GlyphIndex) []byte {
	needed := map[int]bool{0: true}
	for _, g := range gids {
		collectComponents(sf, int(g), needed)
	}

	// Rebuild glyf and a long-format loca preserving glyph ids.
	var glyf []byte
	loca := make([]uint32, sf.numGlyphs+1)
	for gid := 0; gid < sf.numGlyphs; gid++ {
		loca[gid] = uint32(len(glyf))
		if needed[gid] {
			d := sf.glyphData(gid)
			glyf = append(glyf, d...)
			for len(glyf)%2 != 0 { // 2-byte align each glyph
				glyf = append(glyf, 0)
			}
		}
	}
	loca[sf.numGlyphs] = uint32(len(glyf))

	locaBytes := make([]byte, len(loca)*4)
	for i, off := range loca {
		binary.BigEndian.PutUint32(locaBytes[i*4:], off)
	}

	head := cloneBytes(sf.tables["head"])
	binary.BigEndian.PutUint32(head[8:], 0)  // checkSumAdjustment: recomputed later
	binary.BigEndian.PutUint16(head[50:], 1) // indexToLocFormat: long

	tables := map[string][]byte{
		"head": head,
		"hhea": sf.tables["hhea"],
		"maxp": sf.tables["maxp"],
		"hmtx": sf.tables["hmtx"],
		"loca": locaBytes,
		"glyf": glyf,
	}
	// Carry over the cmap (so the subset is a self-contained, parseable font) and
	// the TrueType instruction tables (so hinted glyphs still render as designed)
	// when present.
	for _, opt := range []string{"cmap", "cvt ", "fpgm", "prep"} {
		if b, ok := sf.tables[opt]; ok {
			tables[opt] = b
		}
	}
	return assembleSFNT(0x00010000, tables)
}

// collectComponents adds gid and, when it is a composite glyph, every component
// glyph it references (transitively) to the needed set.
func collectComponents(sf *sfntFont, gid int, needed map[int]bool) {
	if gid < 0 || gid >= sf.numGlyphs || needed[gid] {
		return
	}
	needed[gid] = true
	d := sf.glyphData(gid)
	if len(d) < 10 {
		return
	}
	if int16(binary.BigEndian.Uint16(d)) >= 0 {
		return // simple glyph, no components
	}
	p := 10
	for p+4 <= len(d) {
		flags := binary.BigEndian.Uint16(d[p:])
		comp := int(binary.BigEndian.Uint16(d[p+2:]))
		p += 4
		if flags&argsAreWords != 0 {
			p += 4
		} else {
			p += 2
		}
		switch {
		case flags&haveScale != 0:
			p += 2
		case flags&haveXYScale != 0:
			p += 4
		case flags&have2x2 != 0:
			p += 8
		}
		collectComponents(sf, comp, needed)
		if flags&moreComponents == 0 {
			break
		}
	}
}

// cloneBytes returns a copy of b so in-place patches do not touch the original
// font data.
func cloneBytes(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// assembleSFNT packs tables into an sfnt container with the given version,
// computing the directory, per-table checksums and the head checkSumAdjustment.
func assembleSFNT(version uint32, tables map[string][]byte) []byte {
	tags := make([]string, 0, len(tables))
	for t := range tables {
		tags = append(tags, t)
	}
	// Directory records must be sorted by tag.
	for i := 1; i < len(tags); i++ {
		for j := i; j > 0 && tags[j-1] > tags[j]; j-- {
			tags[j-1], tags[j] = tags[j], tags[j-1]
		}
	}

	n := len(tags)
	entrySelector := 0
	for (1 << (entrySelector + 1)) <= n {
		entrySelector++
	}
	searchRange := (1 << entrySelector) * 16
	rangeShift := n*16 - searchRange

	headerLen := 12 + 16*n
	// Lay out padded table bodies after the directory.
	offsets := make(map[string]int, n)
	var body []byte
	for _, t := range tags {
		offsets[t] = headerLen + len(body)
		body = append(body, tables[t]...)
		for len(body)%4 != 0 {
			body = append(body, 0)
		}
	}

	out := make([]byte, headerLen)
	binary.BigEndian.PutUint32(out[0:], version)
	binary.BigEndian.PutUint16(out[4:], uint16(n))
	binary.BigEndian.PutUint16(out[6:], uint16(searchRange))
	binary.BigEndian.PutUint16(out[8:], uint16(entrySelector))
	binary.BigEndian.PutUint16(out[10:], uint16(rangeShift))
	for i, t := range tags {
		rec := 12 + i*16
		copy(out[rec:], t)
		binary.BigEndian.PutUint32(out[rec+4:], tableChecksum(tables[t]))
		binary.BigEndian.PutUint32(out[rec+8:], uint32(offsets[t]))
		binary.BigEndian.PutUint32(out[rec+12:], uint32(len(tables[t])))
	}
	out = append(out, body...)

	// Patch head.checkSumAdjustment = 0xB1B0AFBA - checksum(whole file).
	if headOff, ok := offsets["head"]; ok {
		adj := 0xB1B0AFBA - tableChecksum(out)
		binary.BigEndian.PutUint32(out[headOff+8:], adj)
	}
	return out
}

// tableChecksum is the sum (mod 2^32) of the data read as big-endian uint32s,
// zero-padded to a multiple of four bytes.
func tableChecksum(b []byte) uint32 {
	var sum uint32
	var i int
	for ; i+4 <= len(b); i += 4 {
		sum += binary.BigEndian.Uint32(b[i:])
	}
	if rem := len(b) - i; rem > 0 {
		var tail [4]byte
		copy(tail[:], b[i:])
		sum += binary.BigEndian.Uint32(tail[:])
	}
	return sum
}
