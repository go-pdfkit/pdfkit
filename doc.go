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
// font is written as a subset with Identity-H encoding, an Identity
// CIDToGIDMap, a per-glyph /W width array and a /ToUnicode CMap so copy and
// paste recover the original text. TrueType outlines embed as a subsetted
// FontFile2 / CIDFontType2; CFF/OpenType outlines embed as a FontFile3 /
// CIDFontType0.
//
// # Determinism
//
// With the zero Options the output contains no timestamps and a content-derived
// /ID, so identical inputs produce byte-identical documents. Set Options.Now to
// stamp creation and modification dates.
//
// # Missing upstream primitives
//
// go-opentype/opentype decodes a font fully but does not expose the raw table
// bytes, the units-per-em, the glyf/loca arrays or a subsetting export that a
// PDF embedder needs, so pdfkit reparses the sfnt container it is handed (see
// sfnt.go) and implements TrueType glyf subsetting itself (see subset.go). CFF
// charstring subsetting is not yet implemented: a CFF font embeds its whole
// 'CFF ' table.
package pdfkit
