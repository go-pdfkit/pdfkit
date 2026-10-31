// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"
)

// One form painted thirty times on two pages is one stream in the file, and
// each page lists it among its XObjects.
func TestFormIsWrittenOnce(t *testing.T) {
	doc := New(Options{})
	f := doc.NewForm(Rect{Width: 10, Height: 10})
	f.SetFillColor(RGB8(255, 0, 0))
	f.Rectangle(Rect{Width: 10, Height: 10})
	f.Fill()
	f.DrawImage(makeTestImage(), Rect{Width: 5, Height: 5})

	p1, p2 := doc.AddPage(A4), doc.AddPage(A4)
	for i := 0; i < 30; i++ {
		p := p1
		if i%2 == 1 {
			p = p2
		}
		if err := p.DrawForm(f, Rect{X: float64(i) * 12, Y: 100, Width: 10, Height: 10}); err != nil {
			t.Fatal(err)
		}
	}
	out := writeDoc(t, doc)
	if n := bytes.Count(out, []byte("/Subtype /Form")); n != 1 {
		t.Errorf("%d form streams, want 1", n)
	}
	if n := strings.Count(content(p1), "/Fm0 Do") + strings.Count(content(p2), "/Fm0 Do"); n != 30 {
		t.Errorf("%d paints, want 30", n)
	}

	r := reopen(t, doc)
	for pg := 1; pg <= 2; pg++ {
		fm := r.Page(pg).V.Key("Resources").Key("XObject").Key("Fm0")
		if fm.Key("Subtype").Name() != "Form" {
			t.Fatalf("page %d: Fm0 is %v", pg, fm)
		}
		if bb := fm.Key("BBox"); bb.Len() != 4 || bb.Index(2).Float64() != 10 || bb.Index(3).Float64() != 10 {
			t.Errorf("BBox = %v", bb)
		}
		// the image the form paints is the form's resource, not the page's
		if fm.Key("Resources").Key("XObject").Key("Im0").Key("Subtype").Name() != "Image" {
			t.Errorf("the form's resources lack its image: %v", fm.Key("Resources"))
		}
		body := string(readStream(t, fm))
		if !strings.Contains(body, "re") || !strings.Contains(body, "/Im0 Do") {
			t.Errorf("form body = %q", body)
		}
	}
	if r.Page(1).V.Key("Resources").Key("XObject").Key("Im0").Kind() != 0 {
		t.Error("the page lists an image only its form paints")
	}
}

// A form may paint a form made before or after it; the references resolve.
func TestFormNesting(t *testing.T) {
	doc := New(Options{Compress: true})
	outer := doc.NewForm(Rect{Width: 20, Height: 20})
	inner := doc.NewForm(Rect{Width: 1, Height: 1})
	inner.Rectangle(Rect{Width: 1, Height: 1})
	inner.Fill()
	if err := outer.DrawForm(inner, Rect{Width: 20, Height: 20}); err != nil {
		t.Fatal(err)
	}
	p := doc.AddPage(A4)
	if err := p.DrawForm(outer, Rect{Width: 40, Height: 40}); err != nil {
		t.Fatal(err)
	}
	r := reopen(t, doc)
	o := r.Page(1).V.Key("Resources").Key("XObject").Key("Fm0")
	in := o.Key("Resources").Key("XObject").Key("Fm1")
	if in.Key("Subtype").Name() != "Form" {
		t.Fatalf("the inner form does not resolve: %v", in)
	}
	if body := string(readStream(t, in)); !strings.Contains(body, "re") {
		t.Errorf("inner body = %q", body)
	}
	if body := string(readStream(t, o)); !strings.Contains(body, "20 0 0 20 0 0 cm") || !strings.Contains(body, "/Fm1 Do") {
		t.Errorf("outer body = %q", body)
	}
}

// A form that would paint itself, directly or through others, is refused: a
// reader would recurse without end.
func TestFormCycle(t *testing.T) {
	doc := New(Options{})
	a, b, c := doc.NewForm(Rect{Width: 1, Height: 1}), doc.NewForm(Rect{Width: 1, Height: 1}), doc.NewForm(Rect{Width: 1, Height: 1})
	if err := a.DrawForm(a, Rect{Width: 1, Height: 1}); !errors.Is(err, ErrFormCycle) {
		t.Errorf("a in a: %v", err)
	}
	if err := a.DrawForm(b, Rect{Width: 1, Height: 1}); err != nil {
		t.Fatal(err)
	}
	if err := b.DrawForm(c, Rect{Width: 1, Height: 1}); err != nil {
		t.Fatal(err)
	}
	if err := c.DrawForm(a, Rect{Width: 1, Height: 1}); !errors.Is(err, ErrFormCycle) {
		t.Errorf("a in c, c in b, b in a: %v", err)
	}
	// diamond: b and c both in d is no cycle
	d := doc.NewForm(Rect{Width: 1, Height: 1})
	if err := d.DrawForm(b, Rect{Width: 1, Height: 1}); err != nil {
		t.Error(err)
	}
	if err := d.DrawForm(c, Rect{Width: 1, Height: 1}); err != nil {
		t.Error(err)
	}
	// and painting the diamond walks c twice, without a false cycle
	if err := doc.NewForm(Rect{Width: 1, Height: 1}).DrawForm(d, Rect{Width: 1, Height: 1}); err != nil {
		t.Error(err)
	}
	if strings.Contains(content(c.Page), "Do") {
		t.Error("the refused paint was written")
	}
	if err := doc.AddPage(A4).DrawForm(d, Rect{Width: 1, Height: 1}); err != nil {
		t.Fatal(err)
	}
	reopen(t, doc)
}

