// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"image"
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

func TestShortSOF(t *testing.T) {
	// SOF marker with a declared length under 8 bytes.
	data := []byte{0xFF, 0xD8, 0xFF, 0xC0, 0x00, 0x06, 0x08, 0x00, 0x10, 0x00, 0x20}
	if _, _, _, err := jpegInfo(data); err == nil {
		t.Error("expected short-SOF error")
	}
}
