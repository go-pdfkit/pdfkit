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
// runs the go-opentype shaper (GSUB/GPOS) for complex scripts. Every embedded
// font is written as a subset with Identity-H encoding, a per-glyph /W width
// array and a /ToUnicode CMap so copy and paste recover the original text.
// TrueType outlines embed as a subsetted FontFile2 / CIDFontType2 with a
// /CIDToGIDMap stream (the subset renumbers glyphs, so the map sends each CID —
// the original glyph id — to its subset id); CFF/OpenType outlines embed as a
// charstring-subsetted FontFile3 / CIDFontType0 whose glyph numbering is
// preserved, so an Identity /CIDToGIDMap suffices.
//
// # Determinism
//
// With the zero Options the output contains no timestamps and a content-derived
// /ID, so identical inputs produce byte-identical documents. Set Options.Now to
// stamp creation and modification dates.
//
// # Widget bridge
//
// AddWidget and AddWidgetVector "print" a github.com/go-widgets/toolkit widget
// tree onto a page. AddWidget lays the tree out at the target size, renders it
// through a go-widgets/painter PixelPainter and places the result as an image
// XObject — any UI, exactly as it draws on screen. AddWidgetVector runs the same
// tree through a painter that emits PDF vector operators instead of pixels, so
// fills and strokes stay crisp and text drawn through the toolkit's built-in
// font becomes real, selectable PDF text (it needs a WidgetOptions.Font). A
// widget label set in a TrueType/OpenType toolkit font (a painter.Face) embeds
// that face's own bytes and is emitted as selectable Type0 text too, so vector
// output is not limited to the toolkit's built-in bitmap font.
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
