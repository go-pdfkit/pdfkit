// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"sort"
	"strconv"
	"strings"
)

// Tagged PDF: the document's logical structure (ISO 32000-1:2008, 14.7 and
// 14.8), which screen readers, reflow and PDF/UA (ISO 14289-1) read.
//
// The structure is a tree of elements (StructElem) under the document's
// root element, built in reading order. Content is tied to an element by
// drawing it inside a marked-content sequence opened with Page.BeginTag:
// the sequence gets a marked-content identifier (MCID) on its page, the
// element lists it among its kids, and the page's entry in the parent tree
// maps the MCID back to the element (14.7.4.4). Content that is not part of
// the author's matter, such as backgrounds, page numbers and ornaments, is
// drawn inside Page.BeginArtifact instead (14.8.2.2). A tagged link
// annotation is an object reference (OBJR) kid of its Link element.
//
// The children of an element, elements and content alike, are kept in the
// order they were added, which is the reading order. An element with no
// content anywhere below it is left out of the file.

// Standard structure types (ISO 32000-1:2008, 14.8.4). Any name may be used;
// these are the ones a reader knows without a role map.
const (
	StructDocument   = "Document"
	StructPart       = "Part"
	StructSect       = "Sect"
	StructDiv        = "Div"
	StructP          = "P"
	StructH1         = "H1"
	StructH2         = "H2"
	StructH3         = "H3"
	StructH4         = "H4"
	StructH5         = "H5"
	StructH6         = "H6"
	StructL          = "L"
	StructLI         = "LI"
	StructLbl        = "Lbl"
	StructLBody      = "LBody"
	StructTable      = "Table"
	StructTR         = "TR"
	StructTH         = "TH"
	StructTD         = "TD"
	StructSpan       = "Span"
	StructLink       = "Link"
	StructAnnot      = "Annot"
	StructFigure     = "Figure"
	StructFormula    = "Formula"
	StructCode       = "Code"
	StructQuote      = "Quote"
	StructBlockQuote = "BlockQuote"
	StructNote       = "Note"
	StructCaption    = "Caption"
)

// StructElem is an element of the logical structure (a StructElem
// dictionary, ISO 32000-1:2008, Table 323). Make one with Document.Structure
// and Add.
type StructElem struct {
	doc    *Document
	parent *StructElem
	// Type is the structure type, the /S entry: one of the Struct constants
	// or another name.
	Type string
	// Alt is an alternate description, for a figure or a formula above all
	// (14.9.3); ActualText is an exact replacement for the element's text
	// (14.9.4); Lang its natural language, a language tag such as "fr"
	// (14.9.2); Title a human-readable title (/T).
	Alt, ActualText, Lang, Title string
	// Attributes are the element's standard structure attributes
	// (14.8.5): a table header's Scope, a cell's RowSpan, a list's
	// ListNumbering… written as one attribute object per owner.
	Attributes []Attribute
	kids       []structKid
	ref        objRef // set while writing
}

// Attribute is one standard structure attribute (ISO 32000-1:2008, 14.8.5):
// its owner ("Table", "List", "Layout", "PrintField"…), its key, and its
// value: a Name, an int, a float64 or a string (a text string).
type Attribute struct {
	Owner, Key string
	Value      any
}

// Name is an attribute value written as a PDF name, such as Column for a
// table header's Scope.
type Name string

// attributes is e's /A: one attribute object per owner, in the order the
// owners first appear; nil without attributes. A value of an unknown type is
// left out.
func (e *StructElem) attributes() pdfValue {
	var dicts []*pdfDict
	owners := map[string]*pdfDict{}
	for _, a := range e.Attributes {
		var v pdfValue
		switch x := a.Value.(type) {
		case Name:
			v = pdfName(x)
		case int:
			v = pdfInt(x)
		case float64:
			v = pdfReal(x)
		case string:
			v = pdfTextString(x)
		default:
			continue
		}
		d := owners[a.Owner]
		if d == nil {
			d = newDict()
			d.set("O", pdfName(a.Owner))
			owners[a.Owner] = d
			dicts = append(dicts, d)
		}
		d.set(a.Key, v)
	}
	switch len(dicts) {
	case 0:
		return nil
	case 1:
		return dicts[0]
	}
	arr := make(pdfArray, len(dicts))
	for i, d := range dicts {
		arr[i] = d
	}
	return arr
}

