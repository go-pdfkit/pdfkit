// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"strings"
	"testing"

	"rsc.io/pdf"
)

// shadeDoc paints one gradient on one page and hands back what an independent
// reader makes of the file.
func shadeDoc(t *testing.T, s Shading) *pdf.Reader {
	t.Helper()
	doc := New(Options{})
	p := doc.AddPage(A4)
	p.Save()
	p.Rectangle(Rect{X: 0, Y: 0, Width: 100, Height: 100})
	p.Clip()
	p.EndPath()
	if err := p.Shade(s); err != nil {
		t.Fatalf("Shade: %v", err)
	}
	p.Restore()
	return reopen(t, doc)
}

var twoStop = Shading{
	X0: 0, Y0: 0, X1: 0, Y1: 100,
	Stops:       []Stop{{0, RGB8(0, 0, 0)}, {1, RGB8(255, 0, 128)}},
	ExtendStart: true, ExtendEnd: true,
}

// TestAShadingIsAResourceAPDFReaderFinds: a gradient the reader cannot reach
// from the page is a gradient no viewer will paint, so the check is made
// through rsc.io/pdf rather than by matching our own bytes.
func TestAShadingIsAResourceAPDFReaderFinds(t *testing.T) {
	r := shadeDoc(t, twoStop)
	sh := r.Page(1).V.Key("Resources").Key("Shading").Key("Sh0")
	if sh.Kind() == pdf.Null {
		t.Fatal("no /Shading/Sh0 in the page resources")
	}
	if got := sh.Key("ShadingType").Int64(); got != 2 {
		t.Errorf("ShadingType = %d, want 2 (axial)", got)
	}
	if got := sh.Key("ColorSpace").Name(); got != "DeviceRGB" {
		t.Errorf("ColorSpace = %q, want DeviceRGB", got)
	}
	co := sh.Key("Coords")
	if co.Len() != 4 || co.Index(3).Float64() != 100 {
		t.Errorf("Coords = %v, want the axis that was asked for", co)
	}
	ex := sh.Key("Extend")
	if ex.Len() != 2 || !ex.Index(0).Bool() || !ex.Index(1).Bool() {
		t.Errorf("Extend = %v, want [true true]", ex)
	}
}

// TestShadePaintsThroughTheClip: PDF has no gradient fill operator, so what
// Shade writes is the sh operator naming the resource. Without it the shading
// sits in the file and never reaches the page.
func TestShadePaintsThroughTheClip(t *testing.T) {
	r := shadeDoc(t, twoStop)
	content := string(readStream(t, r.Page(1).V.Key("Contents")))
	if !strings.Contains(content, "/Sh0 sh") {
		t.Errorf("content stream has no %q:\n%s", "/Sh0 sh", content)
	}
}

// TestTwoStopsAreOneExponentialFunction: the simple gradient does not need the
// stitching machinery, and paying for it would put two dictionaries in every
// file that has one gradient.
func TestTwoStopsAreOneExponentialFunction(t *testing.T) {
	r := shadeDoc(t, twoStop)
	fn := r.Page(1).V.Key("Resources").Key("Shading").Key("Sh0").Key("Function")
	if got := fn.Key("FunctionType").Int64(); got != 2 {
		t.Fatalf("FunctionType = %d, want 2", got)
	}
	if got := fn.Key("C1").Index(0).Float64(); got < 0.99 {
		t.Errorf("C1 red = %v, want 1", got)
	}
	if got := fn.Key("C0").Index(0).Float64(); got != 0 {
		t.Errorf("C0 red = %v, want 0", got)
	}
}

// TestMoreThanTwoStopsStitch: PDF has no function type that takes a list of
// colours, so a multi-stop gradient is one exponential per pair under a
// stitching function.
func TestMoreThanTwoStopsStitch(t *testing.T) {
	r := shadeDoc(t, Shading{X1: 100,
		Stops: []Stop{{0, Gray{0}}, {0.5, Gray{0.5}}, {1, Gray{1}}}})
	fn := r.Page(1).V.Key("Resources").Key("Shading").Key("Sh0").Key("Function")
	if got := fn.Key("FunctionType").Int64(); got != 3 {
		t.Fatalf("FunctionType = %d, want 3 (stitching)", got)
	}
	if got := fn.Key("Functions").Len(); got != 2 {
		t.Errorf("%d sub-functions, want 2 for three stops", got)
	}
	if b := fn.Key("Bounds"); b.Len() != 1 || b.Index(0).Float64() != 0.5 {
		t.Errorf("Bounds = %v, want [0.5]", b)
	}
}

// TestEndsThatDoNotReachTheEdgesArePadded: a function undefined over part of
// [0,1] leaves that part of the axis unpainted. Every drawing program carries
// the end colour outwards instead, and so does this.
func TestEndsThatDoNotReachTheEdgesArePadded(t *testing.T) {
	r := shadeDoc(t, Shading{X1: 100,
		Stops: []Stop{{0.25, Gray{0}}, {0.75, Gray{1}}}})
	fn := r.Page(1).V.Key("Resources").Key("Shading").Key("Sh0").Key("Function")
	if got := fn.Key("FunctionType").Int64(); got != 3 {
		t.Fatalf("FunctionType = %d, want 3: padding adds a flat run at each end", got)
	}
	b := fn.Key("Bounds")
	if b.Len() != 2 || b.Index(0).Float64() != 0.25 || b.Index(1).Float64() != 0.75 {
		t.Errorf("Bounds = %v, want [0.25 0.75]", b)
	}
}

// TestACMYKShadingStaysCMYK is the print case: converting it to RGB on the way
// out would hand the press a colour it did not choose.
func TestACMYKShadingStaysCMYK(t *testing.T) {
	r := shadeDoc(t, Shading{X1: 100, Stops: []Stop{
		{0, CMYK{C: 0.06, M: 1, Y: 0.04}},
		{1, CMYK{K: 1}},
	}})
	sh := r.Page(1).V.Key("Resources").Key("Shading").Key("Sh0")
	if got := sh.Key("ColorSpace").Name(); got != "DeviceCMYK" {
		t.Errorf("ColorSpace = %q, want DeviceCMYK", got)
	}
	if got := sh.Key("Function").Key("C0").Len(); got != 4 {
		t.Errorf("C0 has %d components, want 4", got)
	}
}

// TestAShadingRefusesWhatPDFCannotName: each of these would otherwise be
// written as a file that opens and paints the wrong thing, which is worse than
// not writing it.
func TestAShadingRefusesWhatPDFCannotName(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	cases := []struct {
		why string
		s   Shading
	}{
		{"one stop is not a gradient", Shading{Stops: []Stop{{0, Gray{0}}}}},
		{"no stops at all", Shading{}},
		{"offsets must increase", Shading{Stops: []Stop{{0.5, Gray{0}}, {0.5, Gray{1}}}}},
		{"offsets must not go backwards", Shading{Stops: []Stop{{1, Gray{0}}, {0, Gray{1}}}}},
		{"one shading names one colour space", Shading{Stops: []Stop{{0, Gray{0}}, {1, RGB8(1, 2, 3)}}}},
	}
	for _, c := range cases {
		if err := p.Shade(c.s); err == nil {
			t.Errorf("%s: accepted", c.why)
		}
	}
	if len(p.shadings) != 0 {
		t.Errorf("%d refused shadings still reached the resources", len(p.shadings))
	}
}
