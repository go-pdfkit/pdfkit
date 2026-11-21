// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"

	"github.com/go-pdfkit/reader"
)

// Page import: a page of another PDF becomes a form XObject (ISO 32000-1:2008,
// 8.10) of this document, painted with Page.DrawForm like any other form. It is
// what pdfTeX and xdvipdfmx do for \includegraphics{figure.pdf}: the figure's
// content stream is kept, its resources are copied with every object they
// reach, and the page box becomes the form's /BBox.
//
// The source is untrusted. Everything that bounds the work is checked by
// ImportPDFPage (and PDFPageInfo), before any object is written: how many
// objects the resources reach, how many stream bytes they carry, and how deep a
// direct object nests. A reference back to an ancestor is followed once.

// DefaultMaxImportObjects is the most indirect objects one imported page's
// resources may reach when [Options.MaxImportObjects] is zero.
const DefaultMaxImportObjects = 100_000

// DefaultMaxImportBytes is the most stream bytes (as stored, and as decoded for
// a page whose content is split in several streams) one imported page may
// carry when [Options.MaxImportBytes] is zero.
const DefaultMaxImportBytes = 256 << 20

// maxImportDepth bounds the nesting of the objects the walk descends into,
// references included.
const maxImportDepth = 256

// ErrImportLimit is returned for a page whose resources exceed
// [Options.MaxImportObjects], [Options.MaxImportBytes] or the nesting bound.
var ErrImportLimit = errors.New("pdfkit: imported page exceeds the import limits")

// ErrImportEncrypted is returned for an encrypted source: its strings and
// streams are enciphered under keys this document does not hold.
var ErrImportEncrypted = errors.New("pdfkit: cannot import a page of an encrypted PDF")

// PageInfo is what an imported page measures.
type PageInfo struct {
	// Width and Height are the size, in points, of the page box as displayed:
	// the crop box clamped to the media box (14.11.2), turned by /Rotate.
	Width, Height float64
}

func (d *Document) maxImportObjects() int {
	if d.opts.MaxImportObjects <= 0 {
		return DefaultMaxImportObjects
	}
	return d.opts.MaxImportObjects
}

func (d *Document) maxImportBytes() int64 {
	if d.opts.MaxImportBytes <= 0 {
		return DefaultMaxImportBytes
	}
	return d.opts.MaxImportBytes
}

// importedPage is a page read and bounded, ready to be written.
type importedPage struct {
	src       *reader.Document
	closure   map[int]reader.Object // every indirect object the page reaches, resolved
	resources reader.Object         // direct /Resources, or nil
	group     reader.Object         // direct /Group, or nil
	stream    *reader.Stream        // the page's only content stream, copied as stored
	content   []byte                // the joined content of several streams, decoded
	box       Rect                  // /BBox: the page box, in page space
	matrix    [6]float64            // /Matrix: the page's /Rotate
	shown     Rect                  // box after matrix: what a placement maps onto its rectangle
}

// PDFPageInfo reads page (counting from one) of the PDF src and reports its
// displayed size, applying every check ImportPDFPage applies, so a caller can
// size a figure and know that importing it will succeed.
func (d *Document) PDFPageInfo(src []byte, page int) (PageInfo, error) {
	imp, err := d.prepareImport(src, page)
	if err != nil {
		return PageInfo{}, err
	}
	return PageInfo{Width: imp.shown.Width, Height: imp.shown.Height}, nil
}

// ImportPDFPage returns page (counting from one) of the PDF src as a form of
// this document. The form's box is the page's crop box clamped to its media
// box, which is the box pdfTeX uses by default; /Rotate becomes the form's
// /Matrix. Paint it with Page.DrawForm, which maps the displayed box onto a
// rectangle. Importing the same page of the same bytes twice returns the same
// form, so a figure used thirty times is one stream in the file.
//
// A source that is not a PDF, is encrypted, has no such page, or whose
// resources exceed the import limits is refused with an error and nothing is
// added to the document.
func (d *Document) ImportPDFPage(src []byte, page int) (*Form, error) {
	sum := sha256.Sum256(src)
	key := string(sum[:]) + "#" + strconv.Itoa(page)
	if f, ok := d.importCache[key]; ok {
		return f, nil
	}
	imp, err := d.prepareImport(src, page)
	if err != nil {
		return nil, err
	}
	f := d.NewForm(imp.shown)
	f.imp = imp
	if d.importCache == nil {
		d.importCache = map[string]*Form{}
	}
	d.importCache[key] = f
	return f, nil
}