// structKid is one child of an element: another element, a marked-content
// sequence (page and MCID), or an annotation (page and its index among the
// page's links).
type structKid struct {
	elem  *StructElem
	page  int
	mcid  int
	annot int // >= 0 for an object reference to the page's links[annot]
}

// Structure returns the document's root element, a Document element, making
// it on the first call: a document that calls it is a tagged PDF. Its
// children are the top-level elements.
func (d *Document) Structure() *StructElem {
	if d.structRoot == nil {
		d.structRoot = &StructElem{doc: d, Type: StructDocument}
	}
	return d.structRoot
}

// Add appends a child element of type typ and returns it.
func (e *StructElem) Add(typ string) *StructElem {
	c := &StructElem{doc: e.doc, parent: e, Type: typ}
	e.kids = append(e.kids, structKid{elem: c, annot: -1})
	return c
}

// Parent returns the element's parent, nil for the root.
func (e *StructElem) Parent() *StructElem { return e.parent }

// BeginTag starts a marked-content sequence of e's content: what is drawn
// until the matching EndMarkedContent belongs to e. The sequence is tagged
// with e's type and a fresh MCID, and e lists it as its next child, so
// calling BeginTag in reading order keeps the structure in reading order.
// Sequences may not nest (14.7.4.2 forbids one structure content item in
// another): a BeginTag while one is open closes the open one first.
func (p *Page) BeginTag(e *StructElem) {
	if e == nil || e.doc != p.doc || p.form != nil {
		return
	}
	p.closeTag()
	mcid := len(p.mcids)
	p.mcids = append(p.mcids, e)
	e.kids = append(e.kids, structKid{page: p.index, mcid: mcid, annot: -1})
	var b bytes.Buffer
	pdfName(e.Type).encodePDF(&b)
	b.WriteString(" <</MCID " + strconv.Itoa(mcid) + ">>")
	p.op(b.String(), "BDC")
	p.marked++
	p.tagOpen = true
	p.tagDepth = p.marked
}

// closeTag ends the open tagged sequence, with any span opened inside it.
func (p *Page) closeTag() {
	for p.tagOpen && p.marked >= p.tagDepth {
		p.EndMarkedContent()
	}
}

// Artifact describes content that is not part of the document's matter
// (ISO 32000-1:2008, 14.8.2.2, Table 330): a page's background, a page
// number, a rule, an ornament.
type Artifact struct {
	// Type is "Pagination", "Layout", "Page" or "Background"; empty writes
	// a plain /Artifact BMC.
	Type string
	// Subtype refines a Pagination artifact: "Header", "Footer" or
	// "Watermark".
	Subtype string
	// BBox, when not nil, is the artifact's extent in the page's user space
	// (required for a Background artifact).
	BBox *Rect
}

// BeginArtifact starts a marked-content sequence of artifact content; end it
// with EndMarkedContent. A screen reader skips it, and a tagged document must
// mark every drawing that is not tagged this way (PDF/UA). A BeginArtifact
// while a tagged sequence is open closes that sequence first: an artifact
// may not sit inside tagged content.
func (p *Page) BeginArtifact(a Artifact) {
	p.closeTag()
	if a.Type == "" && a.BBox == nil {
		p.op("/Artifact", "BMC")
		p.marked++
		return
	}
	d := newDict()
	if a.Type != "" {
		d.set("Type", pdfName(a.Type))
	}
	if a.Subtype != "" {
		d.set("Subtype", pdfName(a.Subtype))
	}
	if a.BBox != nil {
		r := *a.BBox
		d.set("BBox", pdfArray{pdfReal(r.X), pdfReal(r.Y), pdfReal(r.X + r.Width), pdfReal(r.Y + r.Height)})
	}
	var b bytes.Buffer
	b.WriteString("/Artifact ")
	d.encodePDF(&b)
	p.op(b.String(), "BDC")
	p.marked++
}

