// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/png"
)

// imageXObject is one embedded image and, optionally, its soft-mask companion.
// The sample data is stored already filtered (FlateDecode for rasters, the raw
// JPEG bytes for DCTDecode).
type imageXObject struct {
	name       string
	width      int
	height     int
	colorSpace string
	bpc        int
	filter     string
	data       []byte
	smask      *imageXObject
}

// imageKey content-addresses an image by the bytes a PDF consumer would see
// once the filter is undone: the *uncompressed* samples plus the geometry and
// colour parameters that give them meaning, and the alpha samples that would
// become a soft mask. The lengths go into the digest ahead of the payloads so
// no shift of bytes between the two slices can forge a match.
//
// Hashing the raw samples (rather than the filtered stream) is what lets a
// duplicate be recognised *before* it pays for compression.
func imageKey(filter, colorSpace string, w, h, bpc int, samples, alpha []byte) string {
	sum := sha256.New()
	fmt.Fprintf(sum, "%s|%s|%d|%d|%d|%d|%d|", filter, colorSpace, w, h, bpc, len(samples), len(alpha))
	sum.Write(samples)
	sum.Write(alpha)
	return string(sum.Sum(nil))
}

// DrawImage embeds img and paints it into the rectangle r (in points). Any
// alpha channel becomes a soft mask, so partially transparent images composite
// correctly. Sample data is FlateDecode-compressed.
//
// Pixel-identical images share one XObject document-wide: the samples are
// hashed before they are compressed, so redrawing the same bitmap costs a hash
// and nothing else.
func (p *Page) DrawImage(img image.Image, r Rect) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	rgb, alpha, hasAlpha := samples(img)
	if !hasAlpha {
		alpha = nil // a fully opaque image gets no soft mask, and keys as such
	}
	key := imageKey("FlateDecode", "DeviceRGB", w, h, 8, rgb, alpha)
	x := p.doc.imageFor(key, func() *imageXObject {
		x := &imageXObject{
			width:      w,
			height:     h,
			colorSpace: "DeviceRGB",
			bpc:        8,
			filter:     "FlateDecode",
			data:       flateCompress(rgb),
		}
		if alpha != nil {
			x.smask = &imageXObject{
				width:      w,
				height:     h,
				colorSpace: "DeviceGray",
				bpc:        8,
				filter:     "FlateDecode",
				data:       flateCompress(alpha),
			}
		}
		return x
	})
	p.placeImage(x, r)
}

// samples splits an image into 8-bit RGB samples and alpha samples, and
// reports whether any pixel is not fully opaque.
//
// The colour samples are written straight, NOT alpha-premultiplied: an image
// with a soft mask whose /Matte entry is absent "shall not be preblended"
// (ISO 32000-1 §11.6.5.3, Table 145), and a viewer composites its samples
// as given. image.Image's RGBA method and an *image.RGBA's Pix are both
// premultiplied, so every pixel that is neither opaque nor fully transparent
// is divided back by its alpha here; before, a half-transparent edge came out
// darkened towards black.
//
// An *image.RGBA already stores exactly those bytes, so it is copied row by
// row; any other image goes through At, which boxes every pixel in an
// interface (a 1280×720 page: 921,600 allocations).
func samples(img image.Image) (rgb, alpha []byte, hasAlpha bool) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	rgb = make([]byte, w*h*3)
	alpha = make([]byte, w*h)
	if m, ok := img.(*image.RGBA); ok {
		o, a := 0, 0
		for y := b.Min.Y; y < b.Max.Y; y++ {
			row := m.Pix[m.PixOffset(b.Min.X, y):m.PixOffset(b.Max.X, y)]
			for i := 0; i < len(row); i += 4 {
				rgb[o], rgb[o+1], rgb[o+2] = row[i], row[i+1], row[i+2]
				alpha[a] = row[i+3]
				if row[i+3] != 0xff {
					hasAlpha = true
					unpremultiply(rgb[o:o+3], row[i+3])
				}
				o += 3
				a++
			}
		}
		return rgb, alpha, hasAlpha
	}
	o, a := 0, 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			cr, cg, cb, ca := img.At(x, y).RGBA()
			rgb[o], rgb[o+1], rgb[o+2] = byte(cr>>8), byte(cg>>8), byte(cb>>8)
			alpha[a] = byte(ca >> 8)
			if ca != 0xffff {
				hasAlpha = true
				unpremultiply(rgb[o:o+3], byte(ca>>8))
			}
			o += 3
			a++
		}
	}
	return rgb, alpha, hasAlpha
}