func (d *Document) prepareImport(src []byte, page int) (imp *importedPage, err error) {
	// The reader is written to refuse, not to panic; a recover costs nothing
	// and keeps a corrupt figure from costing the document.
	defer func() {
		if r := recover(); r != nil {
			imp, err = nil, fmt.Errorf("pdfkit: import: %v", r)
		}
	}()
	doc, err := reader.Open(src)
	if err != nil {
		return nil, fmt.Errorf("pdfkit: import: %w", err)
	}
	if doc.Encrypted() {
		return nil, ErrImportEncrypted
	}
	pg, err := doc.Page(page)
	if err != nil {
		return nil, fmt.Errorf("pdfkit: import: %w", err)
	}
	imp = &importedPage{src: doc, closure: map[int]reader.Object{}}

	media, ok := pageBox(doc, pg, "MediaBox")
	if !ok {
		return nil, errors.New("pdfkit: import: the page has no usable /MediaBox")
	}
	box := media
	if crop, ok := pageBox(doc, pg, "CropBox"); ok {
		if c, ok := intersectBox(crop, media); ok {
			box = c
		}
	}
	imp.box = box
	rot := 0
	if o, err := doc.Resolve(pg.Get("Rotate")); err == nil {
		if n, ok := reader.ToInt(o); ok {
			rot = int(((n % 360) + 360) % 360)
		}
	}
	imp.matrix = rotateMatrix(rot)
	imp.shown = transformedBox(box, imp.matrix)
	if !(imp.shown.Width > 0 && imp.shown.Height > 0) {
		return nil, errors.New("pdfkit: import: the page box is empty")
	}

	w := &importWalk{doc: doc, seen: imp.closure, maxObjs: d.maxImportObjects(), maxBytes: d.maxImportBytes()}
	if o, err := doc.Resolve(pg.Get("Resources")); err == nil {
		if rd, ok := reader.ToDict(o); ok {
			imp.resources = rd
			w.visit(rd, 0)
		}
	}
	if o, err := doc.Resolve(pg.Get("Group")); err == nil {
		if gd, ok := reader.ToDict(o); ok {
			imp.group = gd
			w.visit(gd, 0)
		}
	}
	if w.err != nil {
		return nil, w.err
	}
	if err := d.collectContent(imp, pg, w); err != nil {
		return nil, err
	}
	if w.err != nil {
		return nil, w.err
	}
	return imp, nil
}

// collectContent gathers the page's /Contents. A single stream is kept as
// stored (filters and all), so nothing is inflated at import. Several streams
// must be joined (8.10.2: a form has one), which means decoding them, up to the
// byte limit.
func (d *Document) collectContent(imp *importedPage, pg reader.Dict, w *importWalk) error {
	o, err := imp.src.Resolve(pg.Get("Contents"))
	if err != nil || o == nil {
		return nil // a page with no content is an empty form
	}
	var streams []*reader.Stream
	switch v := o.(type) {
	case *reader.Stream:
		streams = []*reader.Stream{v}
	case reader.Array:
		if len(v) > d.maxImportObjects() {
			return ErrImportLimit
		}
		for _, e := range v {
			r, err := imp.src.Resolve(e)
			if err != nil {
				continue
			}
			if s, ok := reader.ToStream(r); ok {
				streams = append(streams, s)
			}
		}
	}
	switch len(streams) {
	case 0:
		return nil
	case 1:
		imp.stream = streams[0]
		w.bytes += int64(len(streams[0].Raw))
		if w.bytes > w.maxBytes {
			return fmt.Errorf("%w: %d bytes of stream", ErrImportLimit, w.bytes)
		}
		w.visit(streams[0].Dict, 0)
		return nil
	}
	var buf bytes.Buffer
	budget := w.maxBytes
	for _, s := range streams {
		data, err := decodeBounded(imp.src, s, budget)
		if err != nil {
			return err
		}
		budget -= int64(len(data))
		buf.Write(data)
		buf.WriteByte('\n') // 7.8.2: the streams are split at token boundaries
	}
	imp.content = buf.Bytes()
	return nil
}

// isNull reports whether o is absent: Dict.Get answers Null for a missing key.
func isNull(o reader.Object) bool {
	if o == nil {
		return true
	}
	_, ok := o.(reader.Null)
	return ok
}

