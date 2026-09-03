// Copyright (c) the go-pdfkit authors.
// SPDX-License-Identifier: BSD-3-Clause

package pdfkit

import (
	"bytes"
	"strings"
	"testing"
)

// AddNamedDest + AddNamedLink make an in-document jump: a /Link with a GoTo action
// referencing a name in the /Names /Dests tree, each name mapping to [page /FitH y].
// A repeated name keeps its first definition (a name tree requires unique keys).
func TestNamedDestinationAndInternalLink(t *testing.T) {
	doc := New(Options{})
	p1 := doc.AddPage(A4)
	p1.AddNamedLink(Rect{X: 10, Y: 700, Width: 40, Height: 12}, "sec2")

	p2 := doc.AddPage(A4)
	p2.AddNamedDest("sec2", 0, 780)
	p2.AddNamedDest("sec2", 0, 100) // duplicate name: first definition (y=780) wins
	p2.AddNamedDest("intro", 0, 800)

	var b bytes.Buffer
	if err := doc.Write(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"/Names", "/Dests", "/GoTo", "/D", "/FitH", "/Link", "(intro)"} {
		if !strings.Contains(out, want) {
			t.Errorf("PDF output missing %q", want)
		}
	}
	// (sec2) appears exactly twice — once as the link's GoTo /D and once as the (single,
	// deduplicated) name-tree key — not three times, which would mean the duplicate leaked.
	if n := strings.Count(out, "(sec2)"); n != 2 {
		t.Errorf("(sec2) appears %d times, want 2 (the duplicate destination must be deduped)", n)
	}
}

// A document with no named destinations carries no /Names entry in its catalog.
func TestNoDestsNoNames(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	p.Rectangle(Rect{X: 1, Y: 2, Width: 3, Height: 4})
	p.Fill()
	var b bytes.Buffer
	if err := doc.Write(&b); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "/Names") {
		t.Error("a document with no named destinations should not emit a catalog /Names")
	}
}
