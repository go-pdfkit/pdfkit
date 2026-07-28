// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

// PDF's default user-space unit is the point: 1/72 inch. These helpers convert
// physical units to points so pages and coordinates can be expressed naturally.
const (
	pointsPerInch = 72.0
	mmPerInch     = 25.4
)

// Pt returns v points unchanged. It documents intent at call sites.
func Pt(v float64) float64 { return v }

// Mm converts millimetres to points.
func Mm(v float64) float64 { return v / mmPerInch * pointsPerInch }

// In converts inches to points.
func In(v float64) float64 { return v * pointsPerInch }

// PageSize is a page's dimensions in points.
type PageSize struct {
	Width  float64
	Height float64
}

// Landscape returns s rotated to landscape orientation (width >= height).
func (s PageSize) Landscape() PageSize {
	if s.Width >= s.Height {
		return s
	}
	return PageSize{Width: s.Height, Height: s.Width}
}

// Portrait returns s in portrait orientation (height >= width).
func (s PageSize) Portrait() PageSize {
	if s.Height >= s.Width {
		return s
	}
	return PageSize{Width: s.Height, Height: s.Width}
}

// Standard ISO 216 A-series and US page sizes, in points.
var (
	A3     = PageSize{Width: Mm(297), Height: Mm(420)}
	A4     = PageSize{Width: Mm(210), Height: Mm(297)}
	A5     = PageSize{Width: Mm(148), Height: Mm(210)}
	Letter = PageSize{Width: In(8.5), Height: In(11)}
	Legal  = PageSize{Width: In(8.5), Height: In(14)}
	Tabloid = PageSize{Width: In(11), Height: In(17)}
)

// NewPageSize builds a custom page size from a width and height in points.
func NewPageSize(width, height float64) PageSize {
	return PageSize{Width: width, Height: height}
}

// Rect is an axis-aligned rectangle in user space, given by its lower-left
// corner and its size.
type Rect struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}