// decodeBounded decodes one content stream into at most limit bytes. A stream
// filtered by Flate alone, the usual case, is inflated through a limit reader,
// so a few hundred bytes cannot become a gigabyte; any other filter chain goes
// through the reader, whose own ceiling is per stream.
func decodeBounded(doc *reader.Document, s *reader.Stream, limit int64) ([]byte, error) {
	if limit <= 0 {
		return nil, ErrImportLimit
	}
	f := s.Dict.Get("Filter")
	if fo, err := doc.Resolve(f); err == nil {
		f = fo
	}
	if arr, ok := f.(reader.Array); ok && len(arr) == 1 {
		f = arr[0]
	}
	if n, ok := f.(reader.Name); ok && (n == "FlateDecode" || n == "Fl") && isNull(s.Dict.Get("DecodeParms")) && isNull(s.Dict.Get("DP")) {
		zr, err := zlib.NewReader(bytes.NewReader(s.Raw))
		if err != nil {
			return nil, fmt.Errorf("pdfkit: import: %w", err)
		}
		defer zr.Close()
		data, err := io.ReadAll(io.LimitReader(zr, limit+1))
		if int64(len(data)) > limit {
			return nil, fmt.Errorf("%w: a content stream inflates past %d bytes", ErrImportLimit, limit)
		}
		if err != nil && len(data) == 0 {
			return nil, fmt.Errorf("pdfkit: import: %w", err)
		}
		return data, nil // a truncated stream keeps what it yielded
	}
	if isNull(f) {
		if int64(len(s.Raw)) > limit {
			return nil, ErrImportLimit
		}
		return s.Raw, nil
	}
	data, _, err := doc.DecodeStream(s)
	if err != nil {
		return nil, fmt.Errorf("pdfkit: import: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: a content stream decodes past %d bytes", ErrImportLimit, limit)
	}
	return data, nil
}

// pageBox reads a four-number box (the numbers may be indirect) and returns it
// normalised, with finite coordinates.
func pageBox(doc *reader.Document, pg reader.Dict, key reader.Name) (Rect, bool) {
	o, err := doc.Resolve(pg.Get(key))
	if err != nil {
		return Rect{}, false
	}
	arr, ok := reader.ToArray(o)
	if !ok || len(arr) != 4 {
		return Rect{}, false
	}
	var v [4]float64
	for i, e := range arr {
		r, err := doc.Resolve(e)
		if err != nil {
			return Rect{}, false
		}
		f, ok := reader.ToFloat(r)
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) > 1e7 {
			return Rect{}, false
		}
		v[i] = f
	}
	x0, x1 := math.Min(v[0], v[2]), math.Max(v[0], v[2])
	y0, y1 := math.Min(v[1], v[3]), math.Max(v[1], v[3])
	return Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}, true
}

// intersectBox clamps a to b (14.11.2: the crop box is clamped to the media
// box); ok is false when they do not overlap.
func intersectBox(a, b Rect) (Rect, bool) {
	x0, y0 := math.Max(a.X, b.X), math.Max(a.Y, b.Y)
	x1, y1 := math.Min(a.X+a.Width, b.X+b.Width), math.Min(a.Y+a.Height, b.Y+b.Height)
	if x1 <= x0 || y1 <= y0 {
		return Rect{}, false
	}
	return Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}, true
}

// rotateMatrix is the form matrix that shows a page turned clockwise by rot
// degrees (0, 90, 180 or 270), as a viewer shows it.
func rotateMatrix(rot int) [6]float64 {
	switch rot {
	case 90:
		return [6]float64{0, -1, 1, 0, 0, 0}
	case 180:
		return [6]float64{-1, 0, 0, -1, 0, 0}
	case 270:
		return [6]float64{0, 1, -1, 0, 0, 0}
	}
	return [6]float64{1, 0, 0, 1, 0, 0}
}

// transformedBox is the bounding box of r's corners under m (8.10.1 step 2).
func transformedBox(r Rect, m [6]float64) Rect {
	xs := [2]float64{r.X, r.X + r.Width}
	ys := [2]float64{r.Y, r.Y + r.Height}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, x := range xs {
		for _, y := range ys {
			tx := m[0]*x + m[2]*y + m[4]
			ty := m[1]*x + m[3]*y + m[5]
			minX, maxX = math.Min(minX, tx), math.Max(maxX, tx)
			minY, maxY = math.Min(minY, ty), math.Max(maxY, ty)
		}
	}
	return Rect{X: minX, Y: minY, Width: maxX - minX, Height: maxY - minY}
}

// importWalk collects the closure of the indirect objects a page reaches,
// counting what it finds against the limits. seen doubles as the cycle guard:
// an object is entered once, whatever refers back to it.
type importWalk struct {
	doc      *reader.Document
	seen     map[int]reader.Object
	objs     int
	maxObjs  int
	bytes    int64
	maxBytes int64
	err      error
}

// skipKey names what is never followed: links back up the page tree, the
// thumbnail, and the keys that place the object in the source's structure
// tree. /Length is rewritten when the stream is written.
func skipKey(k reader.Name, stream bool) bool {
	switch k {
	case "Parent", "Thumb", "StructParent", "StructParents":
		return true
	case "Length":
		return stream
	}
	return false
}

func (w *importWalk) fail(format string, a ...any) {
	if w.err == nil {
		w.err = fmt.Errorf("%w: %s", ErrImportLimit, fmt.Sprintf(format, a...))
	}
}

