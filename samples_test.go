// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"image"
	"image/color"
	"io"
	"math/rand/v2"
	"testing"
)

// opaqueImage hides an image's concrete type, so samples takes the generic
// path through At.
type opaqueImage struct{ image.Image }

// TestSamplesRGBAFastPath checks that the *image.RGBA path gives the bytes
// the generic path gives, on a sub-image (non-zero origin, stride wider than
// the bounds) with and without transparency.
func TestSamplesRGBAFastPath(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	full := image.NewRGBA(image.Rect(-3, -2, 61, 47))
	for i := range full.Pix {
		full.Pix[i] = byte(rng.UintN(256))
	}
	// Premultiplied: no colour channel above alpha.
	for i := 0; i < len(full.Pix); i += 4 {
		a := full.Pix[i+3]
		for c := range 3 {
			full.Pix[i+c] = min(full.Pix[i+c], a)
		}
	}
	opaque := image.NewRGBA(image.Rect(0, 0, 17, 9))
	for i := 0; i < len(opaque.Pix); i += 4 {
		opaque.Pix[i], opaque.Pix[i+1], opaque.Pix[i+2], opaque.Pix[i+3] = byte(i), byte(i/3), byte(i/7), 0xff
	}
	for name, img := range map[string]*image.RGBA{
		"transparent": full.SubImage(image.Rect(5, 4, 40, 30)).(*image.RGBA),
		"opaque":      opaque,
		"empty":       image.NewRGBA(image.Rect(0, 0, 0, 0)),
	} {
		rgb, alpha, has := samples(img)
		wrgb, walpha, whas := samples(opaqueImage{img})
		if !bytes.Equal(rgb, wrgb) || !bytes.Equal(alpha, walpha) || has != whas {
			t.Errorf("%s: the fast path differs from At (alpha %v, want %v)", name, has, whas)
		}
	}
}

func BenchmarkDrawImage(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for y := range 720 {
		for x := range 1280 {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), uint8(x + y), 0xff})
		}
	}
	for name, m := range map[string]image.Image{"rgba": img, "generic": opaqueImage{img}} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				doc := New(Options{})
				// A different pixel each time, so the document-wide image
				// cache does not answer.
				img.Pix[0] = byte(i)
				doc.AddPage(NewPageSize(960, 540)).DrawImage(m, Rect{Width: 960, Height: 540})
				if err := doc.Write(io.Discard); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
