// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"encoding/binary"
	"fmt"
)

// sfntFont is pdfkit's own light re-parse of the sfnt container behind a font
// blob. go-opentype/opentype decodes the same bytes for glyph indexing,
// metrics and shaping, but it does not expose the raw table data, the
// units-per-em, the glyf/loca arrays or a subsetting export that embedding a
// font into a PDF requires; pdfkit therefore reparses the container it is
// handed. See the package doc for the list of missing upstream primitives.
type sfntFont struct {
	data   []byte
	tables map[string][]byte

	unitsPerEm       int
	numGlyphs        int
	indexToLocFormat int
	numHMetrics      int
	ascender         int
	descender        int
	xMin, yMin       int
	xMax, yMax       int
	italicAngle      float64
	capHeight        int
	flags            int
	isCFF            bool

	loca     []uint32 // byte offsets into glyf, length numGlyphs+1 (TrueType only)
	advances []int    // advance width per glyph, in font units
}

// u16 reads a big-endian uint16 at b[i:].
func u16(b []byte, i int) int { return int(binary.BigEndian.Uint16(b[i:])) }

// s16 reads a big-endian int16 at b[i:].
func s16(b []byte, i int) int { return int(int16(binary.BigEndian.Uint16(b[i:]))) }

// u32 reads a big-endian uint32 at b[i:].
func u32(b []byte, i int) uint32 { return binary.BigEndian.Uint32(b[i:]) }

// parseSFNT decodes the container-level tables pdfkit needs for embedding.
func parseSFNT(data []byte) (*sfntFont, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("pdfkit: short sfnt header")
	}
	version := u32(data, 0)
	switch version {
	case 0x00010000, 0x74727565, 0x4F54544F: // TrueType, "true", "OTTO"
	default:
		return nil, fmt.Errorf("pdfkit: unrecognised sfnt version 0x%08x", version)
	}
	numTables := u16(data, 4)
	if 12+numTables*16 > len(data) {
		return nil, fmt.Errorf("pdfkit: truncated table directory")
	}
	tables := make(map[string][]byte, numTables)
	for i := 0; i < numTables; i++ {
		rec := data[12+i*16:]
		tag := string(rec[0:4])
		off := int(u32(rec, 8))
		length := int(u32(rec, 12))
		if off < 0 || length < 0 || off+length > len(data) {
			return nil, fmt.Errorf("pdfkit: table %q out of range", tag)
		}
		tables[tag] = data[off : off+length]
	}

	f := &sfntFont{data: data, tables: tables}
	if err := f.parseHead(); err != nil {
		return nil, err
	}
	if err := f.parseMaxp(); err != nil {
		return nil, err
	}
	if err := f.parseHhea(); err != nil {
		return nil, err
	}
	if err := f.parseHmtx(); err != nil {
		return nil, err
	}
	f.parseOptional()

	if _, ok := tables["CFF "]; ok {
		f.isCFF = true
	} else if _, ok := tables["CFF2"]; ok {
		f.isCFF = true
	}
	if !f.isCFF {
		if err := f.parseLoca(); err != nil {
			return nil, err
		}
	}
	return f, nil
}

func (f *sfntFont) parseHead() error {
	b, ok := f.tables["head"]
	if !ok || len(b) < 54 {
		return fmt.Errorf("pdfkit: missing or short head table")
	}
	f.unitsPerEm = u16(b, 18)
	if f.unitsPerEm == 0 {
		return fmt.Errorf("pdfkit: head unitsPerEm is zero")
	}
	f.xMin = s16(b, 36)
	f.yMin = s16(b, 38)
	f.xMax = s16(b, 40)
	f.yMax = s16(b, 42)
	f.indexToLocFormat = s16(b, 50)
	return nil
}

func (f *sfntFont) parseMaxp() error {
	b, ok := f.tables["maxp"]
	if !ok || len(b) < 6 {
		return fmt.Errorf("pdfkit: missing or short maxp table")
	}
	f.numGlyphs = u16(b, 4)
	if f.numGlyphs == 0 {
		return fmt.Errorf("pdfkit: maxp numGlyphs is zero")
	}
	return nil
}

func (f *sfntFont) parseHhea() error {
	b, ok := f.tables["hhea"]
	if !ok || len(b) < 36 {
		return fmt.Errorf("pdfkit: missing or short hhea table")
	}
	f.ascender = s16(b, 4)
	f.descender = s16(b, 6)
	f.numHMetrics = u16(b, 34)
	if f.numHMetrics == 0 {
		return fmt.Errorf("pdfkit: hhea numberOfHMetrics is zero")
	}
	return nil
}

func (f *sfntFont) parseHmtx() error {
	b, ok := f.tables["hmtx"]
	if !ok || len(b) < f.numHMetrics*4 {
		return fmt.Errorf("pdfkit: missing or short hmtx table")
	}
	f.advances = make([]int, f.numGlyphs)
	last := 0
	for i := 0; i < f.numGlyphs; i++ {
		if i < f.numHMetrics {
			last = u16(b, i*4)
		}
		f.advances[i] = last
	}
	return nil
}

// parseOptional reads descriptor hints from post and OS/2 when present, filling
// in sensible defaults otherwise. These feed the PDF font descriptor.
func (f *sfntFont) parseOptional() {
	f.flags = 32 // Nonsymbolic
	if b, ok := f.tables["post"]; ok && len(b) >= 16 {
		f.italicAngle = float64(int32(u32(b, 4))) / 65536
		if u32(b, 12) != 0 { // isFixedPitch
			f.flags |= 1
		}
	}
	if f.italicAngle != 0 {
		f.flags |= 64
	}
	if b, ok := f.tables["OS/2"]; ok && len(b) >= 90 && u16(b, 0) >= 2 {
		f.capHeight = s16(b, 88)
	}
	if f.capHeight == 0 {
		f.capHeight = f.ascender
	}
}

func (f *sfntFont) parseLoca() error {
	b, ok := f.tables["loca"]
	if !ok {
		return fmt.Errorf("pdfkit: missing loca table")
	}
	n := f.numGlyphs + 1
	f.loca = make([]uint32, n)
	if f.indexToLocFormat == 0 {
		if len(b) < n*2 {
			return fmt.Errorf("pdfkit: short short-format loca table")
		}
		for i := 0; i < n; i++ {
			f.loca[i] = uint32(u16(b, i*2)) * 2
		}
	} else {
		if len(b) < n*4 {
			return fmt.Errorf("pdfkit: short long-format loca table")
		}
		for i := 0; i < n; i++ {
			f.loca[i] = u32(b, i*4)
		}
	}
	if _, ok := f.tables["glyf"]; !ok {
		return fmt.Errorf("pdfkit: missing glyf table")
	}
	return nil
}

// glyphData returns the glyf bytes for glyph gid, or an empty slice when the
// glyph has no outline (loca[gid]==loca[gid+1]).
func (f *sfntFont) glyphData(gid int) []byte {
	if gid < 0 || gid+1 >= len(f.loca) {
		return nil
	}
	start, end := f.loca[gid], f.loca[gid+1]
	glyf := f.tables["glyf"]
	if start >= end || int(end) > len(glyf) {
		return nil
	}
	return glyf[start:end]
}
