// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"crypto/sha1"
	"encoding/binary"

	"github.com/go-opentype/opentype"
)

// buildFont emits the composite (Type0) font and its descendant CIDFont,
// descriptor, embedded font program and /ToUnicode CMap for f, returning the
// Type0 dictionary's object reference. The font is always embedded as a subset
// with Identity-H encoding.
//
// The two outline flavours are subsetted by go-opentype and differ only in how a
// content-stream glyph id (which is always the original glyph id) reaches the
// embedded program: a TrueType subset renumbers its glyphs compactly, so a
// /CIDToGIDMap stream maps original id -> subset id; a CFF subset preserves glyph
// numbering, so an Identity /CIDToGIDMap suffices.
func (d *Document) buildFont(bd *builder, f *Font) objRef {
	use := d.use[f]
	gids := use.sortedGIDs()
	tag := subsetTag(f, gids)
	baseName := tag + "+" + f.baseName

	fontFileRef, ffKey, remap := d.embedProgram(bd, f, gids)
	descRef := d.buildDescriptor(bd, f, baseName, fontFileRef, ffKey)
	cidRef := d.buildCIDFont(bd, f, baseName, descRef, gids, remap)
	toUniRef := bd.add(d.maybeFlateStream(buildToUnicode(use)))

	dict := newDict()
	dict.set("Type", pdfName("Font"))
	dict.set("Subtype", pdfName("Type0"))
	dict.set("BaseFont", pdfName(baseName))
	dict.set("Encoding", pdfName("Identity-H"))
	dict.set("DescendantFonts", pdfArray{cidRef})
	dict.set("ToUnicode", toUniRef)
	return bd.add(dict)
}

// embedProgram writes the embedded font program stream and returns its
// reference, the descriptor key (/FontFile2 for TrueType, /FontFile3 for CFF)
// that must point at it, and — for a renumbering TrueType subset — the original
// glyph id -> subset glyph id remap the /CIDToGIDMap is built from. The remap is
// nil for CFF (its glyph numbering is preserved) and for a TrueType subset that
// somehow failed (which never happens for a valid 'glyf' font).
func (d *Document) embedProgram(bd *builder, f *Font, gids []opentype.GlyphIndex) (objRef, string, map[opentype.GlyphIndex]opentype.GlyphIndex) {
	if f.isCFF {
		return d.embedCFF(bd, f, gids), "FontFile3", nil
	}
	// TrueType: embed a compact subset and carry its glyph renumbering so the
	// descendant font can map CID (= original glyph id) to the subset glyph id.
	sub, remap, err := f.ot.SubsetTrueType(gids)
	if err != nil {
		// SubsetTrueType errors only for a non-'glyf' font or an out-of-range
		// glyph id; a Font that reports !isCFF always has glyf, so this reduces
		// to an out-of-range glyph. Fall back to the whole font program with an
		// Identity map (original glyph numbering) — correct, just not minimal,
		// and symmetric with the CFF whole-table fallback.
		sub, remap = f.data, nil
	}
	dict := newDict()
	dict.set("Length1", pdfInt(len(sub)))
	s := d.maybeFlate(dict, sub)
	return bd.add(&pdfStream{dict: dict, data: s}), "FontFile2", remap
}

// embedCFF writes the CFF program stream as a CIDFontType0C /FontFile3 and
// returns its reference. It subsets the 'CFF ' charstrings via go-opentype,
// keeping only the used glyphs while preserving glyph numbering (so an Identity
// /CIDToGIDMap stays valid). A CID-keyed CFF or a CFF2 (variable) font cannot be
// charstring-subsetted by this preserve-numbering path, so it falls back to
// embedding the whole table unchanged — correct, just not minimal.
func (d *Document) embedCFF(bd *builder, f *Font, gids []opentype.GlyphIndex) objRef {
	prog, err := f.ot.SubsetCFF(gids)
	if err != nil {
		prog = wholeCFF(f)
	}
	dict := newDict()
	dict.set("Subtype", pdfName("CIDFontType0C"))
	s := d.maybeFlate(dict, prog)
	return bd.add(&pdfStream{dict: dict, data: s})
}

// wholeCFF returns the font's whole, unsubsetted CFF program: the 'CFF ' table
// when present, otherwise the 'CFF2' table. It is the graceful fallback for a
// CID-keyed CFF or a CFF2 font, which the preserve-numbering subsetter rejects.
func wholeCFF(f *Font) []byte {
	if b, ok := f.ot.Table("CFF "); ok {
		return b
	}
	b, _ := f.ot.Table("CFF2")
	return b
}

