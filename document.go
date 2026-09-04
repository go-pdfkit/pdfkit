// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"
)

// Options configures a Document. The zero value is valid and yields a
// deterministic, uncompressed document with no timestamps.
type Options struct {
	// Title, Author, Subject and Keywords populate the document information
	// dictionary. Empty values are omitted. Subject is a one-line description of
	// the document; Keywords is a list of search terms (conventionally
	// comma-separated). Each is written as a PDF text string, so a non-ASCII value
	// is encoded UTF-16BE.
	Title    string
	Author   string
	Subject  string
	Keywords string

	// Producer is the /Producer string in the information dictionary. When
	// empty it defaults to DefaultProducer.
	Producer string

	// Now, when non-nil, is called once at Write time to stamp /CreationDate
	// and /ModDate. When nil no dates are written, keeping output reproducible;
	// tests should leave it nil.
	Now func() time.Time

	// Compress enables FlateDecode compression of content and embedded-font
	// streams. Image streams choose their own filter regardless.
	Compress bool

	// ID, when both entries are non-nil, is used verbatim as the trailer /ID
	// pair. When nil the ID is derived deterministically from the document
	// body, so identical documents get identical IDs without a clock.
	ID [2][]byte
}

// DefaultProducer is the /Producer value used when Options.Producer is empty.
const DefaultProducer = "go-pdfkit/pdfkit"

// Document is a PDF document under construction. Build it with New, append
// pages with AddPage, then serialise with Write. It is not safe for concurrent
// use.
type Document struct {
	opts    Options
	pages   []*Page
	fonts   []*Font            // registration order; index drives the /F<i> name
	fontIx  map[*Font]int      // font -> registration index
	use     map[*Font]*fontUse // per-document glyph usage, keyed by font
	images  []*imageXObject    // registration order; index drives the /Im<i> name
	outline []outlineItem      // document outline (bookmarks), in document order

	// imgCache maps an image's content address (see imageKey) to the XObject
	// already registered for it, so a bitmap drawn many times is embedded
	// once. It is consulted, never iterated: the emitted order and the /Im<i>
	// numbering come from the images slice, so output stays deterministic.
	imgCache map[string]*imageXObject
}

// outlineItem is one entry in the document outline (the viewer's bookmark tree):
// its title, its nesting level (1 = top level, higher = deeper), and the page it
// jumps to.
type outlineItem struct {
	title     string
	level     int
	pageIndex int
}

// AddOutlineItem appends a bookmark to the document outline: title at nesting level
// (1 = top level; a higher level nests under the most recent shallower item),
// jumping to page pageIndex (0-based). Out-of-range pages are ignored. Items build
// the /Outlines tree a PDF viewer shows as its navigation sidebar.
func (d *Document) AddOutlineItem(title string, level, pageIndex int) {
	if level < 1 || pageIndex < 0 || pageIndex >= len(d.pages) {
		return
	}
	d.outline = append(d.outline, outlineItem{title: title, level: level, pageIndex: pageIndex})
}

// New returns a new, empty Document configured by opts.
func New(opts Options) *Document {
	return &Document{
		opts:     opts,
		fontIx:   map[*Font]int{},
		use:      map[*Font]*fontUse{},
		imgCache: map[string]*imageXObject{},
	}
}

// AddPage appends a page of the given size (in points; see PageSize helpers and
// the standard sizes such as A4) and returns it for drawing.
func (d *Document) AddPage(size PageSize) *Page {
	p := &Page{
		doc:        d,
		width:      size.Width,
		height:     size.Height,
		usedFonts:  map[*Font]bool{},
		usedImages: map[*imageXObject]bool{},
		lineWidth:  1,
	}
	d.pages = append(d.pages, p)
	return p
}

// registerFont ensures f has a document resource slot and usage record,
// returning its /F<i> resource name.
func (d *Document) registerFont(f *Font) string {
	if _, ok := d.fontIx[f]; !ok {
		d.fontIx[f] = len(d.fonts)
		d.fonts = append(d.fonts, f)
		d.use[f] = newFontUse()
	}
	return "F" + strconv.Itoa(d.fontIx[f])
}

// registerImage appends an image XObject and returns its /Im<i> resource name.
func (d *Document) registerImage(x *imageXObject) string {
	name := "Im" + strconv.Itoa(len(d.images))
	d.images = append(d.images, x)
	return name
}

