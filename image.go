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
	rgb := make([]byte, 0, w*h*3)
	alpha := make([]byte, 0, w*h)
	hasAlpha := false
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			cr, cg, cb, ca := img.At(x, y).RGBA()
			rgb = append(rgb, byte(cr>>8), byte(cg>>8), byte(cb>>8))
			alpha = append(alpha, byte(ca>>8))
			if ca != 0xffff {
				hasAlpha = true
			}
		}
	}
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

// DrawPNG decodes PNG bytes and embeds the image into r. It returns an error if
// the data is not a valid PNG.
func (p *Page) DrawPNG(data []byte, r Rect) error {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("pdfkit: decode png: %w", err)
	}
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
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return 0, 0, 0, fmt.Errorf("pdfkit: corrupt JPEG (expected marker)")
		}
		marker := data[i+1]
		if marker == 0xD8 || marker == 0xD9 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
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
