// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
)

func TestDrawImageOpaque(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	p.DrawImage(makeOpaqueImage(), Rect{X: 0, Y: 0, Width: 10, Height: 10})
	if len(doc.images) != 1 || doc.images[0].smask != nil {
		t.Error("opaque image should have no soft mask")
	}
	if !strings.Contains(content(p), "/Im0 Do") {
		t.Error("missing image draw operator")
	}
}

func TestDrawPNG(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	if err := p.DrawPNG(pngBytes(makeTestImage()), Rect{Width: 5, Height: 5}); err != nil {
		t.Fatal(err)
	}
	if err := p.DrawPNG([]byte("not a png"), Rect{}); err == nil {
		t.Error("expected PNG decode error")
	}
}

// craftJPEG builds a minimal JPEG header with the given component count so the
// colour-space switch and geometry read can be exercised without a real codec.
func craftJPEG(comps byte) []byte {
	return []byte{
		0xFF, 0xD8, // SOI
		0xFF, 0xD0, // RST0 marker (standalone, skipped)
		0xFF, 0xE0, 0x00, 0x04, 0x00, 0x00, // APP0 segment (skipped)
		0xFF, 0xC0, 0x00, 0x0B, 0x08, 0x00, 0x10, 0x00, 0x20, comps, 0, 0, 0, // SOF0
	}
}

func TestDrawJPEG(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)

	// Real RGB JPEG through the codec.
	if err := p.DrawJPEG(jpegBytes(), Rect{Width: 8, Height: 8}); err != nil {
		t.Fatal(err)
	}
	// Real grayscale JPEG (1 component).
	gray := image.NewGray(image.Rect(0, 0, 4, 4))
	var gbuf bytes.Buffer
	_ = jpeg.Encode(&gbuf, gray, nil)
	if err := p.DrawJPEG(gbuf.Bytes(), Rect{Width: 4, Height: 4}); err != nil {
		t.Fatal(err)
	}
	// Crafted 4-component (CMYK) header.
	if err := p.DrawJPEG(craftJPEG(4), Rect{Width: 1, Height: 1}); err != nil {
		t.Fatalf("cmyk: %v", err)
	}
	if got := doc.images[len(doc.images)-1].colorSpace; got != "DeviceCMYK" {
		t.Errorf("colorSpace = %q", got)
	}
}

