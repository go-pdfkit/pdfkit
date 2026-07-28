// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"crypto/sha1"

	"github.com/go-opentype/opentype"
)

// buildFont emits the composite (Type0) font and its descendant CIDFont,
// descriptor, embedded font program and /ToUnicode CMap for f, returning the
// Type0 dictionary's object reference. The font is always embedded as a
// subset with Identity-H encoding and an Identity CIDToGIDMap, so a CID equals
// a glyph id.
func (d *Document) buildFont(bd *builder, f *Font) objRef {
	use := d.use[f]
	gids := use.sortedGIDs()
	tag := subsetTag(f, gids)
	baseName := tag + "+" + f.baseName

	fontFileRef, ffKey := d.embedProgram(bd, f, gids)
	descRef := d.buildDescriptor(bd, f, baseName, fontFileRef, ffKey)
	cidRef := d.buildCIDFont(bd, f, baseName, descRef, gids)
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
// reference plus the descriptor key (/FontFile2 for TrueType, /FontFile3 for
// CFF) that must point at it.
func (d *Document) embedProgram(bd *builder, f *Font, gids []opentype.GlyphIndex) (objRef, string) {
	if f.sf.isCFF {
		// CFF/OpenType: embed the CFF table as a CIDFontType0C program. pdfkit
		// does not yet subset CFF charstrings, so the whole table is embedded.
		dict := newDict()
		dict.set("Subtype", pdfName("CIDFontType0C"))
		s := d.maybeFlate(dict, f.sf.tables["CFF "])
		return bd.add(&pdfStream{dict: dict, data: s}), "FontFile3"
	}
	sub := subsetTrueType(f.sf, gids)
	dict := newDict()
	dict.set("Length1", pdfInt(len(sub)))
	s := d.maybeFlate(dict, sub)
	return bd.add(&pdfStream{dict: dict, data: s}), "FontFile2"
}

// buildDescriptor writes the FontDescriptor, converting design metrics from
// font units to PDF glyph space (1000 units per em).
func (d *Document) buildDescriptor(bd *builder, f *Font, baseName string, fontFile objRef, ffKey string) objRef {
	sc := 1000.0 / float64(f.sf.unitsPerEm)
	scale := func(v int) pdfValue { return pdfInt(int(float64(v)*sc + 0.5)) }

	desc := newDict()
	desc.set("Type", pdfName("FontDescriptor"))
	desc.set("FontName", pdfName(baseName))
	desc.set("Flags", pdfInt(f.sf.flags))
	desc.set("FontBBox", pdfArray{scale(f.sf.xMin), scale(f.sf.yMin), scale(f.sf.xMax), scale(f.sf.yMax)})
	desc.set("ItalicAngle", pdfReal(f.sf.italicAngle))
	desc.set("Ascent", scale(f.sf.ascender))
	desc.set("Descent", scale(f.sf.descender))
	desc.set("CapHeight", scale(f.sf.capHeight))
	desc.set("StemV", pdfInt(80))
	desc.set(ffKey, fontFile)
	return bd.add(desc)
}

// buildCIDFont writes the descendant CIDFont, including the per-glyph width
// array and the default width.
func (d *Document) buildCIDFont(bd *builder, f *Font, baseName string, desc objRef, gids []opentype.GlyphIndex) objRef {
	subtype := "CIDFontType2"
	if f.sf.isCFF {
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
	cid.set("CIDToGIDMap", pdfName("Identity"))
	cid.set("DW", pdfInt(1000))
	cid.set("W", d.widthArray(f, gids))
	return bd.add(cid)
}

// widthArray builds the /W array, one "cid [width]" pair per used glyph.
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
