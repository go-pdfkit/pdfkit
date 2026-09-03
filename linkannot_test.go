// Copyright (c) the go-pdfkit authors.
// SPDX-License-Identifier: BSD-3-Clause

package pdfkit

import (
	"bytes"
	"strings"
	"testing"
)

// AddLink places a clickable /Link annotation with a URI action over a rectangle,
// so a typeset hyperref link becomes navigable in the PDF. A URL carrying parens
// exercises string escaping.
func TestLinkAnnotation(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	p.Rectangle(Rect{X: 10, Y: 20, Width: 30, Height: 40})
	p.Fill()
	p.AddLink(Rect{X: 100, Y: 200, Width: 80, Height: 12}, "https://example.org/a(b)")

	var b bytes.Buffer
	if err := doc.Write(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"/Annots",
		"/Subtype",
		"/Link",
		"/Rect",
		"/URI",
		`(https://example.org/a\(b\))`, // the parens must be escaped inside the PDF string
	} {
		if !strings.Contains(out, want) {
			t.Errorf("PDF output missing %q", want)
		}
	}
}

// A page with no links emits no /Annots array.
func TestNoLinksNoAnnots(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	p.Rectangle(Rect{X: 1, Y: 2, Width: 3, Height: 4})
	p.Fill()
	var b bytes.Buffer
	if err := doc.Write(&b); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "/Annots") {
		t.Error("a page with no links should not emit an /Annots array")
	}
}