func TestFormOtherDocument(t *testing.T) {
	f := New(Options{}).NewForm(Rect{Width: 1, Height: 1})
	p := New(Options{}).AddPage(A4)
	if err := p.DrawForm(f, Rect{Width: 1, Height: 1}); !errors.Is(err, ErrFormDocument) {
		t.Errorf("err = %v", err)
	}
}

// DrawForm maps the bounding box onto the rectangle, an origin off zero
// included; PaintForm takes the matrix as given and omits the identity.
func TestDrawFormMatrix(t *testing.T) {
	doc := New(Options{})
	f := doc.NewForm(Rect{X: 10, Y: 20, Width: 100, Height: 50})
	if got := f.BBox(); got != (Rect{X: 10, Y: 20, Width: 100, Height: 50}) {
		t.Errorf("BBox = %v", got)
	}
	p := doc.AddPage(A4)
	if p.Document() != doc || f.Document() != doc {
		t.Error("Document does not return the page's document")
	}
	if err := p.DrawForm(f, Rect{Width: 200, Height: 100}); err != nil {
		t.Fatal(err)
	}
	if err := p.PaintForm(f, 1, 0, 0, 1, 0, 0); err != nil {
		t.Fatal(err)
	}
	want := "q\n2 0 0 2 -20 -40 cm\n/Fm0 Do\nQ\nq\n/Fm0 Do\nQ\n"
	if got := content(p); got != want {
		t.Errorf("content =\n%s\nwant\n%s", got, want)
	}
}

// Nothing to paint, or a matrix that is not a number, writes nothing.
func TestDrawFormDegenerate(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	empty := doc.NewForm(Rect{Width: 0, Height: 5})
	f := doc.NewForm(Rect{Width: 5, Height: 5})
	for _, err := range []error{
		p.DrawForm(nil, Rect{Width: 1, Height: 1}),
		p.PaintForm(nil, 1, 0, 0, 1, 0, 0),
		p.DrawForm(empty, Rect{Width: 1, Height: 1}),
		p.PaintForm(f, math.NaN(), 0, 0, 1, 0, 0),
		p.PaintForm(f, 1, 0, 0, 1, math.Inf(1), 0),
	} {
		if err != nil {
			t.Error(err)
		}
	}
	if c := content(p); c != "" {
		t.Errorf("content = %q", c)
	}
	if len(p.usedForms) != 0 {
		t.Error("a form nothing painted is listed as a resource")
	}
}

// A form drawn inside a tagged sequence takes that sequence's structure: the
// form itself carries no MCID, and its own tags and tagged links are not
// written (a form has no place in the structure tree).
func TestFormInTaggedDocument(t *testing.T) {
	doc := New(Options{})
	root := doc.Structure()
	fig := root.Add("Figure")
	fig.Alt = "a red square"
	f := doc.NewForm(Rect{Width: 10, Height: 10})
	f.BeginTag(fig)
	f.AddTaggedLink(root.Add("Link"), Rect{Width: 1, Height: 1}, "https://example.org", "x")
	f.Rectangle(Rect{Width: 10, Height: 10})
	f.Fill()
	f.EndMarkedContent()

	p := doc.AddPage(A4)
	p.BeginTag(fig)
	if err := p.DrawForm(f, Rect{Width: 10, Height: 10}); err != nil {
		t.Fatal(err)
	}
	p.EndMarkedContent()
	if strings.Contains(content(f.Page), "MCID") || strings.Contains(content(f.Page), "BDC") {
		t.Errorf("form content = %q", content(f.Page))
	}
	if len(f.links) != 0 {
		t.Error("a tagged link was kept on a form")
	}
	if c := content(p); !strings.Contains(c, "/Figure <</MCID 0>> BDC\nq\n/Fm0 Do\nQ\nEMC") {
		t.Errorf("page content = %q", c)
	}
	out := writeDoc(t, doc)
	if bytes.Contains(out, []byte("/StructParent ")) {
		t.Error("a form was given a struct parent")
	}
	reopen(t, doc)
}