// imageFor returns the XObject registered for the content address key, calling
// build only the first time that content is seen. Every later draw of a
// pixel-identical bitmap — on this page or any other, since the cache belongs
// to the document — reuses the same object and the same /Im<i> name, so it
// costs one stream in the file instead of one per placement.
func (d *Document) imageFor(key string, build func() *imageXObject) *imageXObject {
	if x, ok := d.imgCache[key]; ok {
		return x
	}
	x := build()
	x.name = d.registerImage(x)
	d.imgCache[key] = x
	return x
}

// builder assembles the flat list of indirect objects and assigns their
// numbers. Object number n lives at objs[n-1].
type builder struct {
	objs []pdfValue
}

// reserve allocates the next object number without a body; fill it with put.
func (bd *builder) reserve() objRef {
	bd.objs = append(bd.objs, nil)
	return objRef(len(bd.objs))
}

// put stores v as the body of a previously reserved object.
func (bd *builder) put(ref objRef, v pdfValue) { bd.objs[ref-1] = v }

// add reserves and fills an object in one step, returning its reference.
func (bd *builder) add(v pdfValue) objRef {
	r := bd.reserve()
	bd.put(r, v)
	return r
}

// Write serialises the document to w as a complete PDF 1.7 file. It returns the
// first write error encountered. Calling Write does not consume the document;
// it may be written more than once.
func (d *Document) Write(w io.Writer) error {
	if len(d.pages) == 0 {
		return fmt.Errorf("pdfkit: document has no pages")
	}
	bd := &builder{}

	catalog := bd.reserve()
	pagesRef := bd.reserve()

	// Reserve a slot per page node up front so the /Kids array can reference
	// them before their bodies exist.
	pageRefs := make([]objRef, len(d.pages))
	for i := range d.pages {
		pageRefs[i] = bd.reserve()
	}

	// All drawing has already happened, so glyph and image usage is final:
	// embed the fonts and images first, then reference them from the pages.
	fontRefs := make(map[*Font]objRef, len(d.fonts))
	for _, f := range d.fonts {
		fontRefs[f] = d.buildFont(bd, f)
	}
	imageRefs := make(map[*imageXObject]objRef, len(d.images))
	for _, x := range d.images {
		imageRefs[x] = d.buildImage(bd, x)
	}

	var dests []destEntry
	for i, p := range d.pages {
		content := p.finishContent()
		cdict := newDict()
		data := d.maybeFlate(cdict, content)
		cstream := bd.add(&pdfStream{dict: cdict, data: data})

		node := newDict()
		node.set("Type", pdfName("Page"))
		node.set("Parent", pagesRef)
		node.set("MediaBox", pdfArray{pdfReal(0), pdfReal(0), pdfReal(p.width), pdfReal(p.height)})
		node.set("Contents", cstream)
		node.set("Resources", p.resources(fontRefs, imageRefs))
		if len(p.links) > 0 {
			node.set("Annots", d.buildLinkAnnots(bd, p.links))
		}
		bd.put(pageRefs[i], node)
		for _, dt := range p.dests {
			dests = append(dests, destEntry{name: dt.name, page: pageRefs[i], y: dt.y})
		}
	}

	kids := make(pdfArray, len(pageRefs))
	for i, r := range pageRefs {
		kids[i] = r
	}
	pagesDict := newDict()
	pagesDict.set("Type", pdfName("Pages"))
	pagesDict.set("Kids", kids)
	pagesDict.set("Count", pdfInt(len(pageRefs)))
	bd.put(pagesRef, pagesDict)

	catDict := newDict()
	catDict.set("Type", pdfName("Catalog"))
	catDict.set("Pages", pagesRef)
	if names := d.buildDestNames(bd, dests); names != 0 {
		catDict.set("Names", names)
	}
	if outlines := d.buildOutlines(bd, pageRefs); outlines != 0 {
		catDict.set("Outlines", outlines)
	}
	bd.put(catalog, catDict)

	var info objRef
	hasInfo := d.opts.Title != "" || d.opts.Author != "" || d.opts.Subject != "" ||
		d.opts.Keywords != "" || d.producer() != "" || d.opts.Now != nil
	if hasInfo {
		info = bd.add(d.infoDict())
	}

	return d.emit(w, bd, catalog, info, hasInfo)
}

