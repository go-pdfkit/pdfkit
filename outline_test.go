// Copyright (c) the go-pdfkit authors.
// SPDX-License-Identifier: BSD-3-Clause

package pdfkit

import (
	"bytes"
	"strings"
	"testing"
)

// AddOutlineItem builds the /Outlines bookmark tree a viewer shows as its navigation
// sidebar. Items nest by level, each jumps to a page (/Dest [page /Fit]), and the
// tree carries the /Parent //Prev //Next //First //Last //Count links PDF requires.
func TestOutlineTree(t *testing.T) {
	doc := New(Options{})
	for i := 0; i < 3; i++ {
		doc.AddPage(A4)
	}
	doc.AddOutlineItem("Introduction", 1, 0)
	doc.AddOutlineItem("Background", 2, 0) // child of Introduction
	doc.AddOutlineItem("Prior work", 2, 1) // sibling of Background
	doc.AddOutlineItem("Method", 1, 2)     // pops back to top level (stack unwind)
	doc.AddOutlineItem("", 0, 0)           // invalid level: ignored
	doc.AddOutlineItem("Off end", 1, 9)    // page out of range: ignored

	var b bytes.Buffer
	if err := doc.Write(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"/Outlines", "/Title", "/Parent", "/First", "/Last", "/Count",
		"/Prev", "/Next", "/Dest", "/Fit",
		"(Introduction)", "(Background)", "(Prior work)", "(Method)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("PDF output missing %q", want)
		}
	}
	// The ignored items never made it into the outline.
	if strings.Contains(out, "(Off end)") {
		t.Error("an out-of-range outline page must be ignored")
	}
}

// A document with no outline items carries no /Outlines entry in its catalog.
func TestNoOutlineNoEntry(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	p.Rectangle(Rect{X: 1, Y: 2, Width: 3, Height: 4})
	p.Fill()
	var b bytes.Buffer
	if err := doc.Write(&b); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "/Outlines") {
		t.Error("a document with no bookmarks should not emit a catalog /Outlines")
	}
}