// AddTaggedLink is AddLink for a tagged document: the annotation is a kid of
// the Link element e, after whatever e already holds, and contents is its
// alternate description (/Contents), which PDF/UA asks of every link.
func (p *Page) AddTaggedLink(e *StructElem, rect Rect, uri, contents string) {
	p.addTaggedLink(e, linkAnnot{rect: rect, uri: uri, contents: contents})
}

// AddTaggedNamedLink is AddNamedLink for a tagged document; see
// AddTaggedLink.
func (p *Page) AddTaggedNamedLink(e *StructElem, rect Rect, dest, contents string) {
	p.addTaggedLink(e, linkAnnot{rect: rect, dest: dest, contents: contents})
}

func (p *Page) addTaggedLink(e *StructElem, l linkAnnot) {
	if p.form != nil {
		return // a form has no annotations, nor a place in the structure
	}
	if e != nil && e.doc == p.doc {
		l.elem = e
		e.kids = append(e.kids, structKid{page: p.index, annot: len(p.links)})
	}
	p.links = append(p.links, l)
}

// tagged reports whether the document carries a structure tree.
func (d *Document) tagged() bool { return d.structRoot != nil }

// hasContent reports whether e or an element below it holds content, and
// so is written.
func (e *StructElem) hasContent() bool {
	for _, k := range e.kids {
		if k.elem == nil || k.elem.hasContent() {
			return true
		}
	}
	return false
}

// structWriter carries what writing the structure tree needs from Write.
type structWriter struct {
	bd       *builder
	pageRefs []objRef
	// annotRefs[page][i] is the object of the page's links[i].
	annotRefs [][]objRef
	// annotKeys[page][i] is that annotation's parent-tree key.
	annotKeys [][]int
}

// reserveStruct numbers every element that is written, depth first, so
// parents and children can refer to one another.
func reserveStruct(bd *builder, e *StructElem) {
	e.ref = bd.reserve()
	for _, k := range e.kids {
		if k.elem != nil && k.elem.hasContent() {
			reserveStruct(bd, k.elem)
		}
	}
}

// fillStruct writes e's dictionary, then its children's.
func (w *structWriter) fillStruct(e *StructElem, parent objRef) {
	d := newDict()
	d.set("Type", pdfName("StructElem"))
	d.set("S", pdfName(e.Type))
	d.set("P", parent)
	// The page of the first marked content is the element's /Pg: its MCIDs
	// on that page are bare integers, others marked-content references.
	pg := -1
	for _, k := range e.kids {
		if k.elem == nil {
			pg = k.page
			break
		}
	}
	if pg >= 0 {
		d.set("Pg", w.pageRefs[pg])
	}
	var kids pdfArray
	for _, k := range e.kids {
		switch {
		case k.elem != nil:
			if k.elem.hasContent() {
				kids = append(kids, k.elem.ref)
			}
		case k.annot >= 0:
			o := newDict()
			o.set("Type", pdfName("OBJR"))
			o.set("Pg", w.pageRefs[k.page])
			o.set("Obj", w.annotRefs[k.page][k.annot])
			kids = append(kids, o)
		case k.page == pg:
			kids = append(kids, pdfInt(k.mcid))
		default:
			m := newDict()
			m.set("Type", pdfName("MCR"))
			m.set("Pg", w.pageRefs[k.page])
			m.set("MCID", pdfInt(k.mcid))
			kids = append(kids, m)
		}
	}
	if len(kids) == 1 {
		d.set("K", kids[0])
	} else {
		d.set("K", kids)
	}
	if e.Title != "" {
		d.set("T", pdfTextString(e.Title))
	}
	if e.Lang != "" {
		d.set("Lang", pdfTextString(e.Lang))
	}
	if e.Alt != "" {
		d.set("Alt", pdfTextString(e.Alt))
	}
	if e.ActualText != "" {
		d.set("ActualText", pdfTextString(e.ActualText))
	}
	if a := e.attributes(); a != nil {
		d.set("A", a)
	}
	w.bd.put(e.ref, d)
	for _, k := range e.kids {
		if k.elem != nil && k.elem.hasContent() {
			w.fillStruct(k.elem, e.ref)
		}
	}
}