// buildLinkAnnots emits one /Link annotation object per link and returns the
// /Annots array referencing them. A link is a borderless rectangle whose /A is a
// URI action; /Rect is [llx lly urx ury] in the page's default user space.
func (d *Document) buildLinkAnnots(bd *builder, links []linkAnnot) pdfArray {
	annots := make(pdfArray, len(links))
	for i, ln := range links {
		action := newDict()
		if ln.dest != "" {
			// An internal jump: GoTo the named destination, resolved through the
			// document's /Dests name tree.
			action.set("S", pdfName("GoTo"))
			action.set("D", pdfString(ln.dest))
		} else {
			action.set("S", pdfName("URI"))
			action.set("URI", pdfString(ln.uri))
		}

		a := newDict()
		a.set("Type", pdfName("Annot"))
		a.set("Subtype", pdfName("Link"))
		a.set("Rect", pdfArray{
			pdfReal(ln.rect.X), pdfReal(ln.rect.Y),
			pdfReal(ln.rect.X + ln.rect.Width), pdfReal(ln.rect.Y + ln.rect.Height),
		})
		a.set("Border", pdfArray{pdfInt(0), pdfInt(0), pdfInt(0)})
		a.set("A", action)
		annots[i] = bd.add(a)
	}
	return annots
}

// destEntry is a named destination resolved to its page during Write.
type destEntry struct {
	name string
	page objRef
	y    float64
}

// buildDestNames builds the /Names dictionary carrying the /Dests name tree that an
// internal GoTo link resolves against. Each name maps to [page /FitH y] — the viewer
// scrolls so y is at the top and the page width fits. Names are unique (first
// definition wins) and sorted, as a PDF name tree requires. Returns 0 (no object)
// when there are no destinations.
func (d *Document) buildDestNames(bd *builder, dests []destEntry) objRef {
	if len(dests) == 0 {
		return 0
	}
	byName := make(map[string]destEntry, len(dests))
	order := make([]string, 0, len(dests))
	for _, dt := range dests {
		if _, ok := byName[dt.name]; !ok {
			byName[dt.name] = dt
			order = append(order, dt.name)
		}
	}
	sort.Strings(order)

	pairs := make(pdfArray, 0, 2*len(order))
	for _, name := range order {
		dt := byName[name]
		pairs = append(pairs, pdfString(name), pdfArray{dt.page, pdfName("FitH"), pdfReal(dt.y)})
	}
	destTree := newDict()
	destTree.set("Names", pairs)

	names := newDict()
	names.set("Dests", bd.add(destTree))
	return bd.add(names)
}

// outlineNode is the resolved tree form of the flat outline items during Write.
type outlineNode struct {
	item     outlineItem
	ref      objRef
	children []*outlineNode
}

// buildOutlines builds the /Outlines tree a viewer shows as its bookmark sidebar and
// returns its root object (0 when there are no items). Items nest by level — an item
// deeper than the one before becomes its child — and each is a /Title with a /Dest to
// [page /Fit] plus the /Parent //Prev //Next //First //Last //Count links a PDF
// outline tree requires.
func (d *Document) buildOutlines(bd *builder, pageRefs []objRef) objRef {
	if len(d.outline) == 0 {
		return 0
	}
	var roots []*outlineNode
	var stack []*outlineNode
	for _, it := range d.outline {
		n := &outlineNode{item: it}
		for len(stack) > 0 && stack[len(stack)-1].item.level >= it.level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			roots = append(roots, n)
		} else {
			p := stack[len(stack)-1]
			p.children = append(p.children, n)
		}
		stack = append(stack, n)
	}

	root := bd.reserve()
	reserveOutline(bd, roots)

	rd := newDict()
	rd.set("Type", pdfName("Outlines"))
	rd.set("First", roots[0].ref)
	rd.set("Last", roots[len(roots)-1].ref)
	rd.set("Count", pdfInt(outlineCount(roots)))
	bd.put(root, rd)

	d.fillOutline(bd, pageRefs, roots, root)
	return root
}

// reserveOutline assigns an object number to every node, depth first, so sibling and
// parent references exist before the bodies are written.
func reserveOutline(bd *builder, ns []*outlineNode) {
	for _, n := range ns {
		n.ref = bd.reserve()
		reserveOutline(bd, n.children)
	}
}

// outlineCount is the number of descendants across all levels of ns — the /Count of
// an open outline node.
func outlineCount(ns []*outlineNode) int {
	c := 0
	for _, n := range ns {
		c += 1 + outlineCount(n.children)
	}
	return c
}