// unpremultiply turns three premultiplied 8-bit samples into straight ones
// for alpha a, rounding to nearest. A fully transparent pixel's colour is
// meaningless and is left as it is (premultiplied, it is 0 already).
func unpremultiply(c []byte, a byte) {
	if a == 0 || a == 0xff {
		return
	}
	for i := range c {
		v := (int(c[i])*255 + int(a)/2) / int(a)
		if v > 255 {
			v = 255
		}
		c[i] = byte(v)
	}
}

// DrawPNG decodes PNG bytes and embeds the image into r. It returns an error if
// the data is not a valid PNG, or if it declares more than
// [Options.MaxPixels] pixels.
func (p *Page) DrawPNG(data []byte, r Rect) error {
	// ⛔ THE HEADER FIRST. png.Decode allocates the whole pixel buffer from
	// IHDR before it reads a byte of image data, so a ceiling applied to what
	// it returns is not a ceiling, it is a post-mortem: a seventy-five byte
	// file claiming 20000x20000 costs 1.5 GiB on the way to being rejected.
	// The spec asks for exactly this reading order — "PNG decoders should
	// detect errors as early as possible" (W3C PNG 3rd ed. §4.10).
	//
	// Multiplied in int64 because the two factors are not ours: IHDR's width
	// and height are four-byte unsigned integers over 0..2^31-1 (§11.2.1), and
	// two of those multiplied in int on a 32-bit build is how a ceiling is
	// passed by overflowing past it.
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("pdfkit: decode png header: %w", err)
	}
	if px := int64(cfg.Width) * int64(cfg.Height); px > int64(p.doc.maxPixels()) {
		return fmt.Errorf("pdfkit: a PNG of %dx%d is %d pixels, past the %d allowed",
			cfg.Width, cfg.Height, px, p.doc.maxPixels())
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("pdfkit: decode png: %w", err)
	}
	// go-pdfkit/convert#4 measures what came back as well, because in general
	// a header is a claim. Not repeated here: both calls above read their
	// dimensions from the same IHDR, so a second comparison could not differ,
	// and an unreachable branch is a comfort rather than a check.
	// TestTheDecodedSizeIsTheSizeTheHeaderDeclared holds that assumption.
	p.DrawImage(img, r)
	return nil
}

// DrawJPEG embeds JPEG bytes directly (DCTDecode, no re-encoding) into r,
// preserving the original compression. Grayscale (1), RGB/YCbCr (3) and CMYK
// (4) component counts are supported.
func (p *Page) DrawJPEG(data []byte, r Rect) error {
	w, h, comps, err := jpegInfo(data)
	if err != nil {
		return err
	}
	cs := ""
	switch comps {
	case 1:
		cs = "DeviceGray"
	case 3:
		cs = "DeviceRGB"
	case 4:
		cs = "DeviceCMYK"
	default:
		return fmt.Errorf("pdfkit: unsupported JPEG component count %d", comps)
	}
	key := imageKey("DCTDecode", cs, w, h, 8, data, nil)
	x := p.doc.imageFor(key, func() *imageXObject {
		return &imageXObject{
			width:      w,
			height:     h,
			colorSpace: cs,
			bpc:        8,
			filter:     "DCTDecode",
			data:       data,
		}
	})
	p.placeImage(x, r)
	return nil
}