// buildStructTree writes the structure tree root with its parent tree and
// returns its reference. Page i's marked content is under parent-tree key i
// (its /StructParents); each tagged annotation has a key after the pages'.
func (d *Document) buildStructTree(w *structWriter) objRef {
	// A note (text annotation) gets an Annot element of its own at the end
	// of the document, made for this write only: Write must not change the
	// document, which may be written again.
	saved := d.structRoot.kids
	defer func() { d.structRoot.kids = saved }()
	d.structRoot.kids = append([]structKid(nil), saved...)
	elemOf := make([][]*StructElem, len(d.pages))
	for pi, p := range d.pages {
		elemOf[pi] = make([]*StructElem, len(p.links)+len(p.notes))
		for i, l := range p.links {
			elemOf[pi][i] = l.elem
		}
		for j := range p.notes {
			e := &StructElem{doc: d, parent: d.structRoot, Type: StructAnnot}
			e.kids = []structKid{{page: pi, annot: len(p.links) + j}}
			d.structRoot.kids = append(d.structRoot.kids, structKid{elem: e, annot: -1})
			elemOf[pi][len(p.links)+j] = e
		}
	}

	root := w.bd.reserve()
	reserveStruct(w.bd, d.structRoot)
	w.fillStruct(d.structRoot, root)

	var nums pdfArray
	for i, p := range d.pages {
		if len(p.mcids) == 0 {
			continue
		}
		arr := make(pdfArray, len(p.mcids))
		for j, e := range p.mcids {
			arr[j] = e.ref
		}
		nums = append(nums, pdfInt(i), w.bd.add(arr))
	}
	next := len(d.pages)
	type annotEntry struct {
		key int
		ref objRef
	}
	var annots []annotEntry
	for pi, keys := range w.annotKeys {
		for ai, k := range keys {
			if k >= 0 {
				annots = append(annots, annotEntry{k, elemOf[pi][ai].ref})
				next = max(next, k+1)
			}
		}
	}
	sort.Slice(annots, func(i, j int) bool { return annots[i].key < annots[j].key })
	for _, a := range annots {
		nums = append(nums, pdfInt(a.key), a.ref)
	}
	pt := newDict()
	pt.set("Nums", nums)

	r := newDict()
	r.set("Type", pdfName("StructTreeRoot"))
	r.set("K", d.structRoot.ref)
	r.set("ParentTree", w.bd.add(pt))
	r.set("ParentTreeNextKey", pdfInt(next))
	w.bd.put(root, r)
	return root
}

// xmpMetadata is the document's XMP metadata packet: its title (dc:title),
// producer, and, for PDF/UA, the identification schema ISO 14289-1, 5
// requires (pdfuaid:part 1).
func (d *Document) xmpMetadata() []byte {
	var b strings.Builder
	b.WriteString("<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n")
	b.WriteString(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">`)
	b.WriteString(`<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">`)
	if d.opts.Title != "" {
		b.WriteString(`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">` + xmlEscape(d.opts.Title) + `</rdf:li></rdf:Alt></dc:title>`)
	}
	if d.opts.Author != "" {
		b.WriteString(`<dc:creator><rdf:Seq><rdf:li>` + xmlEscape(d.opts.Author) + `</rdf:li></rdf:Seq></dc:creator>`)
	}
	if d.opts.Subject != "" {
		b.WriteString(`<dc:description><rdf:Alt><rdf:li xml:lang="x-default">` + xmlEscape(d.opts.Subject) + `</rdf:li></rdf:Alt></dc:description>`)
	}
	b.WriteString(`</rdf:Description>`)
	b.WriteString(`<rdf:Description rdf:about="" xmlns:pdf="http://ns.adobe.com/pdf/1.3/"><pdf:Producer>` + xmlEscape(d.producer()) + `</pdf:Producer>`)
	if d.opts.Keywords != "" {
		b.WriteString(`<pdf:Keywords>` + xmlEscape(d.opts.Keywords) + `</pdf:Keywords>`)
	}
	b.WriteString(`</rdf:Description>`)
	if d.opts.PDFUA {
		b.WriteString(`<rdf:Description rdf:about="" xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/"><pdfuaid:part>1</pdfuaid:part></rdf:Description>`)
	}
	b.WriteString("</rdf:RDF></x:xmpmeta>\n<?xpacket end=\"w\"?>")
	return []byte(b.String())
}

func xmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
				continue // not allowed in XML 1.0
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}