// fillOutline writes each node's item dictionary, linking its parent, siblings and
// children.
func (d *Document) fillOutline(bd *builder, pageRefs []objRef, ns []*outlineNode, parent objRef) {
	for i, n := range ns {
		od := newDict()
		od.set("Title", pdfTextString(n.item.title))
		od.set("Parent", parent)
		od.set("Dest", pdfArray{pageRefs[n.item.pageIndex], pdfName("Fit")})
		if i > 0 {
			od.set("Prev", ns[i-1].ref)
		}
		if i < len(ns)-1 {
			od.set("Next", ns[i+1].ref)
		}
		if len(n.children) > 0 {
			od.set("First", n.children[0].ref)
			od.set("Last", n.children[len(n.children)-1].ref)
			od.set("Count", pdfInt(outlineCount(n.children)))
		}
		bd.put(n.ref, od)
		d.fillOutline(bd, pageRefs, n.children, n.ref)
	}
}

// producer returns the effective /Producer string.
func (d *Document) producer() string {
	if d.opts.Producer != "" {
		return d.opts.Producer
	}
	return DefaultProducer
}

// infoDict builds the document information dictionary.
func (d *Document) infoDict() *pdfDict {
	info := newDict()
	if d.opts.Title != "" {
		info.set("Title", pdfTextString(d.opts.Title))
	}
	if d.opts.Author != "" {
		info.set("Author", pdfTextString(d.opts.Author))
	}
	if d.opts.Subject != "" {
		info.set("Subject", pdfTextString(d.opts.Subject))
	}
	if d.opts.Keywords != "" {
		info.set("Keywords", pdfTextString(d.opts.Keywords))
	}
	if p := d.producer(); p != "" {
		info.set("Producer", pdfString(p))
	}
	if d.opts.Now != nil {
		date := pdfString(formatPDFDate(d.opts.Now()))
		info.set("CreationDate", date)
		info.set("ModDate", date)
	}
	return info
}

// emit writes the object bodies, the cross-reference table and the trailer.
func (d *Document) emit(w io.Writer, bd *builder, catalog, info objRef, hasInfo bool) error {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	// A comment with high bytes marks the file as binary for transfer tools.
	buf.WriteString("%\xe2\xe3\xcf\xd3\n")

	offsets := make([]int, len(bd.objs))
	for i, o := range bd.objs {
		offsets[i] = buf.Len()
		buf.WriteString(strconv.Itoa(i + 1))
		buf.WriteString(" 0 obj\n")
		o.encodePDF(&buf)
		buf.WriteString("\nendobj\n")
	}

	xrefOff := buf.Len()
	n := len(bd.objs) + 1
	buf.WriteString("xref\n")
	buf.WriteString("0 " + strconv.Itoa(n) + "\n")
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}

	trailer := newDict()
	trailer.set("Size", pdfInt(n))
	trailer.set("Root", catalog)
	if hasInfo {
		trailer.set("Info", info)
	}
	id := d.documentID(buf.Bytes())
	trailer.set("ID", pdfArray{pdfHexString(id[0]), pdfHexString(id[1])})

	buf.WriteString("trailer\n")
	trailer.encodePDF(&buf)
	buf.WriteString("\nstartxref\n")
	buf.WriteString(strconv.Itoa(xrefOff))
	buf.WriteString("\n%%EOF\n")

	_, err := w.Write(buf.Bytes())
	return err
}

// documentID returns the trailer /ID pair. A caller-supplied ID is used as-is;
// otherwise it is derived from the document body so equal documents get equal
// IDs without consulting a clock.
func (d *Document) documentID(body []byte) [2][]byte {
	if d.opts.ID[0] != nil && d.opts.ID[1] != nil {
		return d.opts.ID
	}
	sum := sha256.Sum256(body)
	h := sum[:16]
	return [2][]byte{h, h}
}

// maybeFlate returns data unchanged, or FlateDecode-compressed with the filter
// recorded on dict, according to Options.Compress.
func (d *Document) maybeFlate(dict *pdfDict, data []byte) []byte {
	if !d.opts.Compress {
		return data
	}
	dict.set("Filter", pdfName("FlateDecode"))
	return flateCompress(data)
}

// formatPDFDate renders t in PDF date syntax, e.g. D:20260728231500+02'00'.
func formatPDFDate(t time.Time) string {
	_, off := t.Zone()
	sign := '+'
	if off < 0 {
		sign = '-'
		off = -off
	}
	return fmt.Sprintf("D:%04d%02d%02d%02d%02d%02d%c%02d'%02d'",
		t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second(),
		sign, off/3600, (off%3600)/60)
}
