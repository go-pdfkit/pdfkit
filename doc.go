// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

// Package pdfkit is a pure-Go, CGO-free PDF 1.7 writer with a Go-idiomatic API.
//
// It builds documents from pages, draws vector graphics and text, embeds
// TrueType and OpenType/CFF fonts as subsetted composite (Type0) fonts, and
// places JPEG and raster images. Fonts are parsed and shaped with
// github.com/go-opentype/opentype; nothing outside the Go standard library and
// our own pure-Go libraries is required.
//
// # Quick start
//
//	doc := pdfkit.New(pdfkit.Options{})
//	face, _ := pdfkit.LoadFont(ttfBytes)
//	p := doc.AddPage(pdfkit.A4)
//	p.SetFont(face, 24)
//	p.Text(72, 720, "Hello, PDF")
//	_ = doc.Write(w) // any io.Writer
//
// # Coordinate system
//
// User space is measured in points (1/72 inch) with the origin at the
// lower-left corner and y increasing upward, matching PDF. The Pt, Mm and In
// helpers convert physical units; the standard page sizes (A4, Letter, ...)
// and NewPageSize give a page's dimensions.
//
// # Text and fonts
//
// LoadFont parses a font blob once; a Font is immutable and may be shared.
// SetFont selects it for a page, then Text draws a left-to-right run. TextShaped
// runs the go-opentype shaper (GSUB/GPOS) for complex scripts and writes the
// run as one TJ array per baseline segment, with a numeric correction only
// where shaping departs from the font's own advances (kerning, marks). Font
// and colour operators are written once and not repeated while unchanged;
// coordinates carry at most four decimals. Every embedded
// font is written as a subset with Identity-H encoding, a per-glyph /W width
// array and a /ToUnicode CMap so copy and paste recover the original text.
// TrueType outlines embed as a subsetted FontFile2 / CIDFontType2 with a
// /CIDToGIDMap stream (the subset renumbers glyphs, so the map sends each CID —
// the original glyph id — to its subset id); CFF/OpenType outlines embed as a
// charstring-subsetted FontFile3 / CIDFontType0 whose glyph numbering is
// preserved, so an Identity /CIDToGIDMap suffices.
//
// # Images
//
// DrawJPEG stores the original JPEG bytes as a DCTDecode stream, so nothing is
// re-encoded. DrawPNG and DrawImage walk any image.Image into 8-bit DeviceRGB
// samples compressed with FlateDecode, and an image that is not fully opaque
// gets its alpha channel as a DeviceGray /SMask.
//
// An image is content-addressed by its uncompressed samples together with the
// width, height, colour space, bits per component and the alpha samples — the
// raw JPEG bytes in the DCTDecode case. Pixel-identical bitmaps therefore share
// a single XObject across the whole document, whichever page paints them and
// whichever entry point embedded them; the samples are hashed before they are
// compressed, so a repeat never pays for compression twice. This matters for
// pages that reuse one icon many times: an HTML renderer feeding pdfkit 166
// rasterised copies of a handful of icon SVGs now emits a handful of streams.
//
// # Determinism
//
// With the zero Options the output contains no timestamps and a content-derived
// /ID, so identical inputs produce byte-identical documents. Set Options.Now to
// stamp creation and modification dates. The image cache is consulted but never
// iterated: object order and /Im<i> numbering follow first-sighting order, so
// deduplication does not put map iteration on the output path.
//
// # Widget bridge
//
// The subpackage github.com/go-pdfkit/pdfkit/widget "prints" a
// github.com/go-widgets/toolkit widget tree onto a page: widget.Add as an
// image, widget.AddVector as vector operators with selectable text. It is a
// separate package so that importing pdfkit does not link the toolkit; a test
// fails if the root package's import graph ever reaches it again. Until
// v0.20.0 these were Page.AddWidget and Page.AddWidgetVector.
//
// # Font embedding
//
// go-opentype/opentype supplies every primitive PDF embedding needs: the
// descriptor scalars (units-per-em, bounding box, ascent/descent, cap height,
// italic angle, flags, StemV), the by-glyph advances for the /W array, and the
// glyph subsetters. TrueType 'glyf' fonts are subsetted with
// Font.SubsetTrueType and CFF fonts with Font.SubsetCFF, so pdfkit keeps no
// private sfnt re-parse or subsetter of its own. A CID-keyed CFF or a CFF2
// (variable) font, which the preserve-numbering CFF subsetter does not handle,
// gracefully falls back to embedding its whole 'CFF '/'CFF2' table.
package pdfkit
