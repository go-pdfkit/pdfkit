// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"fmt"
	"math"
)

// TransitionStyle is the /S entry of a transition dictionary: how a
// presentation moves to a page from another (PDF 32000-1 12.4.4.1,
// Table 162).
type TransitionStyle string

// The transition styles, with the PDF version that introduced each.
const (
	TransitionSplit    TransitionStyle = "Split"
	TransitionBlinds   TransitionStyle = "Blinds"
	TransitionBox      TransitionStyle = "Box"
	TransitionWipe     TransitionStyle = "Wipe"
	TransitionDissolve TransitionStyle = "Dissolve"
	TransitionGlitter  TransitionStyle = "Glitter"
	// TransitionReplace is /R, the default: the new page replaces the old
	// one, with no effect.
	TransitionReplace TransitionStyle = "R"
	// PDF 1.5.
	TransitionPush    TransitionStyle = "Push"
	TransitionCover   TransitionStyle = "Cover"
	TransitionUncover TransitionStyle = "Uncover"
	TransitionFade    TransitionStyle = "Fade"
)

// Transition is the transition dictionary of a page: the effect of moving to
// the page from the one before it. The zero value of every field but Style is
// the entry's default, and writes nothing.
//
// The Fly style, which needs /SS and /B as well, is not offered.
type Transition struct {
	Style TransitionStyle
	// Duration is /D, the effect's length in seconds; 0 leaves it to the
	// viewer's default, which is 1 second.
	Duration float64
	// Vertical is /Dm /V, the dimension of Split and Blinds (default
	// horizontal).
	Vertical bool
	// Outward is /M /O, the motion of Split and Box: from the centre rather
	// than inward from the edges.
	Outward bool
	// Direction is /Di, in degrees counterclockwise from left-to-right, for
	// Wipe, Glitter, Cover, Uncover and Push: 0 left to right (the default),
	// 90 bottom to top and 180 right to left (Wipe only), 270 top to bottom,
	// 315 top-left to bottom-right (Glitter only).
	Direction int
}

// SetTransition gives the page a transition dictionary (/Trans). It returns
// an error, and changes nothing, for a style this package does not write, a
// duration that is negative or not a number, or an entry the style does not
// take: Table 162 lists which style each of Dm, M and Di is for, and which
// values of Di each admits.
func (p *Page) SetTransition(t Transition) error {
	if err := t.validate(); err != nil {
		return err
	}
	p.transition = &t
	return nil
}

// SetDisplayDuration sets the page's /Dur: the longest the page is shown, in
// seconds, before a presentation advances to the next page by itself (PDF
// 32000-1 12.4.4.1). 0 removes it, and the page does not advance.
func (p *Page) SetDisplayDuration(seconds float64) error {
	if seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return fmt.Errorf("pdfkit: display duration %v is not a duration", seconds)
	}
	p.dur = seconds
	return nil
}

func (t Transition) validate() error {
	var di []int // the values of Di the style admits; nil: it takes no Di
	switch t.Style {
	case TransitionSplit, TransitionBlinds, TransitionBox, TransitionDissolve, TransitionReplace, TransitionFade:
	case TransitionWipe:
		di = []int{0, 90, 180, 270}
	case TransitionGlitter:
		di = []int{0, 270, 315}
	case TransitionPush, TransitionCover, TransitionUncover:
		di = []int{0, 270}
	default:
		return fmt.Errorf("pdfkit: transition style %q is not one this package writes", t.Style)
	}
	if t.Duration < 0 || math.IsNaN(t.Duration) || math.IsInf(t.Duration, 0) {
		return fmt.Errorf("pdfkit: transition duration %v is not a duration", t.Duration)
	}
	if t.Vertical && t.Style != TransitionSplit && t.Style != TransitionBlinds {
		return fmt.Errorf("pdfkit: /Dm is for Split and Blinds, not %s", t.Style)
	}
	if t.Outward && t.Style != TransitionSplit && t.Style != TransitionBox {
		return fmt.Errorf("pdfkit: /M is for Split and Box, not %s", t.Style)
	}
	if t.Direction != 0 {
		ok := false
		for _, d := range di {
			ok = ok || d == t.Direction
		}
		if !ok {
			return fmt.Errorf("pdfkit: /Di %d is not one %s takes", t.Direction, t.Style)
		}
	}
	return nil
}

// dict is the transition dictionary, with only what is not a default.
func (t Transition) dict() *pdfDict {
	d := newDict()
	d.set("Type", pdfName("Trans"))
	d.set("S", pdfName(t.Style))
	if t.Duration > 0 {
		d.set("D", pdfReal(t.Duration))
	}
	if t.Vertical {
		d.set("Dm", pdfName("V"))
	}
	if t.Outward {
		d.set("M", pdfName("O"))
	}
	if t.Direction != 0 {
		d.set("Di", pdfInt(t.Direction))
	}
	return d
}
