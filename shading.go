// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"fmt"
	"strconv"
)

// A Stop is one colour on a gradient, at a position along it from 0 at the
// start to 1 at the end.
type Stop struct {
	Offset float64
	Color  Color
}

// A Shading is an axial gradient: colour interpolated along the line from
// (X0,Y0) to (X1,Y1), in the page's user space.
//
// It is what a logo, a chart's fill and a printed background all need, and
// until now the only way to put one in a PDF from here was to rasterise it —
// which turns a shape that scales without limit into pixels, and on a large
// print is exactly the defect the vector file was chosen to avoid.
//
// Extend says whether the end colours carry on past the ends of the axis.
// Without them the area beyond each end is left unpainted, which for a gradient
// used as a background shows as two hard bands of nothing.
type Shading struct {
	X0, Y0, X1, Y1 float64
	Stops          []Stop
	ExtendStart    bool
	ExtendEnd      bool
}

// Shade paints the gradient over the current clip.
//
// PDF has no "fill this path with a gradient" operator: a gradient is painted
// through whatever clip is in force. So the shape comes first and the paint
// second — set the path, clip to it, then Shade:
//
//	p.MoveTo(...); p.CurveTo(...); p.ClosePath()
//	p.Clip(); p.EndPath()
//	p.Shade(g)
//
// Wrap the pair in Save and Restore, or the clip stays in force for everything
// drawn afterwards.
//
// It reports an error for fewer than two stops, for a stop with no colour, for
// offsets that do not strictly increase, and for stops that do not all share
// one colour space —
// PDF names the space once for the whole shading, so a gradient from grey to
// CMYK cannot be written rather than being quietly converted.
func (p *Page) Shade(s Shading) error {
	dict, err := s.dict()
	if err != nil {
		return err
	}
	name := "Sh" + strconv.Itoa(len(p.shadings))
	p.shadings = append(p.shadings, dict)
	p.op("/"+name, "sh")
	return nil
}

// dict builds the shading dictionary, validating as it goes.
func (s Shading) dict() (*pdfDict, error) {
	if len(s.Stops) < 2 {
		return nil, fmt.Errorf("pdfkit: a shading needs at least two stops, got %d", len(s.Stops))
	}
	// Every stop is checked for a colour before any is asked for its space:
	// asking first would panic on a nil colour in stop 0 rather than say what
	// is wrong with it.
	for i, st := range s.Stops {
		if st.Color == nil {
			return nil, fmt.Errorf("pdfkit: shading stop %d has no colour", i)
		}
	}
	space := s.Stops[0].Color.space()
	for i, st := range s.Stops {
		if st.Color.space() != space {
			return nil, fmt.Errorf("pdfkit: shading stop %d is %s where stop 0 is %s; one shading names one colour space",
				i, st.Color.space(), space)
		}
		if i > 0 && st.Offset <= s.Stops[i-1].Offset {
			return nil, fmt.Errorf("pdfkit: shading stop %d is at %v, not past stop %d at %v",
				i, st.Offset, i-1, s.Stops[i-1].Offset)
		}
	}

	// The function is defined over the whole of [0,1], so a gradient whose
	// first stop starts late or whose last ends early is padded with its own
	// end colour. That is what every drawing program shows for the same input,
	// and leaving the ends undefined would show as unpainted bands instead.
	stops := make([]Stop, len(s.Stops))
	copy(stops, s.Stops)
	if stops[0].Offset > 0 {
		stops = append([]Stop{{Offset: 0, Color: stops[0].Color}}, stops...)
	}
	if last := stops[len(stops)-1]; last.Offset < 1 {
		stops = append(stops, Stop{Offset: 1, Color: last.Color})
	}

	d := newDict()
	d.set("ShadingType", pdfInt(2))
	d.set("ColorSpace", space)
	d.set("Coords", pdfArray{pdfReal(s.X0), pdfReal(s.Y0), pdfReal(s.X1), pdfReal(s.Y1)})
	d.set("Function", stitch(stops))
	d.set("Extend", pdfArray{pdfBool(s.ExtendStart), pdfBool(s.ExtendEnd)})
	return d, nil
}

// stitch builds the function that maps a position along the axis to a colour.
//
// Two stops are one exponential function interpolating between them. More are
// a stitching function over one exponential per pair, which is how PDF spells
// a multi-stop gradient: it has no single function type that takes a list of
// colours.
func stitch(stops []Stop) *pdfDict {
	if len(stops) == 2 {
		return exponential(stops[0].Color, stops[1].Color)
	}
	fns := make(pdfArray, 0, len(stops)-1)
	bounds := make(pdfArray, 0, len(stops)-2)
	encode := make(pdfArray, 0, 2*(len(stops)-1))
	for i := 0; i+1 < len(stops); i++ {
		fns = append(fns, exponential(stops[i].Color, stops[i+1].Color))
		if i > 0 {
			bounds = append(bounds, pdfReal(stops[i].Offset))
		}
		// Each sub-function is written over its own [0,1], so the stitcher
		// maps every sub-domain onto the whole of it.
		encode = append(encode, pdfReal(0), pdfReal(1))
	}
	d := newDict()
	d.set("FunctionType", pdfInt(3))
	d.set("Domain", pdfArray{pdfReal(0), pdfReal(1)})
	d.set("Functions", fns)
	d.set("Bounds", bounds)
	d.set("Encode", encode)
	return d
}

// exponential interpolates linearly between two colours: a type 2 function
// with N=1.
func exponential(c0, c1 Color) *pdfDict {
	toArr := func(c Color) pdfArray {
		cs := c.comps()
		a := make(pdfArray, len(cs))
		for i, v := range cs {
			a[i] = pdfReal(v)
		}
		return a
	}
	d := newDict()
	d.set("FunctionType", pdfInt(2))
	d.set("Domain", pdfArray{pdfReal(0), pdfReal(1)})
	d.set("C0", toArr(c0))
	d.set("C1", toArr(c1))
	d.set("N", pdfInt(1))
	return d
}
