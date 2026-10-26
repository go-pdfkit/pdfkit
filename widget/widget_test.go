// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package widget

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/go-opentype/fonts/dejavusans"
	"github.com/go-pdfkit/pdfkit"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"rsc.io/pdf"
)

// reopen writes doc and reparses it with the independent rsc.io/pdf reader,
// which acts as a correctness oracle for the generated file structure.
func reopen(t *testing.T, doc *pdfkit.Document) *pdf.Reader {
	t.Helper()
	var buf bytes.Buffer
	if err := doc.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	r, err := pdf.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("rsc.io/pdf reopen: %v", err)
	}
	return r
}

// firstFontDict returns page 1's /F0 font dictionary.
func firstFontDict(r *pdf.Reader) pdf.Value {
	return r.Page(1).V.Key("Resources").Key("Font").Key("F0")
}

func readStream(t *testing.T, v pdf.Value) []byte {
	t.Helper()
	rc := v.Reader()
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	return b
}

// testFont is DejaVu Sans: a real TrueType font with 'H' and 'i'.
func testFont(t *testing.T) *pdfkit.Font {
	t.Helper()
	f, err := pdfkit.LoadFont(dejavusans.TTF)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// buildScene returns a small but non-trivial widget tree: a container
// holding a prominent button (fills a rounded rect, strokes a border and
// draws centred text) and a label.
func buildScene() toolkit.Widget {
	root := toolkit.NewContainer(toolkit.NewBoxLayout())
	btn := toolkit.NewButton("Hi", nil)
	btn.Style = toolkit.ButtonProminent
	root.AddWidget(btn)
	root.AddWidget(toolkit.NewLabel("Hi"))
	return root
}

func contentBytes(t *testing.T, r *pdf.Reader) []byte {
	t.Helper()
	return readStream(t, r.Page(1).V.Key("Contents"))
}

// TestAddRaster renders a widget tree as an image XObject and verifies via
// rsc.io/pdf that the image is present, correctly sized and painted.
func TestAddRaster(t *testing.T) {
	doc := pdfkit.New(pdfkit.Options{})
	p := doc.AddPage(pdfkit.A4)
	rect := pdfkit.Rect{X: 72, Y: 500, Width: 200, Height: 120}
	if err := Add(p, buildScene(), rect, &Options{Scale: 3}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	r := reopen(t, doc)
	im := r.Page(1).V.Key("Resources").Key("XObject").Key("Im0")
	if got := im.Key("Subtype").Name(); got != "Image" {
		t.Fatalf("XObject Subtype = %q, want Image", got)
	}
	if gw, gh := im.Key("Width").Int64(), im.Key("Height").Int64(); gw != 600 || gh != 360 {
		t.Fatalf("image dims = %dx%d, want 600x360", gw, gh)
	}
	if !bytes.Contains(contentBytes(t, r), []byte("/Im0 Do")) {
		t.Error("content stream does not draw /Im0")
	}
	painted := false
	for _, b := range readStream(t, im) {
		painted = painted || b != 0
	}
	if !painted {
		t.Error("raster image is entirely black; widget tree did not render")
	}
}

// TestAddRasterDefaults drives the nil-options branch.
func TestAddRasterDefaults(t *testing.T) {
	doc := pdfkit.New(pdfkit.Options{})
	p := doc.AddPage(pdfkit.A4)
	if err := Add(p, buildScene(), pdfkit.Rect{X: 10, Y: 10, Width: 100, Height: 50}, nil); err != nil {
		t.Fatalf("Add: %v", err)
	}
	im := reopen(t, doc).Page(1).V.Key("Resources").Key("XObject").Key("Im0")
	if gw, gh := im.Key("Width").Int64(), im.Key("Height").Int64(); gw != 200 || gh != 100 {
		t.Fatalf("default-scale dims = %dx%d, want 200x100", gw, gh)
	}
}

func TestAddRasterEmptyRect(t *testing.T) {
	p := pdfkit.New(pdfkit.Options{}).AddPage(pdfkit.A4)
	if err := Add(p, buildScene(), pdfkit.Rect{Width: 0, Height: 100}, &Options{Scale: 2}); !errors.Is(err, ErrEmptyRect) {
		t.Fatalf("err = %v, want ErrEmptyRect", err)
	}
}

// TestAddVector renders the tree with PDF vector operators: a Type0 font and
// selectable text alongside fill, stroke, curve and clip operators.
func TestAddVector(t *testing.T) {
	doc := pdfkit.New(pdfkit.Options{})
	p := doc.AddPage(pdfkit.A4)
	opts := &Options{Theme: toolkit.DefaultDark(), Scale: 2, Font: testFont(t)}
	if err := AddVector(p, buildScene(), pdfkit.Rect{X: 40, Y: 400, Width: 240, Height: 140}, opts); err != nil {
		t.Fatalf("AddVector: %v", err)
	}
	r := reopen(t, doc)
	if got := firstFontDict(r).Key("Subtype").Name(); got != "Type0" {
		t.Fatalf("font Subtype = %q, want Type0", got)
	}
	content := contentBytes(t, r)
	for _, op := range [][]byte{[]byte("Tj"), []byte("f\n"), []byte("S\n"), []byte(" c\n"), []byte("W\n")} {
		if !bytes.Contains(content, op) {
			t.Errorf("vector content stream missing operator %q", op)
		}
	}
}

// TestAddVectorTrueTypeSelectable: with a TrueType face as the toolkit's
// font, a label becomes real, selectable Type0 text embedded in that face,
// not an image.
func TestAddVectorTrueTypeSelectable(t *testing.T) {
	ttFace, err := toolkit.NewTrueTypeFont(dejavusans.TTF, 16)
	if err != nil {
		t.Fatalf("toolkit.NewTrueTypeFont: %v", err)
	}
	toolkit.SetFont(ttFace)
	defer toolkit.SetFont(nil)

	doc := pdfkit.New(pdfkit.Options{})
	p := doc.AddPage(pdfkit.A4)
	if err := AddVector(p, buildScene(), pdfkit.Rect{X: 40, Y: 400, Width: 240, Height: 140}, &Options{Scale: 2, Font: testFont(t)}); err != nil {
		t.Fatalf("AddVector: %v", err)
	}
	r := reopen(t, doc)
	fd := firstFontDict(r)
	if got := fd.Key("Subtype").Name(); got != "Type0" {
		t.Fatalf("font Subtype = %q, want Type0", got)
	}
	if got := fd.Key("DescendantFonts").Index(0).Key("Subtype").Name(); got != "CIDFontType2" {
		t.Errorf("descendant Subtype = %q, want CIDFontType2", got)
	}
	content := contentBytes(t, r)
	if !bytes.Contains(content, []byte("Tj")) {
		t.Error("no Tj: the label did not become text")
	}
	if bytes.Contains(content, []byte(" Do\n")) || r.Page(1).V.Key("Resources").Key("XObject").Kind() != pdf.Null {
		t.Error("an XObject: text was rasterised")
	}
	// The ToUnicode CMap maps the label's glyphs back to H and i.
	tu := string(readStream(t, fd.Key("ToUnicode")))
	for _, u := range []string{"<0048>", "<0069>"} {
		if !strings.Contains(tu, u) {
			t.Errorf("ToUnicode has no %s: the label would not copy out", u)
		}
	}
}

// brokenFace's bytes are not a font, so TextFace falls back to Text.
type brokenFace struct{}

func (brokenFace) FontData() []byte { return []byte("not a font") }
func (brokenFace) SizePx() int      { return 12 }
func (brokenFace) Ascent() int      { return 10 }

type fakeFace struct {
	data   []byte
	size   int
	ascent int
}

func (f *fakeFace) FontData() []byte { return f.data }
func (f *fakeFace) SizePx() int      { return f.size }
func (f *fakeFace) Ascent() int      { return f.ascent }

func newTestVectorPainter(t *testing.T) (*pdfkit.Document, *vectorPainter) {
	t.Helper()
	doc := pdfkit.New(pdfkit.Options{})
	p := doc.AddPage(pdfkit.A4)
	return doc, &vectorPainter{page: p, rh: 200, sx: 1, sy: 1, w: 200, h: 200, font: testFont(t)}
}

func TestVectorPainterTextFaceFallback(t *testing.T) {
	doc, vp := newTestVectorPainter(t)
	ink := painter.RGB(10, 20, 30)
	vp.TextFace(0, 0, "", brokenFace{}, ink)
	vp.TextFace(0, 0, "x", brokenFace{}, painter.RGBA{})
	vp.TextFace(4, 12, "Hi", brokenFace{}, ink)
	vp.TextFace(4, 24, "Hi", brokenFace{}, ink) // memoised nil entry
	if !bytes.Contains(contentBytes(t, reopen(t, doc)), []byte("Tj")) {
		t.Error("fallback TextFace produced no text-show operator")
	}
}

func TestVectorPainterTextFaceEmbedsFace(t *testing.T) {
	doc, vp := newTestVectorPainter(t)
	face := &fakeFace{data: dejavusans.TTF, size: 16, ascent: 13}
	vp.TextFace(10, 20, "Hi", face, painter.RGB(0, 0, 0))
	vp.TextFace(10, 40, "iH", face, painter.RGB(0, 0, 0))
	if len(vp.faceFonts) != 1 {
		t.Fatalf("faceFonts cached %d fonts, want 1", len(vp.faceFonts))
	}
	if got := firstFontDict(reopen(t, doc)).Key("Subtype").Name(); got != "Type0" {
		t.Errorf("embedded face Subtype = %q, want Type0", got)
	}
}

func TestAddVectorNoFont(t *testing.T) {
	p := pdfkit.New(pdfkit.Options{}).AddPage(pdfkit.A4)
	if err := AddVector(p, buildScene(), pdfkit.Rect{Width: 100, Height: 100}, nil); !errors.Is(err, ErrNoFont) {
		t.Fatalf("err = %v, want ErrNoFont", err)
	}
}

func TestAddVectorEmptyRect(t *testing.T) {
	p := pdfkit.New(pdfkit.Options{}).AddPage(pdfkit.A4)
	if err := AddVector(p, buildScene(), pdfkit.Rect{Width: 100, Height: 0}, &Options{Font: testFont(t)}); !errors.Is(err, ErrEmptyRect) {
		t.Fatalf("err = %v, want ErrEmptyRect", err)
	}
}

// TestVectorPainterPrimitives drives every primitive and branch directly.
func TestVectorPainterPrimitives(t *testing.T) {
	doc, vp := newTestVectorPainter(t)
	red := painter.RGB(200, 20, 20)
	semi := painter.RGBA{R: 0, G: 0, B: 200, A: 128}
	vp.FillRect(painter.Rect{X: 5, Y: 5, W: 40, H: 20}, red)
	vp.FillRect(painter.Rect{X: 0, Y: 0, W: 10, H: 10}, semi)
	vp.StrokeRect(painter.Rect{X: 5, Y: 40, W: 40, H: 20}, red, 0)
	vp.FillRoundRect(painter.Rect{X: 60, Y: 5, W: 40, H: 30}, 8, red)
	vp.FillRoundRect(painter.Rect{X: 60, Y: 45, W: 40, H: 30}, 0, red)
	vp.StrokeRoundRect(painter.Rect{X: 110, Y: 5, W: 40, H: 30}, 6, red, 2)
	vp.StrokeRoundRect(painter.Rect{X: 110, Y: 45, W: 40, H: 30}, 0, red, 1)
	vp.FillRoundRect(painter.Rect{X: 5, Y: 80, W: 100, H: 20}, 999, red)
	vp.PutPixel(3, 3, red)
	vp.Text(10, 100, "Hi", red)
	if w, h := vp.Size(); w != 200 || h != 200 {
		t.Fatalf("Size = %dx%d, want 200x200", w, h)
	}
	vp.PushClip(painter.Rect{X: 0, Y: 0, W: 50, H: 50})
	vp.FillRect(painter.Rect{X: 1, Y: 1, W: 10, H: 10}, red)
	vp.PopClip()
	vp.PopClip() // empty stack: no-op
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
	content := contentBytes(t, reopen(t, doc))
	for _, op := range [][]byte{[]byte("re\n"), []byte("f\n"), []byte("S\n"), []byte(" c\n"), []byte("Tj"), []byte("W\n"), []byte("/GS0 gs")} {
		if !bytes.Contains(content, op) {
			t.Errorf("content stream missing operator %q", op)
		}
	}
}
