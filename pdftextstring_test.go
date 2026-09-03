// Copyright (c) the go-pdfkit authors.
// SPDX-License-Identifier: BSD-3-Clause

package pdfkit

import (
	"bytes"
	"strings"
	"testing"
)

// A PDF text string (document title, author, bookmark label) is a literal (…) string
// when pure ASCII and a UTF-16BE <FEFF…> hex string when it carries any non-ASCII
// rune, so accented and astral text displays correctly instead of as mojibake.
func TestPDFTextString(t *testing.T) {
	enc := func(s string) string {
		var b bytes.Buffer
		pdfTextString(s).encodePDF(&b)
		return b.String()
	}
	if got := enc("Hi (x)"); got != `(Hi \(x\))` {
		t.Errorf("ASCII = %q, want a literal escaped string", got)
	}
	// café: c=0063 a=0061 f=0066 é=00E9, prefixed by the BOM FEFF.
	if got := enc("café"); got != "<FEFF00630061006600E9>" {
		t.Errorf("accented = %q, want UTF-16BE hex", got)
	}
	// 😀 = U+1F600 encodes as the surrogate pair D83D DE00.
	if got := enc("😀"); got != "<FEFFD83DDE00>" {
		t.Errorf("astral = %q, want a UTF-16 surrogate pair", got)
	}
}

// A document's accented title, author and bookmark are UTF-16BE-encoded in the PDF,
// so a viewer shows them correctly rather than the raw UTF-8 bytes.
func TestAccentedMetadataAndOutline(t *testing.T) {
	doc := New(Options{Title: "Résumé", Author: "Frédéric"})
	doc.AddPage(A4)
	doc.AddOutlineItem("Méthode", 1, 0)
	var b bytes.Buffer
	if err := doc.Write(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "<FEFF") {
		t.Error("accented metadata was not written as a UTF-16BE string")
	}
	if strings.Contains(out, "Résumé") || strings.Contains(out, "Méthode") {
		t.Error("accented text leaked as raw UTF-8 bytes instead of UTF-16BE")
	}
}
