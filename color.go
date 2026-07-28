// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import "strings"

// Color is a paint in one of PDF's device colour spaces. Its ops method emits
// the content-stream operator that selects it for filling (stroke=false) or
// stroking (stroke=true). Component values are in the range [0,1].
type Color interface {
	ops(stroke bool) string
}

// join renders the components followed by the fill or stroke operator.
func join(comps []float64, fillOp, strokeOp string, stroke bool) string {
	var b strings.Builder
	for _, c := range comps {
		b.WriteString(ftoa(c))
		b.WriteByte(' ')
	}
	if stroke {
		b.WriteString(strokeOp)
	} else {
		b.WriteString(fillOp)
	}
	return b.String()
}

// Gray is a DeviceGray colour: a single intensity from 0 (black) to 1 (white).
type Gray struct{ V float64 }

func (c Gray) ops(stroke bool) string {
	return join([]float64{c.V}, "g", "G", stroke)
}

// RGB is a DeviceRGB colour with red, green and blue components in [0,1].
type RGB struct{ R, G, B float64 }

func (c RGB) ops(stroke bool) string {
	return join([]float64{c.R, c.G, c.B}, "rg", "RG", stroke)
}

// RGB8 builds an RGB colour from 8-bit components (0-255).
func RGB8(r, g, b uint8) RGB {
	return RGB{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255}
}

// CMYK is a DeviceCMYK colour with cyan, magenta, yellow and black components
// in [0,1].
type CMYK struct{ C, M, Y, K float64 }

func (c CMYK) ops(stroke bool) string {
	return join([]float64{c.C, c.M, c.Y, c.K}, "k", "K", stroke)
}
