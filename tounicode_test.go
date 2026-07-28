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
	// 150 mapped glyphs force two bfchar blocks (100 + 50).
	for i := 1; i <= 150; i++ {
		u.mark(opentype.GlyphIndex(i), []rune{rune('A' + i)})
	}
	out := string(buildToUnicode(u))
	if strings.Count(out, "beginbfchar") != 2 {
		t.Errorf("expected 2 bfchar blocks, got %d", strings.Count(out, "beginbfchar"))
	}
	if !strings.Contains(out, "100 beginbfchar") || !strings.Contains(out, "50 beginbfchar") {
		t.Errorf("unexpected block sizes:\n%s", out[:200])
	}
}

func TestBuildToUnicodeSkipsUnmapped(t *testing.T) {
	u := newFontUse() // only .notdef, which has no text
	out := string(buildToUnicode(u))
	if strings.Contains(out, "beginbfchar") {
		t.Error("no entries expected for an all-unmapped font")
	}
	if !strings.Contains(out, "endcmap") {
		t.Error("CMap trailer missing")
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
