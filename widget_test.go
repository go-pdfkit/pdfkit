// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"errors"
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"rsc.io/pdf"
)

// buildScene returns a small but non-trivial widget tree: a container holding a
// prominent button (fills a rounded rect, strokes a border and draws centred
// text) and a label. It exercises fills, strokes, rounded rects and text on
// both the raster and vector paths.
func buildScene() toolkit.Widget {
	root := toolkit.NewContainer(toolkit.NewBoxLayout())
	btn := toolkit.NewButton("Hi", nil)
	btn.Style = toolkit.ButtonProminent
	root.AddWidget(btn)
	root.AddWidget(toolkit.NewLabel("Hi"))
	return root
}

// contentBytes returns page 1's (uncompressed) content stream via the
// independent rsc.io/pdf reader.
func contentBytes(t *testing.T, r *pdf.Reader) []byte {
	t.Helper()
	return readStream(t, r.Page(1).V.Key("Contents"))
}

// TestAddWidgetRaster renders a widget tree as an image XObject and verifies via
// rsc.io/pdf that the image is present, correctly sized and actually painted.
func TestAddWidgetRaster(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	rect := Rect{X: 72, Y: 500, Width: 200, Height: 120}
	if err := p.AddWidget(buildScene(), rect, &WidgetOptions{Scale: 3}); err != nil {
		t.Fatalf("AddWidget: %v", err)
	}
	r := reopen(t, doc)

	im := r.Page(1).V.Key("Resources").Key("XObject").Key("Im0")
	if got := im.Key("Subtype").Name(); got != "Image" {
		t.Fatalf("XObject Subtype = %q, want Image", got)
	}
	wantW, wantH := int64(600), int64(360) // 200*3 x 120*3
	if gw, gh := im.Key("Width").Int64(), im.Key("Height").Int64(); gw != wantW || gh != wantH {
		t.Fatalf("image dims = %dx%d, want %dx%d", gw, gh, wantW, wantH)
	}
	// The content stream must reference the image.
	if !bytes.Contains(contentBytes(t, r), []byte("/Im0 Do")) {
		t.Error("content stream does not draw /Im0")
	}
	// The decoded RGB samples must contain a painted (non-black) pixel: the
	// button fills with a visible colour, proving the tree actually rendered.
	pix := readStream(t, im)
	painted := false
	for _, b := range pix {
		if b != 0 {
			painted = true
			break
		}
	}
	if !painted {
		t.Error("raster image is entirely black; widget tree did not render")
	}
}

// TestAddWidgetRasterDefaults drives the nil-options branch (default theme +
// default scale).
func TestAddWidgetRasterDefaults(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	if err := p.AddWidget(buildScene(), Rect{X: 10, Y: 10, Width: 100, Height: 50}, nil); err != nil {
		t.Fatalf("AddWidget: %v", err)
	}
	r := reopen(t, doc)
	// Default scale is 2, so 100x50 pt -> 200x100 px.
	im := r.Page(1).V.Key("Resources").Key("XObject").Key("Im0")
	if gw, gh := im.Key("Width").Int64(), im.Key("Height").Int64(); gw != 200 || gh != 100 {
		t.Fatalf("default-scale dims = %dx%d, want 200x100", gw, gh)
	}
}

// TestAddWidgetRasterEmptyRect covers the empty-rectangle error branch.
func TestAddWidgetRasterEmptyRect(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	err := p.AddWidget(buildScene(), Rect{Width: 0, Height: 100}, &WidgetOptions{Scale: 2})
	if !errors.Is(err, errWidgetRectEmpty) {
		t.Fatalf("err = %v, want errWidgetRectEmpty", err)
	}
}

// TestAddWidgetVector renders the tree with PDF vector operators and confirms
// via rsc.io/pdf that a Type0 font resource and selectable text are present
// alongside fill/stroke path operators.
func TestAddWidgetVector(t *testing.T) {
	f, err := LoadFont(synthTTF(defaultSynth()))
	if err != nil {
		t.Fatal(err)
	}
	doc := New(Options{})
	p := doc.AddPage(A4)
	opts := &WidgetOptions{
		Theme: toolkit.DefaultDark(),
		Scale: 2,
		Font:  f,
	}
	if err := p.AddWidgetVector(buildScene(), Rect{X: 40, Y: 400, Width: 240, Height: 140}, opts); err != nil {
		t.Fatalf("AddWidgetVector: %v", err)
	}
	r := reopen(t, doc)

	// A Type0 font resource proves text became real, selectable PDF text.
	if got := firstFontDict(r).Key("Subtype").Name(); got != "Type0" {
		t.Fatalf("font Subtype = %q, want Type0", got)
	}
	content := contentBytes(t, r)
	for _, op := range [][]byte{
		[]byte("Tj"),   // text show
		[]byte("f\n"),  // fill
		[]byte("S\n"),  // stroke
		[]byte(" c\n"), // rounded-corner Bézier
		[]byte("W\n"),  // clip
	} {
		if !bytes.Contains(content, op) {
			t.Errorf("vector content stream missing operator %q", op)
		}
	}
}

// TestAddWidgetVectorNoFont covers the missing-font error branch.
func TestAddWidgetVectorNoFont(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	err := p.AddWidgetVector(buildScene(), Rect{Width: 100, Height: 100}, nil)
	if !errors.Is(err, errWidgetVectorNoFont) {
		t.Fatalf("err = %v, want errWidgetVectorNoFont", err)
	}
}

