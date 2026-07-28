// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import "encoding/binary"

// assembleSFNT packs tables into an sfnt container with the given version,
// computing the directory, per-table checksums and the head checkSumAdjustment.
// It is a test-only helper for synthesising fonts: the production code no longer
// assembles sfnt containers (go-opentype's subsetters do), so this lives with the
// synth builder rather than in the package.
func assembleSFNT(version uint32, tables map[string][]byte) []byte {
	tags := make([]string, 0, len(tables))
	for t := range tables {
		tags = append(tags, t)
	}
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

// bw is a minimal big-endian byte writer for synthesising font tables in tests.
type bw struct{ b []byte }

func (w *bw) u8(v uint8)   { w.b = append(w.b, v) }
func (w *bw) u16(v uint16) { w.b = binary.BigEndian.AppendUint16(w.b, v) }
func (w *bw) u32(v uint32) { w.b = binary.BigEndian.AppendUint32(w.b, v) }
func (w *bw) i16(v int16)  { w.u16(uint16(v)) }

// synthOpts parameterises the synthetic TrueType font so tests can exercise the
// optional-table branches of the sfnt reader.
type synthOpts struct {
	italicAngle float64
	fixedPitch  bool
	withOS2     bool
	capHeight   int16
	withName    bool
	withPost    bool
	longLoca    bool
	noCmap      bool // omit cmap: valid container, but opentype.Parse rejects it
}

// defaultSynth returns options for a well-formed regular font.
func defaultSynth() synthOpts {
	return synthOpts{withOS2: true, capHeight: 700, withName: true, withPost: true}
}

// simpleBox builds a single-contour glyph outlining the rectangle
// (xMin,yMin)-(xMax,yMax) with four on-curve points.
func simpleBox(xMin, yMin, xMax, yMax int16) []byte {
	w := &bw{}
	w.i16(1) // numberOfContours
	w.i16(xMin)
	w.i16(yMin)
	w.i16(xMax)
	w.i16(yMax)
	w.u16(3) // endPtsOfContours[0] -> 4 points
	w.u16(0) // instructionLength
	for i := 0; i < 4; i++ {
		w.u8(0x01) // ON_CURVE_POINT, 16-bit x and y deltas
	}
	xs := []int16{xMin, xMax - xMin, 0, xMin - xMax}
	ys := []int16{yMin, 0, yMax - yMin, 0}
	for _, d := range xs {
		w.i16(d)
	}
	for _, d := range ys {
		w.i16(d)
	}
	return w.b
}

// compositeAB builds a composite glyph referencing glyphs 1 and 2, the second
// offset to the right, exercising both the words and bytes argument encodings.
func compositeAB() []byte {
	w := &bw{}
	w.i16(-1) // composite
	w.i16(0)
	w.i16(0)
	w.i16(200)
	w.i16(100)
	// component 1: ARGS_ARE_WORDS | ARGS_ARE_XY_VALUES | MORE_COMPONENTS
	w.u16(0x0023)
	w.u16(1)
	w.i16(0)
	w.i16(0)
	// component 2: ARGS_ARE_XY_VALUES only (byte-sized args, last component)
	w.u16(0x0002)
	w.u16(2)
	w.u8(120)
	w.u8(0)
	return w.b
}

// synthTTF assembles a valid TrueType ('glyf') font with four glyphs: an empty
// .notdef, two boxes mapped to 'H' and 'i', and a composite mapped to 'A'.
func synthTTF(o synthOpts) []byte {
	glyphs := [][]byte{
		nil, // .notdef, no outline
		simpleBox(0, 0, 700, 700),
		simpleBox(0, 0, 400, 700),
		compositeAB(),
	}
	numGlyphs := len(glyphs)

	// glyf + loca (offsets are byte offsets; short loca stores them halved).
	var glyf []byte
	offs := make([]uint32, numGlyphs+1)
	for i, g := range glyphs {
		offs[i] = uint32(len(glyf))
		glyf = append(glyf, g...)
		for len(glyf)%2 != 0 {
			glyf = append(glyf, 0)
		}
	}
	offs[numGlyphs] = uint32(len(glyf))

	loca := &bw{}
	for _, off := range offs {
		if o.longLoca {
			loca.u32(off)
		} else {
			loca.u16(uint16(off / 2))
		}
	}

	head := &bw{}
	head.u32(0x00010000) // version
	head.u32(0)          // fontRevision
	head.u32(0)          // checkSumAdjustment
	head.u32(0x5F0F3CF5) // magic
	head.u16(0)          // flags
	head.u16(1000)       // unitsPerEm (offset 18)
	head.u32(0)          // created hi
	head.u32(0)          // created lo
	head.u32(0)          // modified hi
	head.u32(0)          // modified lo
	head.i16(0)          // xMin (offset 36)
	head.i16(-200)       // yMin
	head.i16(700)        // xMax
	head.i16(800)        // yMax
	head.u16(0)          // macStyle
	head.u16(8)          // lowestRecPPEM
	head.i16(2)          // fontDirectionHint
	if o.longLoca {      // indexToLocFormat (offset 50)
		head.i16(1)
	} else {
		head.i16(0)
	}
	head.i16(0) // glyphDataFormat

	maxp := &bw{}
	maxp.u32(0x00005000) // version 0.5
	maxp.u16(uint16(numGlyphs))

	hhea := &bw{}
	hhea.u32(0x00010000) // version
	hhea.i16(800)        // ascender (offset 4)
	hhea.i16(-200)       // descender
	hhea.i16(0)          // lineGap
	hhea.u16(1000)       // advanceWidthMax
	for i := 0; i < 10; i++ {
		hhea.i16(0) // minLSB..reserved, filling offsets 12..32
	}
	hhea.i16(0)                 // metricDataFormat (offset 32)
	hhea.u16(uint16(numGlyphs)) // numberOfHMetrics (offset 34)

	advances := []uint16{600, 700, 400, 900}
	hmtx := &bw{}
	for i := 0; i < numGlyphs; i++ {
		hmtx.u16(advances[i])
		hmtx.i16(0) // lsb
	}

	cmap := buildCmap12(map[rune]uint16{'A': 3, 'H': 1, 'i': 2})

	tables := map[string][]byte{
		"head": head.b,
		"maxp": maxp.b,
		"hhea": hhea.b,
		"hmtx": hmtx.b,
		"cmap": cmap,
		"loca": loca.b,
		"glyf": glyf,
	}
	if o.noCmap {
		delete(tables, "cmap")
	}
	if o.withName {
		tables["name"] = buildName("TestFont")
	}
	if o.withPost {
		tables["post"] = buildPost(o.italicAngle, o.fixedPitch)
	}
	if o.withOS2 {
		tables["OS/2"] = buildOS2(o.capHeight)
	}
	return assembleSFNT(0x00010000, tables)
}

// buildCmap12 builds a cmap table with a single format-12 (platform 3/10)
// subtable from a rune->glyph map.
func buildCmap12(m map[rune]uint16) []byte {
	// Sort runes ascending for the required group ordering.
	runes := make([]rune, 0, len(m))
	for r := range m {
		runes = append(runes, r)
	}
	for i := 1; i < len(runes); i++ {
		for j := i; j > 0 && runes[j-1] > runes[j]; j-- {
			runes[j-1], runes[j] = runes[j], runes[j-1]
		}
	}
	sub := &bw{}
	sub.u16(12) // format
	sub.u16(0)  // reserved
	sub.u32(uint32(16 + 12*len(runes)))
	sub.u32(0) // language
	sub.u32(uint32(len(runes)))
	for _, r := range runes {
		sub.u32(uint32(r))
		sub.u32(uint32(r))
		sub.u32(uint32(m[r]))
	}

	t := &bw{}
	t.u16(0) // version
	t.u16(1) // numTables
	t.u16(3) // platformID
	t.u16(10)
	t.u32(12) // offset to subtable
	t.b = append(t.b, sub.b...)
	return t.b
}

// buildName builds a name table carrying nameID 6 (PostScript name) as both a
// Windows (UTF-16BE) and a Macintosh (ASCII) record.
func buildName(ps string) []byte {
	var storage []byte
	macOff := len(storage)
	storage = append(storage, []byte(ps)...) // Macintosh ASCII
	winOff := len(storage)
	for _, c := range ps { // Windows UTF-16BE
		storage = append(storage, 0, byte(c))
	}

	t := &bw{}
	t.u16(0) // format
	t.u16(2) // count
	t.u16(uint16(6 + 2*12))
	// record 0: Windows (platform 3, enc 1, lang 0x409)
	t.u16(3)
	t.u16(1)
	t.u16(0x409)
	t.u16(6)
	t.u16(uint16(len(ps) * 2))
	t.u16(uint16(winOff))
	// record 1: Macintosh (platform 1, enc 0, lang 0)
	t.u16(1)
	t.u16(0)
	t.u16(0)
	t.u16(6)
	t.u16(uint16(len(ps)))
	t.u16(uint16(macOff))
	t.b = append(t.b, storage...)
	return t.b
}

// buildPost builds a version-3.0 post table with the given italic angle and
// fixed-pitch flag.
func buildPost(italic float64, fixed bool) []byte {
	t := &bw{}
	t.u32(0x00030000) // version
	t.u32(uint32(int32(italic * 65536)))
	t.i16(-100) // underlinePosition
	t.i16(50)   // underlineThickness
	if fixed {
		t.u32(1)
	} else {
		t.u32(0)
	}
	t.u32(0) // minMemType42
	t.u32(0) // maxMemType42
	t.u32(0) // minMemType1
	t.u32(0) // maxMemType1
	return t.b
}

// buildOS2 builds a version-2 OS/2 table carrying sCapHeight at offset 88.
func buildOS2(capHeight int16) []byte {
	b := make([]byte, 96)
	binary.BigEndian.PutUint16(b[0:], 2) // version
	binary.BigEndian.PutUint16(b[88:], uint16(capHeight))
	return b
}