func TestJPEGInfoErrors(t *testing.T) {
	cases := map[string][]byte{
		"empty":         {},
		"bad SOI":       {0x00, 0x00},
		"no marker":     {0xFF, 0xD8, 0x12, 0x34, 0x00, 0x00},
		"short segment": {0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x01},
		"unsupported":   craftJPEG(2),
		"no SOF":        {0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x02},
	}
	for name, data := range cases {
		doc := New(Options{})
		p := doc.AddPage(A4)
		if err := p.DrawJPEG(data, Rect{}); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

// writeDoc serialises doc and fails the test if that errors.
func writeDoc(t *testing.T, doc *Document) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := doc.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// countImageObjects counts the image XObject streams actually emitted.
func countImageObjects(t *testing.T, doc *Document) int {
	t.Helper()
	return bytes.Count(writeDoc(t, doc), []byte("/Subtype /Image"))
}

func TestDrawImageDedupIdentical(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	img := makeOpaqueImage()
	p.DrawImage(img, Rect{Width: 10, Height: 10})
	p.DrawImage(makeOpaqueImage(), Rect{X: 20, Width: 10, Height: 10}) // same pixels, other value

	if len(doc.images) != 1 {
		t.Fatalf("registered %d XObjects, want 1", len(doc.images))
	}
	if n := countImageObjects(t, doc); n != 1 {
		t.Errorf("emitted %d image objects, want 1", n)
	}
	// Both placements must still paint, and both through the shared name.
	if got := strings.Count(content(p), "/Im0 Do"); got != 2 {
		t.Errorf("/Im0 Do appears %d times, want 2", got)
	}
	if strings.Contains(content(p), "/Im1") {
		t.Error("a second resource name leaked into the content stream")
	}
}

func TestDrawImageDistinctNotDeduped(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	p.DrawImage(makeOpaqueImage(), Rect{Width: 10, Height: 10})
	p.DrawImage(makeTestImage(), Rect{X: 20, Width: 10, Height: 10}) // different pixels *and* alpha

	if len(doc.images) != 2 {
		t.Fatalf("registered %d XObjects, want 2", len(doc.images))
	}
	// Two placements, two names; the second carries a soft mask, so three
	// image streams reach the file.
	if n := countImageObjects(t, doc); n != 3 {
		t.Errorf("emitted %d image objects, want 3 (two images + one soft mask)", n)
	}
	c := content(p)
	if !strings.Contains(c, "/Im0 Do") || !strings.Contains(c, "/Im1 Do") {
		t.Errorf("both placements should paint distinct XObjects: %q", c)
	}
}

func TestDrawImageAlphaIsPartOfTheKey(t *testing.T) {
	// Same RGB samples, different alpha: the two must not collapse into one.
	opaque := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	translucent := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			opaque.Set(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
			translucent.Set(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
		}
	}
	doc := New(Options{})
	p := doc.AddPage(A4)
	p.DrawImage(opaque, Rect{Width: 4, Height: 4})
	p.DrawImage(translucent, Rect{X: 10, Width: 4, Height: 4})

	if len(doc.images) != 2 {
		t.Fatalf("alpha ignored by the key: registered %d XObjects, want 2", len(doc.images))
	}
	if doc.images[0].smask != nil {
		t.Error("opaque image should have no soft mask")
	}
	if doc.images[1].smask == nil {
		t.Error("translucent image should have a soft mask")
	}
}

func TestDrawPNGDedup(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	encoded := pngBytes(makeTestImage())
	for i := 0; i < 3; i++ {
		if err := p.DrawPNG(encoded, Rect{X: float64(i * 10), Width: 5, Height: 5}); err != nil {
			t.Fatal(err)
		}
	}
	if len(doc.images) != 1 {
		t.Fatalf("registered %d XObjects, want 1", len(doc.images))
	}
	// One image plus its soft mask.
	if n := countImageObjects(t, doc); n != 2 {
		t.Errorf("emitted %d image objects, want 2 (image + soft mask)", n)
	}
	if got := strings.Count(content(p), "/Im0 Do"); got != 3 {
		t.Errorf("/Im0 Do appears %d times, want 3", got)
	}
}

func TestDrawJPEGDedup(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	data := jpegBytes()
	if err := p.DrawJPEG(data, Rect{Width: 8, Height: 8}); err != nil {
		t.Fatal(err)
	}
	// A byte-identical copy, not the same slice, so identity cannot be doing
	// the work.
	if err := p.DrawJPEG(append([]byte(nil), data...), Rect{X: 20, Width: 8, Height: 8}); err != nil {
		t.Fatal(err)
	}
	if err := p.DrawJPEG(craftJPEG(4), Rect{Width: 1, Height: 1}); err != nil {
		t.Fatal(err)
	}
	if len(doc.images) != 2 {
		t.Fatalf("registered %d XObjects, want 2", len(doc.images))
	}
	if n := countImageObjects(t, doc); n != 2 {
		t.Errorf("emitted %d image objects, want 2", n)
	}
}

func TestImageDedupAcrossPages(t *testing.T) {
	doc := New(Options{})
	p1 := doc.AddPage(A4)
	p2 := doc.AddPage(A4)
	p1.DrawImage(makeOpaqueImage(), Rect{Width: 10, Height: 10})
	p2.DrawImage(makeOpaqueImage(), Rect{Width: 10, Height: 10})

	if len(doc.images) != 1 {
		t.Fatalf("registered %d XObjects, want 1 shared across pages", len(doc.images))
	}
	if n := countImageObjects(t, doc); n != 1 {
		t.Errorf("emitted %d image objects, want 1", n)
	}
	// Each page must still list it in its own /Resources /XObject dictionary.
	for i, p := range []*Page{p1, p2} {
		if !p.usedImages[doc.images[0]] {
			t.Errorf("page %d does not claim the shared XObject as a resource", i)
		}
		if !strings.Contains(content(p), "/Im0 Do") {
			t.Errorf("page %d does not paint the shared XObject", i)
		}
	}
	if n := bytes.Count(writeDoc(t, doc), []byte("/XObject <<")); n != 2 {
		t.Errorf("%d /XObject resource dictionaries, want one per page", n)
	}
}

func TestImageDedupStaysDeterministic(t *testing.T) {
	build := func() *Document {
		doc := New(Options{})
		p := doc.AddPage(A4)
		// Interleave repeats and fresh content so a map-ordered registration
		// would show up as a difference between the two writes.
		p.DrawImage(makeOpaqueImage(), Rect{Width: 4, Height: 4})
		p.DrawImage(makeTestImage(), Rect{X: 10, Width: 4, Height: 4})
		p.DrawImage(makeOpaqueImage(), Rect{X: 20, Width: 4, Height: 4})
		if err := p.DrawJPEG(jpegBytes(), Rect{X: 30, Width: 4, Height: 4}); err != nil {
			t.Fatal(err)
		}
		p.DrawImage(makeTestImage(), Rect{X: 40, Width: 4, Height: 4})
		return doc
	}
	doc := build()
	first, second := writeDoc(t, doc), writeDoc(t, doc)
	if !bytes.Equal(first, second) {
		t.Error("two writes of one document differ")
	}
	if other := writeDoc(t, build()); !bytes.Equal(first, other) {
		t.Error("two identically built documents differ")
	}
	if len(doc.images) != 3 {
		t.Errorf("registered %d XObjects, want 3", len(doc.images))
	}
}

func TestShortSOF(t *testing.T) {
	// SOF marker with a declared length under 8 bytes.
	data := []byte{0xFF, 0xD8, 0xFF, 0xC0, 0x00, 0x06, 0x08, 0x00, 0x10, 0x00, 0x20}
	if _, _, _, err := jpegInfo(data); err == nil {
		t.Error("expected short-SOF error")
	}
}