// TestAddWidgetVectorEmptyRect covers the empty-rectangle error branch on the
// vector path (with a font supplied so it reaches the size check).
func TestAddWidgetVectorEmptyRect(t *testing.T) {
	f, err := LoadFont(synthTTF(defaultSynth()))
	if err != nil {
		t.Fatal(err)
	}
	doc := New(Options{})
	p := doc.AddPage(A4)
	err = p.AddWidgetVector(buildScene(), Rect{Width: 100, Height: 0}, &WidgetOptions{Font: f})
	if !errors.Is(err, errWidgetRectEmpty) {
		t.Fatalf("err = %v, want errWidgetRectEmpty", err)
	}
}

// newTestVectorPainter builds a vectorPainter over a fresh page with a 1 pt =
// 1 px mapping, plus the document it belongs to for serialisation.
func newTestVectorPainter(t *testing.T) (*Document, *vectorPainter) {
	t.Helper()
	f, err := LoadFont(synthTTF(defaultSynth()))
	if err != nil {
		t.Fatal(err)
	}
	doc := New(Options{})
	p := doc.AddPage(A4)
	vp := &vectorPainter{
		page: p,
		rx:   0, ry: 0, rh: 200,
		sx: 1, sy: 1,
		w: 200, h: 200,
		font: f,
	}
	return doc, vp
}

// TestVectorPainterPrimitives drives every vectorPainter primitive and branch
// directly, then serialises through rsc.io/pdf to assert the expected operators
// landed in the content stream.
func TestVectorPainterPrimitives(t *testing.T) {
	doc, vp := newTestVectorPainter(t)

	red := painter.RGB(200, 20, 20)
	semi := painter.RGBA{R: 0, G: 0, B: 200, A: 128} // exercises applyAlpha (ca/CA)

	// Fills and strokes.
	vp.FillRect(painter.Rect{X: 5, Y: 5, W: 40, H: 20}, red)
	vp.FillRect(painter.Rect{X: 0, Y: 0, W: 10, H: 10}, semi)      // alpha branch
	vp.StrokeRect(painter.Rect{X: 5, Y: 40, W: 40, H: 20}, red, 0) // lineW<1 clamp

	// Rounded rects: rounded path + degrade-to-square branch.
	vp.FillRoundRect(painter.Rect{X: 60, Y: 5, W: 40, H: 30}, 8, red)
	vp.FillRoundRect(painter.Rect{X: 60, Y: 45, W: 40, H: 30}, 0, red) // radius<=0 degrade
	vp.StrokeRoundRect(painter.Rect{X: 110, Y: 5, W: 40, H: 30}, 6, red, 2)
	vp.StrokeRoundRect(painter.Rect{X: 110, Y: 45, W: 40, H: 30}, 0, red, 1) // degrade
	// Wide-short rect + oversized radius: exercises cornerRadii's r.H<r.W and
	// clamp-to-half-side branches.
	vp.FillRoundRect(painter.Rect{X: 5, Y: 80, W: 100, H: 20}, 999, red)

	// Pixel + text + size.
	vp.PutPixel(3, 3, red)
	vp.Text(10, 100, "Hi", red)
	if w, h := vp.Size(); w != 200 || h != 200 {
		t.Fatalf("Size = %dx%d, want 200x200", w, h)
	}

	// Clip push/pop, plus a pop with an empty stack (no-op branch).
	vp.PushClip(painter.Rect{X: 0, Y: 0, W: 50, H: 50})
	vp.FillRect(painter.Rect{X: 1, Y: 1, W: 10, H: 10}, red)
	vp.PopClip()
	vp.PopClip() // clipDepth == 0, no-op

	// No-op branches: fully transparent and empty geometry emit nothing.
	transparent := painter.RGBA{}
	vp.FillRect(painter.Rect{X: 0, Y: 0, W: 10, H: 10}, transparent)
	vp.FillRect(painter.Rect{X: 0, Y: 0, W: 0, H: 10}, red)
	vp.StrokeRect(painter.Rect{X: 0, Y: 0, W: 10, H: 10}, transparent, 1)
	vp.StrokeRect(painter.Rect{X: 0, Y: 0, W: 0, H: 10}, red, 1)
	vp.FillRoundRect(painter.Rect{X: 0, Y: 0, W: 10, H: 10}, 4, transparent)
	vp.FillRoundRect(painter.Rect{X: 0, Y: 0, W: 0, H: 10}, 4, red)
	vp.StrokeRoundRect(painter.Rect{X: 0, Y: 0, W: 10, H: 10}, 4, transparent, 1)
	vp.StrokeRoundRect(painter.Rect{X: 0, Y: 0, W: 0, H: 10}, 4, red, 1)
	vp.PutPixel(0, 0, transparent)
	vp.Text(0, 0, "x", transparent)
	vp.Text(0, 0, "", red)

	r := reopen(t, doc)
	content := contentBytes(t, r)
	for _, op := range [][]byte{
		[]byte("re\n"), []byte("f\n"), []byte("S\n"),
		[]byte(" c\n"), []byte("Tj"), []byte("W\n"), []byte("/GS0 gs"),
	} {
		if !bytes.Contains(content, op) {
			t.Errorf("content stream missing operator %q", op)
		}
	}
}