// placeImage emits the operators that paint an already-registered XObject
// into r, and records it as a resource this page uses.
func (p *Page) placeImage(x *imageXObject, r Rect) {
	p.usedImages[x] = true
	p.Save()
	p.Transform(r.Width, 0, 0, r.Height, r.X, r.Y)
	p.op("/"+x.name, "Do")
	p.Restore()
}

// jpegInfo scans a JFIF/EXIF JPEG for its first start-of-frame marker and
// returns the image width, height and component count.
func jpegInfo(data []byte) (w, h, comps int, err error) {
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0, 0, 0, fmt.Errorf("pdfkit: not a JPEG (bad SOI)")
	}
	// ⛔ The three skips below are not leniency for its own sake. DrawJPEG
	// embeds these bytes UNTOUCHED, so whoever opens the PDF decodes the same
	// file with a libjpeg-shaped decoder — and a writer that refuses what every
	// reader of its own output accepts is the one in the wrong. The rules are
	// taken from Go's image/jpeg (reader.go:542-581), which states where each
	// comes from; both divergences below were found by FuzzJPEGInfo, within a
	// second of each other, against image/jpeg as the oracle.
	i := 2
	for i+1 < len(data) {
		// Extraneous non-marker bytes between segments. libjpeg's next_marker
		// raises JWRN_EXTRANEOUS_DATA and skips to the next X'FF'; image/jpeg
		// does the same and so do we. (Found: a file image/jpeg read as
		// 12336x12336 was refused here as "expected marker".)
		if data[i] != 0xFF {
			i++
			continue
		}
		marker := data[i+1]
		// X'FF00' is byte stuffing inside entropy-coded data, not a marker —
		// a marker's second byte is "not equal to 0 or X'FF'" (T.81 §B.1.1.2).
		if marker == 0x00 {
			i += 2
			continue
		}
		// "Any marker may optionally be preceded by any number of fill bytes,
		// which are bytes assigned code X'FF'" (§B.1.1.2). (Found: a legal
		// file image/jpeg read as 4x4 was refused here as a bad segment
		// length, because the walk took the second X'FF' for the marker and
		// then read the real marker as a length.)
		if marker == 0xFF {
			i++
			continue
		}
		// The markers that stand alone and carry no segment: Table B.1's
		// asterisked rows — SOI, EOI, the eight restarts, and TEM.
		if marker == 0xD8 || marker == 0xD9 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		if i+4 > len(data) {
			return 0, 0, 0, fmt.Errorf("pdfkit: corrupt JPEG (truncated segment)")
		}
		seg := int(data[i+2])<<8 | int(data[i+3])
		if seg < 2 || i+2+seg > len(data) {
			return 0, 0, 0, fmt.Errorf("pdfkit: corrupt JPEG (bad segment length)")
		}
		// SOF markers carry the frame geometry; C4/C8/CC are not frame headers.
		if marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC {
			if seg < 8 {
				return 0, 0, 0, fmt.Errorf("pdfkit: corrupt JPEG (short SOF)")
			}
			h = int(data[i+5])<<8 | int(data[i+6])
			w = int(data[i+7])<<8 | int(data[i+8])
			comps = int(data[i+9])
			return w, h, comps, nil
		}
		i += 2 + seg
	}
	return 0, 0, 0, fmt.Errorf("pdfkit: no SOF marker in JPEG")
}

// buildImage serialises an image XObject (and its soft mask) into the document.
func (d *Document) buildImage(bd *builder, x *imageXObject) objRef {
	var smaskRef objRef
	if x.smask != nil {
		smaskRef = d.buildImage(bd, x.smask)
	}
	dict := newDict()
	dict.set("Type", pdfName("XObject"))
	dict.set("Subtype", pdfName("Image"))
	dict.set("Width", pdfInt(x.width))
	dict.set("Height", pdfInt(x.height))
	dict.set("ColorSpace", pdfName(x.colorSpace))
	dict.set("BitsPerComponent", pdfInt(x.bpc))
	dict.set("Filter", pdfName(x.filter))
	if x.smask != nil {
		dict.set("SMask", smaskRef)
	}
	return bd.add(&pdfStream{dict: dict, data: x.data})
}
