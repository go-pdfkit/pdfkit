// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"strings"
	"testing"

	"github.com/go-opentype/opentype"
)

func TestBuildToUnicodeChunking(t *testing.T) {
	u := newFontUse()
	// 150 mapped glyphs, and .notdef is always mapped, so 151 entries force
	// two bfchar blocks of 100 and 51.
	for i := 1; i <= 150; i++ {
		u.mark(opentype.GlyphIndex(i), []rune{rune('A' + i)})
	}
	out := string(buildToUnicode(u))
	if strings.Count(out, "beginbfchar") != 2 {
		t.Errorf("expected 2 bfchar blocks, got %d", strings.Count(out, "beginbfchar"))
	}
	if !strings.Contains(out, "100 beginbfchar") || !strings.Contains(out, "51 beginbfchar") {
		t.Errorf("unexpected block sizes:\n%s", out[:200])
	}
}

func TestBuildToUnicodeMapsNotdefToTheReplacementCharacter(t *testing.T) {
	// A font used for nothing but characters it does not have still says so.
	// .notdef reads back as U+FFFD, which is what the page shows too: the
	// face's own .notdef draws a box.
	u := newFontUse()
	out := string(buildToUnicode(u))
	if !strings.Contains(out, "1 beginbfchar") {
		t.Errorf("expected one entry for .notdef:\n%s", out)
	}
	if !strings.Contains(out, "<0000> <FFFD>") {
		t.Errorf("glyph 0 is not mapped to the replacement character:\n%s", out)
	}
	if !strings.Contains(out, "endcmap") {
		t.Error("CMap trailer missing")
	}
}

func TestNotdefKeepsItsMappingWhateverAsksForIt(t *testing.T) {
	// The defect this replaced: the first character a font lacked took glyph
	// 0's entry, so every later missing character read back as that first one.
	// Poppler read a file written here as "a 漢漢 b 漢漢 c 漢漢 d" where
	// "a 漢字 b かな c 한글 d" had been written.
	u := newFontUse()
	u.mark(0, []rune{'漢'})
	u.mark(0, []rune{'字'})
	u.mark(0, []rune{'\U0001F600'})
	if got := u.toUni[0]; len(got) != 1 || got[0] != '\uFFFD' {
		t.Errorf("glyph 0 maps to %q, want the replacement character", string(got))
	}
	out := string(buildToUnicode(u))
	for _, unwanted := range []string{"6F22", "5B57", "D83D"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("the CMap claims .notdef is %s:\n%s", unwanted, out)
		}
	}
}

func TestUTF16beHexAstral(t *testing.T) {
	// U+1F600 encodes as a surrogate pair: eight hex digits.
	if got := utf16beHex([]rune{0x1F600}); got != "D83DDE00" {
		t.Errorf("astral hex = %q", got)
	}
	// A BMP rune is four hex digits.
	if got := utf16beHex([]rune{'A'}); got != "0041" {
		t.Errorf("bmp hex = %q", got)
	}
}
