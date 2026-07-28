// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"encoding/binary"
	"fmt"

	"github.com/go-opentype/opentype"
)

// Font is a loaded TrueType or OpenType font ready to be used and embedded. It
// is immutable and may be shared across documents and goroutines; per-document
// glyph usage is tracked separately by the Document. Build one with LoadFont.
type Font struct {
	ot       *opentype.Font
	face     *opentype.Face
	data     []byte // the whole sfnt, retained for the whole-font embed fallback
	baseName string
	isCFF    bool
}

// LoadFont parses a TrueType ('glyf') or OpenType/CFF ('OTTO'/CFF) font from
// its raw bytes. The bytes are retained and must not be mutated afterwards.
// Parsing, glyph indexing, metrics, shaping and subsetting are all delegated to
// github.com/go-opentype/opentype; pdfkit keeps no private sfnt re-parse.
func LoadFont(data []byte) (*Font, error) {
	ot, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("pdfkit: parse font: %w", err)
	}
	// The face is sized to the em, so AdvanceIndexUnits returns advances in font
	// units — exactly what a PDF /W width array wants — at the base (uninstanced)
	// design.
	face := ot.NewFace(ot.UnitsPerEm())

	name := ""
	if nameTable, ok := ot.Table("name"); ok {
		name = readPSName(nameTable)
	}
	if name == "" {
		name = "PDFKitFont"
	}
	return &Font{ot: ot, face: face, data: data, baseName: name, isCFF: fontIsCFF(ot)}, nil
}

// fontIsCFF reports whether the font carries CFF or CFF2 (PostScript) outlines,
// as opposed to TrueType 'glyf' outlines. It is read from the table directory
// the font was parsed from.
func fontIsCFF(ot *opentype.Font) bool {
	if _, ok := ot.Table("CFF "); ok {
		return true
	}
	_, ok := ot.Table("CFF2")
	return ok
}

// IsCFF reports whether the font carries CFF/OpenType outlines (embedded as a
// CIDFontType0), as opposed to TrueType 'glyf' outlines (CIDFontType2).
func (f *Font) IsCFF() bool { return f.isCFF }

// UnitsPerEm returns the font's design grid size.
func (f *Font) UnitsPerEm() int { return f.ot.UnitsPerEm() }

// NumGlyphs returns the number of glyphs in the font.
func (f *Font) NumGlyphs() int { return f.ot.NumGlyphs() }

// BaseName returns the font's PostScript name, used for the PDF /BaseFont.
func (f *Font) BaseName() string { return f.baseName }

// glyphWidth1000 returns glyph gid's advance width in PDF glyph space (1000
// units per em). The advance comes from the go-opentype face by glyph index, so
// it tracks the instanced design a PDF /W array must report.
func (f *Font) glyphWidth1000(gid opentype.GlyphIndex) int {
	adv := f.face.AdvanceIndexUnits(gid)
	return int(adv*1000/float64(f.ot.UnitsPerEm()) + 0.5)
}

// fontUse records, for one document, which glyphs of a font are drawn and the
// text each glyph stands for (for the /ToUnicode CMap).
type fontUse struct {
	gids  map[opentype.GlyphIndex]bool
	toUni map[opentype.GlyphIndex][]rune
}

// newFontUse returns an empty usage record already including .notdef (glyph 0).
func newFontUse() *fontUse {
	return &fontUse{
		gids:  map[opentype.GlyphIndex]bool{0: true},
		toUni: map[opentype.GlyphIndex][]rune{},
	}
}

// mark records that glyph gid is used and, when runes is non-empty and the
// glyph has no mapping yet, associates it with that text for copy/paste.
func (u *fontUse) mark(gid opentype.GlyphIndex, runes []rune) {
	u.gids[gid] = true
	if len(runes) > 0 {
		if _, ok := u.toUni[gid]; !ok {
			cp := make([]rune, len(runes))
			copy(cp, runes)
			u.toUni[gid] = cp
		}
	}
}

// sortedGIDs returns the used glyph ids in ascending order.
func (u *fontUse) sortedGIDs() []opentype.GlyphIndex {
	out := make([]opentype.GlyphIndex, 0, len(u.gids))
	for g := range u.gids {
		out = append(out, g)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// u16 reads a big-endian uint16 at b[i:].
func u16(b []byte, i int) int { return int(binary.BigEndian.Uint16(b[i:])) }

// readPSName extracts nameID 6 (the PostScript name) from a raw 'name' table,
// returning "" when it is absent or unreadable. Windows records are UTF-16BE;
// Macintosh records are single-byte; PostScript names are ASCII either way.
func readPSName(b []byte) string {
	if len(b) < 6 {
		return ""
	}
	count := u16(b, 2)
	storage := u16(b, 4)
	best := ""
	for i := 0; i < count; i++ {
		rec := 6 + i*12
		if rec+12 > len(b) {
			break
		}
		platform := u16(b, rec)
		nameID := u16(b, rec+6)
		length := u16(b, rec+8)
		off := storage + u16(b, rec+10)
		if nameID != 6 || off+length > len(b) {
			continue
		}
		s := decodeName(b[off:off+length], platform)
		if s != "" {
			best = s
			if platform == 1 { // Macintosh ASCII is the cleanest source
				break
			}
		}
	}
	return best
}

// decodeName turns a raw name-record value into an ASCII string, handling the
// UTF-16BE encoding Windows (platform 3) and Unicode (platform 0) records use.
func decodeName(raw []byte, platform int) string {
	if platform == 3 || platform == 0 {
		out := make([]byte, 0, len(raw)/2)
		for i := 0; i+1 < len(raw); i += 2 {
			hi, lo := raw[i], raw[i+1]
			if hi == 0 && lo >= 0x20 && lo < 0x7f {
				out = append(out, lo)
			}
		}
		return string(out)
	}
	out := make([]byte, 0, len(raw))
	for _, c := range raw {
		if c >= 0x20 && c < 0x7f {
			out = append(out, c)
		}
	}
	return string(out)
}
