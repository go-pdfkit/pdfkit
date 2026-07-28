// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/go-opentype/opentype"
)

// toUniEntry pairs a glyph id with the UTF-16BE hex of the text it represents.
type toUniEntry struct {
	gid opentype.GlyphIndex
	hex string
}

// buildToUnicode renders a /ToUnicode CMap mapping each used glyph's Identity-H
// code (its glyph id) to the Unicode text it stands for, so extracted or
// copied text is legible. Glyphs with no recorded text are omitted.
func buildToUnicode(use *fontUse) []byte {
	var entries []toUniEntry
	for _, gid := range use.sortedGIDs() {
		runes, ok := use.toUni[gid]
		if !ok || len(runes) == 0 {
			continue
		}
		entries = append(entries, toUniEntry{gid: gid, hex: utf16beHex(runes)})
	}

	var b strings.Builder
	b.WriteString("/CIDInit /ProcSet findresource begin\n")
	b.WriteString("12 dict begin\nbegincmap\n")
	b.WriteString("/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n")
	b.WriteString("/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n")
	b.WriteString("1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")

	// bfchar sections are capped at 100 entries each by the specification.
	for i := 0; i < len(entries); i += 100 {
		chunk := entries[i:min(i+100, len(entries))]
		b.WriteString(strconv.Itoa(len(chunk)))
		b.WriteString(" beginbfchar\n")
		for _, e := range chunk {
			b.WriteByte('<')
			writeStringsHex16(&b, uint16(e.gid))
			b.WriteString("> <")
			b.WriteString(e.hex)
			b.WriteString(">\n")
		}
		b.WriteString("endbfchar\n")
	}

	b.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	return []byte(b.String())
}

// utf16beHex encodes runes as upper-case UTF-16BE hex (surrogate pairs for
// astral characters).
func utf16beHex(runes []rune) string {
	units := utf16.Encode(runes)
	var b strings.Builder
	for _, u := range units {
		writeStringsHex16(&b, u)
	}
	return b.String()
}

// writeStringsHex16 appends v as four upper-case hex digits.
func writeStringsHex16(b *strings.Builder, v uint16) {
	const hex = "0123456789ABCDEF"
	b.WriteByte(hex[(v>>12)&0xf])
	b.WriteByte(hex[(v>>8)&0xf])
	b.WriteByte(hex[(v>>4)&0xf])
	b.WriteByte(hex[v&0xf])
}
