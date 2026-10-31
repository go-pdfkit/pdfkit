// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"errors"
	"math"
	"strconv"
)

// Form is a form XObject (ISO 32000-1:2008, 8.10): a content stream drawn
// once and painted any number of times, on any page or inside other forms,
// at the cost of one Do each. A logo, a glyph drawn as paths, an emoji or a
// formula used thirty times is one stream in the file, not thirty.
//
// A Form is drawn like a page: it embeds a *Page, so every drawing method
// (paths, colours, alpha, shadings, images, text, other forms) works on it,
// in its own coordinate system, form space, whose visible part is the
// bounding box given to NewForm. What belongs to a page and not to a drawing
// is not available inside a form: BeginTag writes nothing (a form's content
// is part of the tagged sequence its Do is painted in and shall not contain
// sequences of its own, 14.7.4.2, the one way a form painted many times can
// be in the structure), and
// links, notes and named destinations added to it are never written.
type Form struct {
	*Page
	bbox Rect
	name string
}

// NewForm returns an empty form whose form space shows bbox (its BBox
// entry, 8.10.2 Table 95). Draw into it, then paint it with Page.DrawForm.
func (d *Document) NewForm(bbox Rect) *Form {
	f := &Form{bbox: bbox, name: "Fm" + strconv.Itoa(len(d.forms))}
	f.Page = &Page{
		doc:        d,
		index:      -1,
		width:      bbox.Width,
		height:     bbox.Height,
		usedFonts:  map[*Font]bool{},
		usedImages: map[*imageXObject]bool{},
		lineWidth:  1,
		form:       f,
	}
	d.forms = append(d.forms, f)
	return f
}

// BBox returns the form's bounding box in form space.
func (f *Form) BBox() Rect { return f.bbox }

// ErrFormCycle is returned by DrawForm when the form would paint itself,
// directly or through the forms it paints: a reader would recurse forever.
var ErrFormCycle = errors.New("pdfkit: a form cannot paint itself")

// ErrFormDocument is returned by DrawForm for a form of another document.
var ErrFormDocument = errors.New("pdfkit: the form belongs to another document")

// DrawForm paints f so that its bounding box fills r, in the current user
// space: q, a cm mapping the box onto r, Do, Q (8.10.1). A form with an
// empty box paints nothing.
func (p *Page) DrawForm(f *Form, r Rect) error {
	if f == nil || f.bbox.Width == 0 || f.bbox.Height == 0 {
		return nil
	}
	sx, sy := r.Width/f.bbox.Width, r.Height/f.bbox.Height
	return p.PaintForm(f, sx, 0, 0, sy, r.X-f.bbox.X*sx, r.Y-f.bbox.Y*sy)
}

// PaintForm paints f under the matrix [a b c d e f] concatenated onto the
// current transformation matrix, the general form of DrawForm.
func (p *Page) PaintForm(f *Form, a, b, c, d, e, ff float64) error {
	if f == nil {
		return nil
	}
	if f.doc != p.doc {
		return ErrFormDocument
	}
	if p.form != nil && f.reaches(p.form) {
		return ErrFormCycle
	}
	for _, v := range [...]float64{a, b, c, d, e, ff} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
	}
	if p.usedForms == nil {
		p.usedForms = map[*Form]bool{}
	}
	p.usedForms[f] = true
	p.Save()
	if a != 1 || b != 0 || c != 0 || d != 1 || e != 0 || ff != 0 {
		p.Transform(a, b, c, d, e, ff)
	}
	p.op("/"+f.name, "Do")
	p.Restore()
	return nil
}

// reaches reports whether f is g or paints g, through any depth of forms.
func (f *Form) reaches(g *Form) bool {
	seen := map[*Form]bool{}
	stack := []*Form{f}
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if x == g {
			return true
		}
		if seen[x] {
			continue
		}
		seen[x] = true
		for y := range x.usedForms {
			stack = append(stack, y)
		}
	}
	return false
}

// buildForms reserves an object per form, then writes each form's stream,
// so that a form may paint forms created before or after it.
func (d *Document) buildForms(bd *builder, fontRefs map[*Font]objRef, imageRefs map[*imageXObject]objRef) map[*Form]objRef {
	refs := make(map[*Form]objRef, len(d.forms))
	for _, f := range d.forms {
		refs[f] = bd.reserve()
	}
	for _, f := range d.forms {
		dict := newDict()
		dict.set("Type", pdfName("XObject"))
		dict.set("Subtype", pdfName("Form"))
		b := f.bbox
		dict.set("BBox", pdfArray{pdfReal(b.X), pdfReal(b.Y), pdfReal(b.X + b.Width), pdfReal(b.Y + b.Height)})
		dict.set("Resources", f.resources(fontRefs, imageRefs, refs))
		data := d.maybeFlate(dict, f.finishContent())
		bd.put(refs[f], &pdfStream{dict: dict, data: data})
	}
	return refs
}
