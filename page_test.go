// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"strings"
	"testing"
)

// content returns the raw content-stream bytes of a page for assertions.
func content(p *Page) string { return string(p.finishContent()) }

func TestGraphicsOperators(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	if p.Width() != A4.Width || p.Height() != A4.Height {
		t.Error("page dimensions wrong")
	}
	p.Save()
	p.Translate(10, 20)
	p.Scale(2, 3)
	p.Rotate(90)
	p.Skew(10, 5)
	p.Transform(1, 0, 0, 1, 5, 5)
	p.MoveTo(0, 0)
	p.LineTo(10, 0)
	p.CurveTo(1, 2, 3, 4, 5, 6)
	p.Rectangle(Rect{X: 1, Y: 1, Width: 8, Height: 8})
	p.ClosePath()
	p.SetLineWidth(2)
	p.SetLineCap(CapRound)
	p.SetLineJoin(JoinBevel)
	p.SetMiterLimit(4)
	p.SetDash([]float64{3, 2}, 1)
	p.SetDash(nil, 0)
	p.SetStrokeColor(Gray{0})
	p.Stroke()
	p.Restore()

	c := content(p)
	for _, op := range []string{"q\n", "10 20 cm", "2 0 0 3 0 0 cm", " m\n", " l\n", " c\n", " re\n", "h\n", "2 w", "1 J", "2 j", "4 M", "[3 2] 1 d", "[] 0 d", "S\n", "Q\n"} {
		if !strings.Contains(c, op) {
			t.Errorf("content missing %q\n%s", op, c)
		}
	}
}

func TestFillModesAndClip(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	p.Rectangle(Rect{Width: 1, Height: 1})
	p.Fill()
	p.Rectangle(Rect{Width: 1, Height: 1})
	p.FillEvenOdd()
	p.Rectangle(Rect{Width: 1, Height: 1})
	p.FillStroke()
	p.Rectangle(Rect{Width: 1, Height: 1})
	p.FillStrokeEvenOdd()
	p.Rectangle(Rect{Width: 1, Height: 1})
	p.Clip()
	p.EndPath()
	p.Rectangle(Rect{Width: 1, Height: 1})
	p.ClipEvenOdd()
	p.EndPath()
	c := content(p)
	for _, op := range []string{"f\n", "f*\n", "B\n", "B*\n", "W\n", "n\n", "W*\n"} {
		if !strings.Contains(c, op) {
			t.Errorf("content missing %q", op)
		}
	}
}

func TestColors(t *testing.T) {
	cases := map[string]string{
		Gray{0.5}.ops(false):        "0.5 g",
		Gray{0.5}.ops(true):         "0.5 G",
		RGB{1, 0, 0}.ops(false):     "1 0 0 rg",
		RGB{1, 0, 0}.ops(true):      "1 0 0 RG",
		CMYK{0, 0, 0, 1}.ops(false): "0 0 0 1 k",
		CMYK{0, 0, 0, 1}.ops(true):  "0 0 0 1 K",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
	if c := RGB8(255, 128, 0); c.R != 1 || c.B != 0 {
		t.Errorf("RGB8 = %+v", c)
	}
}

func TestAlphaExtGState(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	p.SetAlpha(0.5, 0.7)
	p.SetAlpha(0.5, 0.7) // reused, same GS0
	p.SetAlpha(0.2, 0.2) // new GS1
	if len(p.extGStates) != 2 {
		t.Fatalf("extGStates = %d, want 2", len(p.extGStates))
	}
	c := content(p)
	if !strings.Contains(c, "/GS0 gs") || !strings.Contains(c, "/GS1 gs") {
		t.Errorf("missing gs operators\n%s", c)
	}
	res := p.resources(nil, nil)
	if enc(res) == "" || !strings.Contains(enc(res), "ExtGState") {
		t.Error("resources missing ExtGState")
	}
}

func TestSortDict(t *testing.T) {
	d := newDict()
	d.set("C", pdfInt(1)).set("A", pdfInt(2)).set("B", pdfInt(3))
	got := enc(sortDict(d))
	if got != "<</A 2 /B 3 /C 1>>" {
		t.Errorf("sortDict = %q", got)
	}
}

func TestMultiPage(t *testing.T) {
	doc := New(Options{})
	doc.AddPage(A4.Portrait())
	doc.AddPage(A3.Landscape())
	r := reopen(t, doc)
	if r.NumPage() != 2 {
		t.Fatalf("NumPage = %d, want 2", r.NumPage())
	}
}