// buildDescriptor writes the FontDescriptor, converting design metrics from
// font units to PDF glyph space (1000 units per em). Every scalar comes straight
// from go-opentype's descriptor accessors; a zero cap height (absent OS/2) falls
// back to the ascender, as a PDF consumer must.
func (d *Document) buildDescriptor(bd *builder, f *Font, baseName string, fontFile objRef, ffKey string) objRef {
	upm := f.ot.UnitsPerEm()
	sc := 1000.0 / float64(upm)
	scale := func(v int) pdfValue { return pdfInt(int(float64(v)*sc + 0.5)) }

	xMin, yMin, xMax, yMax := f.ot.FontBBox()
	capHeight := f.ot.CapHeight()
	if capHeight == 0 {
		capHeight = f.ot.Ascent()
	}

	desc := newDict()
	desc.set("Type", pdfName("FontDescriptor"))
	desc.set("FontName", pdfName(baseName))
	desc.set("Flags", pdfInt(f.ot.Flags()))
	desc.set("FontBBox", pdfArray{scale(xMin), scale(yMin), scale(xMax), scale(yMax)})
	desc.set("ItalicAngle", pdfReal(f.ot.ItalicAngle()))
	desc.set("Ascent", scale(f.ot.Ascent()))
	desc.set("Descent", scale(f.ot.Descent()))
	desc.set("CapHeight", scale(capHeight))
	desc.set("StemV", scale(f.ot.StemV()))
	desc.set(ffKey, fontFile)
	return bd.add(desc)
}

// buildCIDFont writes the descendant CIDFont, including the per-glyph width
// array, the default width and the /CIDToGIDMap. When remap is nil the map is the
// Identity name (CFF, or a preserved-numbering program); otherwise it is a stream
// mapping each CID (original glyph id) to its subset glyph id.
func (d *Document) buildCIDFont(bd *builder, f *Font, baseName string, desc objRef, gids []opentype.GlyphIndex, remap map[opentype.GlyphIndex]opentype.GlyphIndex) objRef {
	subtype := "CIDFontType2"
	if f.isCFF {
		subtype = "CIDFontType0"
	}
	sysInfo := newDict()
	sysInfo.set("Registry", pdfString("Adobe"))
	sysInfo.set("Ordering", pdfString("Identity"))
	sysInfo.set("Supplement", pdfInt(0))

	cid := newDict()
	cid.set("Type", pdfName("Font"))
	cid.set("Subtype", pdfName(subtype))
	cid.set("BaseFont", pdfName(baseName))
	cid.set("CIDSystemInfo", sysInfo)
	cid.set("FontDescriptor", desc)
	if remap != nil {
		cid.set("CIDToGIDMap", d.cidToGIDMap(bd, remap))
	} else {
		cid.set("CIDToGIDMap", pdfName("Identity"))
	}
	cid.set("DW", pdfInt(1000))
	cid.set("W", d.widthArray(f, gids))
	return bd.add(cid)
}

// cidToGIDMap builds the /CIDToGIDMap stream for a renumbering TrueType subset. A
// content stream addresses glyphs by CID = original glyph id (Identity-H), so the
// map is indexed by original id and yields the subset id, two big-endian bytes
// per CID. It is sized to cover every glyph the subset kept (the remap's largest
// original id), which includes every CID a content stream can reference; any gap
// left in the array maps to glyph 0 (.notdef). It returns the stream reference.
func (d *Document) cidToGIDMap(bd *builder, remap map[opentype.GlyphIndex]opentype.GlyphIndex) objRef {
	maxOld := 0
	for old := range remap {
		if int(old) > maxOld {
			maxOld = int(old)
		}
	}
	raw := make([]byte, (maxOld+1)*2)
	for old, nw := range remap {
		binary.BigEndian.PutUint16(raw[int(old)*2:], uint16(nw))
	}
	dict := newDict()
	data := d.maybeFlate(dict, raw)
	return bd.add(&pdfStream{dict: dict, data: data})
}

// widthArray builds the /W array, one "cid [width]" pair per used glyph. The CID
// is the original glyph id (the /CIDToGIDMap, or a preserved numbering, resolves
// it to the embedded program's glyph), and the width is taken by that same
// original id from the go-opentype face.
func (d *Document) widthArray(f *Font, gids []opentype.GlyphIndex) pdfArray {
	var w pdfArray
	for _, g := range gids {
		w = append(w, pdfInt(int(g)), pdfArray{pdfInt(f.glyphWidth1000(g))})
	}
	return w
}

// maybeFlateStream wraps a ToUnicode stream, compressing it when requested.
func (d *Document) maybeFlateStream(data []byte) *pdfStream {
	dict := newDict()
	out := d.maybeFlate(dict, data)
	return &pdfStream{dict: dict, data: out}
}

// subsetTag derives the required six-uppercase-letter subset prefix
// deterministically from the font and the exact set of embedded glyphs.
func subsetTag(f *Font, gids []opentype.GlyphIndex) string {
	h := sha1.New()
	h.Write([]byte(f.baseName))
	var buf [2]byte
	for _, g := range gids {
		buf[0] = byte(g >> 8)
		buf[1] = byte(g)
		h.Write(buf[:])
	}
	sum := h.Sum(nil)
	var tag [6]byte
	for i := 0; i < 6; i++ {
		tag[i] = 'A' + sum[i]%26
	}
	return string(tag[:])
}
