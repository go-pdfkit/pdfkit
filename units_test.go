// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestUnits(t *testing.T) {
	if Pt(10) != 10 {
		t.Error("Pt")
	}
	if !approx(In(1), 72) {
		t.Errorf("In(1) = %v", In(1))
	}
	if !approx(Mm(25.4), 72) {
		t.Errorf("Mm(25.4) = %v", Mm(25.4))
	}
}

func TestOrientation(t *testing.T) {
	if l := A4.Landscape(); l.Width < l.Height {
		t.Error("Landscape not wide")
	}
	// Already landscape returns unchanged.
	wide := PageSize{Width: 100, Height: 50}
	if wide.Landscape() != wide {
		t.Error("already-landscape changed")
	}
	if p := A4.Portrait(); p.Height < p.Width {
		t.Error("Portrait not tall")
	}
	// Already portrait returns unchanged.
	tall := PageSize{Width: 50, Height: 100}
	if tall.Portrait() != tall {
		t.Error("already-portrait changed")
	}
	// Portrait of a landscape swaps.
	if p := wide.Portrait(); p.Width != 50 || p.Height != 100 {
		t.Errorf("portrait swap = %+v", p)
	}
}

func TestNewPageSize(t *testing.T) {
	s := NewPageSize(123, 456)
	if s.Width != 123 || s.Height != 456 {
		t.Errorf("NewPageSize = %+v", s)
	}
}