func (w *importWalk) visit(o reader.Object, depth int) {
	if w.err != nil {
		return
	}
	if depth > maxImportDepth {
		w.fail("nesting deeper than %d", maxImportDepth)
		return
	}
	switch v := o.(type) {
	case reader.Ref:
		if _, ok := w.seen[v.Num]; ok {
			return
		}
		w.seen[v.Num] = reader.Null{} // entered: a reference back finds this
		w.objs++
		if w.objs > w.maxObjs {
			w.fail("more than %d objects", w.maxObjs)
			return
		}
		r, err := reader.Resolve(v, w.doc.Get)
		if err != nil || r == nil {
			r = reader.Null{} // a dangling reference is the null object (7.3.10)
		}
		w.seen[v.Num] = r
		w.visit(r, depth+1)
	case reader.Array:
		for _, e := range v {
			w.visit(e, depth+1)
		}
	case reader.Dict:
		w.dict(v, depth, false)
	case *reader.Stream:
		w.bytes += int64(len(v.Raw))
		if w.bytes > w.maxBytes {
			w.fail("more than %d bytes of stream", w.maxBytes)
			return
		}
		w.dict(v.Dict, depth, true)
	}
}

func (w *importWalk) dict(d reader.Dict, depth int, stream bool) {
	for k, e := range d {
		if skipKey(k, stream) {
			continue
		}
		w.visit(e, depth+1)
	}
}

// importConv writes the closure of one source document into a builder,
// renumbering as it goes. One conv serves every page imported from a document,
// so the objects they share are written once.
type importConv struct {
	bd   *builder
	imp  *importedPage
	refs map[int]objRef
}

func (c *importConv) value(o reader.Object) pdfValue {
	switch v := o.(type) {
	case reader.Bool:
		return pdfBool(v)
	case reader.Integer:
		return pdfInt(v)
	case reader.Real:
		return pdfReal(v)
	case reader.String:
		return pdfString(append([]byte(nil), v...))
	case reader.Name:
		return pdfName(v)
	case reader.Array:
		out := make(pdfArray, len(v))
		for i, e := range v {
			out[i] = c.value(e)
		}
		return out
	case reader.Dict:
		return c.dict(v, false)
	case *reader.Stream:
		return &pdfStream{dict: c.dict(v.Dict, true), data: v.Raw}
	case reader.Ref:
		if r, ok := c.refs[v.Num]; ok {
			return r
		}
		obj, ok := c.imp.closure[v.Num]
		if !ok {
			return pdfNull{}
		}
		r := c.bd.reserve()
		c.refs[v.Num] = r
		c.bd.put(r, c.value(obj))
		return r
	}
	return pdfNull{}
}

// dict converts d with its keys in ascending order, so the file is the same
// every time it is written.
func (c *importConv) dict(d reader.Dict, stream bool) *pdfDict {
	keys := make([]string, 0, len(d))
	for k := range d {
		if !skipKey(k, stream) {
			keys = append(keys, string(k))
		}
	}
	sort.Strings(keys)
	out := newDict()
	for _, k := range keys {
		out.set(k, c.value(d[reader.Name(k)]))
	}
	return out
}

// importedForm builds the form XObject of an imported page.
func (d *Document) importedForm(bd *builder, f *Form, convs map[*reader.Document]*importConv) pdfValue {
	imp := f.imp
	c := convs[imp.src]
	if c == nil {
		c = &importConv{bd: bd, imp: imp, refs: map[int]objRef{}}
		convs[imp.src] = c
	}
	c.imp = imp
	var dict *pdfDict
	var data []byte
	if imp.stream != nil {
		dict = c.dict(imp.stream.Dict, true)
		data = imp.stream.Raw
	} else {
		dict = newDict()
		data = d.maybeFlate(dict, imp.content)
	}
	dict.set("Type", pdfName("XObject"))
	dict.set("Subtype", pdfName("Form"))
	b := imp.box
	dict.set("BBox", pdfArray{pdfReal(b.X), pdfReal(b.Y), pdfReal(b.X + b.Width), pdfReal(b.Y + b.Height)})
	if m := imp.matrix; m != [6]float64{1, 0, 0, 1, 0, 0} {
		dict.set("Matrix", pdfArray{pdfReal(m[0]), pdfReal(m[1]), pdfReal(m[2]), pdfReal(m[3]), pdfReal(m[4]), pdfReal(m[5])})
	}
	if imp.resources != nil {
		dict.set("Resources", c.value(imp.resources))
	} else {
		dict.set("Resources", newDict())
	}
	if imp.group != nil {
		dict.set("Group", c.value(imp.group))
	}
	return &pdfStream{dict: dict, data: data}
}
