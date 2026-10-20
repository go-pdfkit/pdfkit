// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"testing"
)

// A text annotation reads back through the independent reader with its
// contents, author, subject, icon, colour and opacity, after the page's links,
// whether the file is classic or packed into object streams.
func TestTextAnnotation(t *testing.T) {
	for _, packed := range []bool{false, true} {
		doc := New(Options{ObjectStreams: packed, Compress: packed})
		p := doc.AddPage(A4)
		p.AddLink(Rect{X: 1, Y: 2, Width: 3, Height: 4}, "https://example.org/")
		p.AddTextAnnotation(TextAnnotation{
			Rect:     Rect{X: 0, Y: 20, Width: 20, Height: 0},
			Contents: "first\n\nsecond (with) \\ parens — é",
			Title:    "Ada",
			Subject:  "Note",
			Color:    []float64{1, 0.92, 0.42},
			Opacity:  0.25,
		})
		p.AddTextAnnotation(TextAnnotation{Contents: ""}) // ignored
		q := doc.AddPage(A4)
		q.AddTextAnnotation(TextAnnotation{Contents: "plain", Name: "Comment"})

		b := writeDoc(t, doc)
		r := reopenBytes(t, b)
		annots := r.Page(1).V.Key("Annots")
		if annots.Len() != 2 {
			t.Fatalf("packed=%v: page 1 has %d annotations, want a link and a note", packed, annots.Len())
		}
		a := annots.Index(1)
		if got := a.Key("Subtype").Name(); got != "Text" {
			t.Errorf("subtype = %q", got)
		}
		if got := a.Key("Contents").Text(); got != "first\n\nsecond (with) \\ parens — é" {
			t.Errorf("contents = %q", got)
		}
		if got := a.Key("T").Text(); got != "Ada" {
			t.Errorf("T = %q", got)
		}
		if got := a.Key("Subj").Text(); got != "Note" {
			t.Errorf("Subj = %q", got)
		}
		if got := a.Key("Name").Name(); got != "Note" {
			t.Errorf("Name = %q, want the default icon", got)
		}
		if a.Key("C").Len() != 3 || a.Key("C").Index(1).Float64() != 0.92 {
			t.Errorf("C = %v", a.Key("C"))
		}
		if got := a.Key("CA").Float64(); got != 0.25 {
			t.Errorf("CA = %v", got)
		}
		if got := r.Page(2).V.Key("Annots").Index(0).Key("Name").Name(); got != "Comment" {
			t.Errorf("page 2 icon = %q", got)
		}
		if bytes.Contains(b, []byte("/CA 0")) && !bytes.Contains(b, []byte("/CA 0.25")) {
			t.Error("opacity written wrongly")
		}
	}
}

// A page with no annotation has no /Annots; one with only notes has one.
func TestTextAnnotationOnly(t *testing.T) {
	doc := New(Options{})
	doc.AddPage(A4)
	p := doc.AddPage(A4)
	p.AddTextAnnotation(TextAnnotation{Contents: "x"})
	r := reopenBytes(t, writeDoc(t, doc))
	if r.Page(1).V.Key("Annots").Len() != 0 {
		t.Error("page 1 should have no annotations")
	}
	if r.Page(2).V.Key("Annots").Len() != 1 {
		t.Error("page 2 should have its note")
	}
}

// AddOutlineItemWith writes a bold, italic title and a position: the
// destination is [page /XYZ x y null], which keeps the viewer's zoom.
func TestOutlineOptions(t *testing.T) {
	doc := New(Options{})
	doc.AddPage(A4)
	at := &Point{X: 72, Y: 700}
	doc.AddOutlineItemWith("Page 1", 1, 0, OutlineOptions{Bold: true})
	doc.AddOutlineItemWith("Heading", 2, 0, OutlineOptions{Italic: true, At: at})
	at.Y = 1 // the item took a copy
	doc.AddOutlineItem("Plain", 1, 0)

	b := writeDoc(t, doc)
	for _, want := range []string{"/F 2", "/F 1", "/XYZ 72 700 null", "/Fit"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("output lacks %q", want)
		}
	}
	r := reopenBytes(t, b)
	first := r.Trailer().Key("Root").Key("Outlines").Key("First")
	if got := first.Key("Title").Text(); got != "Page 1" {
		t.Fatalf("first title = %q", got)
	}
	if got := first.Key("F").Int64(); got != 2 {
		t.Errorf("F = %d, want bold", got)
	}
	child := first.Key("First")
	if got := child.Key("Title").Text(); got != "Heading" {
		t.Errorf("child title = %q", got)
	}
	if d := child.Key("Dest"); d.Index(1).Name() != "XYZ" || d.Index(2).Float64() != 72 || d.Index(3).Float64() != 700 {
		t.Errorf("Dest = %v", d)
	}
}
