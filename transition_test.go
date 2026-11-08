// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"math"
	"testing"

	"rsc.io/pdf"
)

// A page's transition and display duration read back through the independent
// reader as PDF 32000-1 Table 162 writes them, classic or packed; a page
// without one has neither entry.
func TestTransition(t *testing.T) {
	for _, packed := range []bool{false, true} {
		doc := New(Options{ObjectStreams: packed, Compress: packed})
		p := doc.AddPage(A4)
		if err := p.SetTransition(Transition{Style: TransitionPush, Duration: 0.5, Direction: 270}); err != nil {
			t.Fatal(err)
		}
		if err := p.SetDisplayDuration(5); err != nil {
			t.Fatal(err)
		}
		q := doc.AddPage(A4)
		if err := q.SetTransition(Transition{Style: TransitionSplit, Vertical: true, Outward: true}); err != nil {
			t.Fatal(err)
		}
		doc.AddPage(A4)

		r := reopenBytes(t, writeDoc(t, doc))
		tr := r.Page(1).V.Key("Trans")
		if tr.Key("Type").Name() != "Trans" || tr.Key("S").Name() != "Push" || tr.Key("D").Float64() != 0.5 || int(tr.Key("Di").Float64()) != 270 {
			t.Errorf("packed=%v: page 1 /Trans = %v", packed, tr)
		}
		if tr.Key("Dm").Kind() != pdf.Null || tr.Key("M").Kind() != pdf.Null {
			t.Errorf("packed=%v: a Push wrote Dm or M: %v", packed, tr)
		}
		if got := r.Page(1).V.Key("Dur").Float64(); got != 5 {
			t.Errorf("packed=%v: /Dur = %v", packed, got)
		}
		tr = r.Page(2).V.Key("Trans")
		if tr.Key("S").Name() != "Split" || tr.Key("Dm").Name() != "V" || tr.Key("M").Name() != "O" || tr.Key("D").Kind() != pdf.Null {
			t.Errorf("packed=%v: page 2 /Trans = %v", packed, tr)
		}
		if r.Page(3).V.Key("Trans").Kind() != pdf.Null || r.Page(3).V.Key("Dur").Kind() != pdf.Null {
			t.Errorf("packed=%v: page 3 has presentation entries", packed)
		}
	}
}

// What Table 162 says a style does not take is refused, and nothing is set.
func TestTransitionRefusals(t *testing.T) {
	bad := []Transition{
		{Style: "Fly"},                        // not written here
		{Style: ""},                           // no style
		{Style: TransitionFade, Duration: -1}, //
		{Style: TransitionFade, Duration: math.NaN()},
		{Style: TransitionFade, Vertical: true},     // Dm: Split and Blinds
		{Style: TransitionWipe, Outward: true},      // M: Split, Box and Fly
		{Style: TransitionPush, Direction: 180},     // 180 is Wipe only
		{Style: TransitionGlitter, Direction: 90},   // 90 is Wipe only
		{Style: TransitionDissolve, Direction: 270}, // Di: Wipe, Glitter, Cover, Uncover, Push
		{Style: TransitionWipe, Direction: 315},     // 315 is Glitter only
		{Style: TransitionCover, Direction: 45},
	}
	for _, tr := range bad {
		p := New(Options{}).AddPage(A4)
		if err := p.SetTransition(tr); err == nil {
			t.Errorf("%+v was accepted", tr)
		}
		if p.transition != nil {
			t.Errorf("%+v changed the page", tr)
		}
	}
	good := []Transition{
		{Style: TransitionWipe, Direction: 180}, {Style: TransitionGlitter, Direction: 315},
		{Style: TransitionBlinds, Vertical: true}, {Style: TransitionBox, Outward: true},
		{Style: TransitionUncover, Direction: 270}, {Style: TransitionReplace}, {Style: TransitionDissolve},
	}
	for _, tr := range good {
		if err := New(Options{}).AddPage(A4).SetTransition(tr); err != nil {
			t.Errorf("%+v: %v", tr, err)
		}
	}
	p := New(Options{}).AddPage(A4)
	for _, d := range []float64{-1, math.NaN(), math.Inf(1)} {
		if p.SetDisplayDuration(d) == nil {
			t.Errorf("display duration %v was accepted", d)
		}
	}
	if p.SetDisplayDuration(0) != nil {
		t.Error("0 removes the duration")
	}
}
