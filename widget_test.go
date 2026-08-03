// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"errors"
	"strings"
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

// TestAddWidgetVectorTrueTypeSelectable is the end-to-end proof of the
// FacePainter seam: when the toolkit's active font is a real TrueType face
// (NewTrueTypeFont), a widget's label renders through AddWidgetVector as REAL,
// SELECTABLE Type0 text embedded in that face — not a rasterised image. It
// reuses the synthetic TrueType font for both the toolkit face and the fallback
// pdfkit Font, then reparses the output with the independent rsc.io/pdf reader.
func TestAddWidgetVectorTrueTypeSelectable(t *testing.T) {
	ttf := synthTTF(defaultSynth()) // glyphs for 'H','i','A'

	// Make a TrueType face the whole toolkit UI's active font. Draw of any text
	// now routes through truetypeFont.Draw -> painter.FacePainter.TextFace.
	ttFace, err := toolkit.NewTrueTypeFont(ttf, 16)
	if err != nil {
		t.Fatalf("toolkit.NewTrueTypeFont: %v", err)
	}
	toolkit.SetFont(ttFace)
	defer toolkit.SetFont(nil) // restore the bitmap default for the other tests

	fallback, err := LoadFont(ttf) // required by the AddWidgetVector API
	if err != nil {
		t.Fatal(err)
	}
	doc := New(Options{})
	p := doc.AddPage(A4)
	opts := &WidgetOptions{Scale: 2, Font: fallback}
	// buildScene draws the label "Hi" (and a button labelled "Hi"): both glyphs
	// exist in the synthetic face.
	if err := p.AddWidgetVector(buildScene(), Rect{X: 40, Y: 400, Width: 240, Height: 140}, opts); err != nil {
		t.Fatalf("AddWidgetVector: %v", err)
	}
	r := reopen(t, doc)

	// The embedded face is a Type0 composite font with a TrueType (CIDFontType2)
	// descendant — the signature of real, selectable text, not a bitmap.
	fd := firstFontDict(r)
	if got := fd.Key("Subtype").Name(); got != "Type0" {
		t.Fatalf("font Subtype = %q, want Type0 (selectable text, not an image)", got)
	}
	if got := fd.Key("DescendantFonts").Index(0).Key("Subtype").Name(); got != "CIDFontType2" {
		t.Errorf("descendant Subtype = %q, want CIDFontType2 (embedded TrueType)", got)
	}

	content := contentBytes(t, r)
	// A text-show operator must be present, and the run must NOT have been placed
	// as an image XObject (no `Do`): that is the "real text, not a picture" proof.
	if !bytes.Contains(content, []byte("Tj")) {
		t.Error("vector content stream has no Tj: the label did not become text")
	}
	if bytes.Contains(content, []byte(" Do\n")) {
		t.Error("vector content stream draws an XObject: text was rasterised, not shown as text")
	}
	if x := r.Page(1).V.Key("Resources").Key("XObject"); x.Kind() != pdf.Null {
		t.Error("page has an XObject resource: the vector path should place no image")
	}

	// The label glyph codes ('H'=GID 1, 'i'=GID 2 in the synthetic face) must be
	// shown, and the /ToUnicode CMap must map them back to "Hi" so a reader can
	// copy the real text out.
	for _, code := range []string{"0001", "0002"} {
		if !bytes.Contains(content, []byte(code)) {
			t.Errorf("content stream missing glyph code <%s> for the label", code)
		}
	}
	tu := string(readStream(t, fd.Key("ToUnicode")))
	for code, want := range map[string]string{"<0001> <0048>": "H", "<0002> <0069>": "i"} {
		if !strings.Contains(tu, code) {
			t.Errorf("ToUnicode missing %q (maps glyph to %q) — text would not be selectable as %q", code, want, want)
		}
	}
}

// TestVectorPainterTextFaceFallback covers TextFace's degrade-to-fallback path:
// a face whose sfnt bytes do not parse cannot be embedded, so the run is emitted
// through the plain Text primitive (the painter's fallback font) instead of
// nothing. It also covers the empty-string / transparent-ink early return and
// the face-font memoisation (a second draw of the same face hits the cache).
func TestVectorPainterTextFaceFallback(t *testing.T) {
	doc, vp := newTestVectorPainter(t)
	ink := painter.RGB(10, 20, 30)

	// Early returns: no text and no ink both no-op.
	vp.TextFace(0, 0, "", brokenFace{}, ink)
	vp.TextFace(0, 0, "x", brokenFace{}, painter.RGBA{})

	// A broken face parses to nothing, so TextFace falls back to v.Text (the
	// fallback pdfkit font), which still yields selectable text.
	vp.TextFace(4, 12, "Hi", brokenFace{}, ink)
	// Drawing the same broken face again exercises the memoised nil entry.
	vp.TextFace(4, 24, "Hi", brokenFace{}, ink)

	r := reopen(t, doc)
	if !bytes.Contains(contentBytes(t, r), []byte("Tj")) {
		t.Error("fallback TextFace produced no text-show operator")
	}
}

// brokenFace is a painter.Face whose FontData does not parse as a font, so
// pdfkit cannot embed it and must fall back to the painter's own font.
type brokenFace struct{}

func (brokenFace) FontData() []byte { return []byte("not a font") }
func (brokenFace) SizePx() int      { return 12 }
func (brokenFace) Ascent() int      { return 10 }

// TestVectorPainterTextFaceEmbedsFace drives TextFace with a parseable face
// directly (bypassing the toolkit) and confirms the face itself is embedded as
// a Type0 font and cached across calls.
func TestVectorPainterTextFaceEmbedsFace(t *testing.T) {
	doc, vp := newTestVectorPainter(t)
	face := &fakeFace{data: synthTTF(defaultSynth()), size: 16, ascent: 13}
	ink := painter.RGB(0, 0, 0)

	vp.TextFace(10, 20, "Hi", face, ink) // loads + embeds
	vp.TextFace(10, 40, "iH", face, ink) // cache hit (same face pointer)

	if len(vp.faceFonts) != 1 {
		t.Fatalf("faceFonts cached %d fonts, want 1 (memoised)", len(vp.faceFonts))
	}
	r := reopen(t, doc)
	if got := firstFontDict(r).Key("Subtype").Name(); got != "Type0" {
		t.Errorf("embedded face Subtype = %q, want Type0", got)
	}
}

// fakeFace is a minimal painter.Face backed by explicit bytes/size/ascent.
type fakeFace struct {
	data   []byte
	size   int
	ascent int
}

func (f *fakeFace) FontData() []byte { return f.data }
func (f *fakeFace) SizePx() int      { return f.size }
func (f *fakeFace) Ascent() int      { return f.ascent }

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
