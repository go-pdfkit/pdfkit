// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"
)

// jpegInfo is the only parser in this package that reads bytes a caller
// supplied, and it reads them by hand: markers, segment lengths and offsets,
// every one of them a number out of the file. image/jpeg is the oracle it is
// measured against — not because it is the specification, but because it is
// what every consumer of the PDF we write will have used to look at the same
// bytes.
//
// ⛔ The property is NOT "jpegInfo accepts what image/jpeg accepts". jpegInfo
// stops at the first start-of-frame and never looks at the entropy-coded data,
// so it accepts files image/jpeg rejects, and that is by design: pdfkit
// embeds the bytes without decoding them. What must never happen is the two
// AGREEING TO DISAGREE — a file both read, with different dimensions — because
// then pdfkit writes a /Width into the PDF that the consumer's own decoder
// contradicts, and the picture comes out sheared with nothing to blame.
func FuzzJPEGInfo(f *testing.F) {
	f.Add(jpegBytes())
	f.Add(grayJPEGBytes(f))
	f.Add(fillBytesJPEG(f))
	f.Add([]byte{0xFF, 0xD8})
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xC0, 0x00, 0x0B, 0x08, 0x00, 0x10, 0x00, 0x10, 0x01})
	f.Add([]byte("not a jpeg at all"))
	// TEM, which stands alone (Table B.1), then a frame header.
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0x01, 0xFF, 0xC0, 0x00, 0x0B, 0x08, 0x00, 0x10, 0x00, 0x10, 0x01})
	// X'FF00': byte stuffing, not a marker.
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0x00, 0x00, 0x0B})
	// A marker with nothing behind it to hold its length.
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xC0, 0x00})
	// A frame header that declares more than it carries.
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xC0, 0x00, 0x07, 0x08, 0x00, 0x10, 0x00})
	// A frame header too short to hold a frame: present, well-formed, empty.
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xC0, 0x00, 0x04, 0xAA, 0xBB})
	// Extraneous bytes between segments, which libjpeg warns about and skips.
	f.Add([]byte{0xFF, 0xD8, 0x30, 0xFF, 0xC0, 0x00, 0x0B, 0x08, 0x00, 0x10, 0x00, 0x10, 0x01})

	f.Fuzz(func(t *testing.T, data []byte) {
		w, h, comps, err := jpegInfo(data)

		cfg, cfgErr := jpeg.DecodeConfig(bytes.NewReader(data))
		if cfgErr != nil {
			return // image/jpeg could not read it; there is nothing to compare against
		}
		if err != nil {
			t.Fatalf("image/jpeg read this as %dx%d and jpegInfo refused it (%v): a JPEG a "+
				"consumer can decode is one DrawJPEG should be able to embed",
				cfg.Width, cfg.Height, err)
		}
		if w != cfg.Width || h != cfg.Height {
			t.Fatalf("jpegInfo says %dx%d, image/jpeg says %dx%d: DrawJPEG would write a "+
				"/Width the reader's own decoder contradicts",
				w, h, cfg.Width, cfg.Height)
		}
		if comps < 1 || comps > 4 {
			t.Fatalf("jpegInfo reports %d components for a file image/jpeg decoded", comps)
		}
	})
}

// grayJPEGBytes is a one-component frame: the other colour space DrawJPEG
// names, and a different SOF component count for the parser to walk past.
func grayJPEGBytes(f *testing.F) []byte {
	f.Helper()
	var buf bytes.Buffer
	g := image.NewGray(image.Rect(0, 0, 8, 8))
	for i := range g.Pix {
		g.Pix[i] = uint8(i)
	}
	if err := jpeg.Encode(&buf, g, nil); err != nil {
		f.Fatal(err)
	}
	return buf.Bytes()
}

// fillBytesJPEG is a valid JPEG with a fill byte inserted before the marker
// that follows SOI. ⛔ A marker "may optionally be preceded by any number of
// fill bytes" (ITU-T T.81 §B.1.1.2), so this is a legal file every decoder
// accepts — and the shape a hand-written marker walk is most likely to lose
// its place in. It is a seed rather than a test so the fuzzer can build on it.
func fillBytesJPEG(f *testing.F) []byte {
	f.Helper()
	src := jpegBytes()
	out := make([]byte, 0, len(src)+2)
	out = append(out, src[0], src[1]) // SOI
	out = append(out, 0xFF, 0xFF)     // two fill bytes
	out = append(out, src[2:]...)
	return out
}

// FuzzDrawImageSamples drives the sample splitter over images whose pixels
// come from the fuzzer. It allocates nothing a caller did not already hold --
// the geometry is this test's, so the bytes are the only thing being varied --
// and it exists because unpremultiply does integer arithmetic on numbers the
// image chose.
//
// ⛔ The property is one no bug in the arithmetic can satisfy by accident: a
// fully opaque pixel must come back EXACTLY as it went in. Half-transparent
// pixels are checked by round-trip bound instead, because dividing by alpha
// and multiplying back is lossy and an exact assertion there would be a test
// of rounding rather than of correctness.
func FuzzDrawImageSamples(f *testing.F) {
	f.Add([]byte{0xFF, 0x00, 0x00, 0xFF})
	f.Add([]byte{0x80, 0x40, 0x20, 0x80})
	f.Add([]byte{0x00, 0x00, 0x00, 0x00})

	f.Fuzz(func(t *testing.T, pix []byte) {
		const w, h = 4, 4
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for i := range img.Pix {
			if i < len(pix) {
				img.Pix[i] = pix[i]
			}
		}
		// Premultiplied storage: a channel above its alpha is a state no
		// encoder produces and image/color does not define. Clamp rather than
		// skip, so every input still exercises the walk.
		for i := 0; i+3 < len(img.Pix); i += 4 {
			a := img.Pix[i+3]
			for c := range 3 {
				if img.Pix[i+c] > a {
					img.Pix[i+c] = a
				}
			}
		}

		rgb, alpha, hasAlpha := samples(img)
		if len(rgb) != w*h*3 || len(alpha) != w*h {
			t.Fatalf("samples returned %d colour and %d alpha bytes for %d pixels",
				len(rgb), len(alpha), w*h)
		}
		opaque := true
		for i := 0; i+3 < len(img.Pix); i += 4 {
			px := i / 4
			if a := img.Pix[i+3]; a != 0xFF {
				opaque = false
				continue
			}
			for c := range 3 {
				if rgb[px*3+c] != img.Pix[i+c] {
					t.Fatalf("pixel %d channel %d went in as %d and came out as %d, and it "+
						"was fully opaque: nothing should have been divided",
						px, c, img.Pix[i+c], rgb[px*3+c])
				}
			}
			if alpha[px] != 0xFF {
				t.Fatalf("pixel %d is opaque and its mask sample is %d", px, alpha[px])
			}
		}
		if opaque && hasAlpha {
			t.Fatal("every pixel is opaque and samples asked for a soft mask anyway: " +
				"the document pays for an /SMask that masks nothing")
		}
		// The straight samples must be a colour, not an overflow. Only
		// reachable through unpremultiply, which is where the arithmetic is.
		for px := range w * h {
			a := img.Pix[px*4+3]
			if a == 0 || a == 0xFF {
				continue
			}
			for c := range 3 {
				if got := rgb[px*3+c]; int(got) < int(img.Pix[px*4+c]) {
					t.Fatalf("pixel %d channel %d: %d premultiplied became %d straight at "+
						"alpha %d, which is darker than it went in",
						px, c, img.Pix[px*4+c], got, a)
				}
			}
		}
	})
}
