// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/go-pdfkit/reader"
)

// taggedDoc is a two-page tagged document with every kind of structure
// content: a heading, a paragraph that holds a link and runs onto the next
// page, a figure, an element left empty, a paragraph in another language,
// a background and a page-number artifact, a note.
func taggedDoc(t *testing.T, opts Options) *Document {
	t.Helper()
	f := loadTestFont(t)
	d := New(opts)
	root := d.Structure()

	p1 := d.AddPage(A4)
	s1 := root.Add(StructSect)
	bg := Rect{X: 0, Y: 0, Width: p1.Width(), Height: p1.Height()}
	p1.BeginArtifact(Artifact{Type: "Background", BBox: &bg})
	p1.SetFillColor(RGB8(250, 250, 250))
	p1.Rectangle(bg)
	p1.Fill()
	p1.EndMarkedContent()

	p1.SetFont(f, 24)
	h := s1.Add(StructH1)
	p1.BeginTag(h)
	if err := p1.Text(72, 760, "Title"); err != nil {
		t.Fatal(err)
	}
	p1.EndMarkedContent()

	p1.SetFont(f, 12)
	para := s1.Add(StructP)
	p1.BeginTag(para)
	_ = p1.Text(72, 700, "See")
	p1.EndMarkedContent()
	link := para.Add(StructLink)
	p1.BeginTag(link)
	// a span inside a tagged sequence is allowed: it is not structure
	p1.BeginActualText("the site")
	_ = p1.Text(100, 700, "the site")
	p1.EndMarkedContent()
	p1.EndMarkedContent()
	p1.AddTaggedLink(link, Rect{X: 100, Y: 695, Width: 50, Height: 14}, "https://example.com/", "the site")
	p1.BeginTag(para)
	_ = p1.Text(150, 700, ", and")
	p1.EndMarkedContent()

	fig := s1.Add(StructFigure)
	fig.Alt = "A red square"
	p1.BeginTag(fig)
	p1.SetFillColor(RGB8(255, 0, 0))
	p1.Rectangle(Rect{X: 72, Y: 500, Width: 100, Height: 100})
	p1.Fill()
	p1.EndMarkedContent()
	s1.Add(StructP) // never drawn: left out

	p2 := d.AddPage(A4)
	p2.SetFont(f, 12)
	p2.BeginTag(para) // the paragraph runs on
	_ = p2.Text(72, 760, "the rest")
	p2.EndMarkedContent()
	s2 := root.Add(StructSect)
	q := s2.Add(StructP)
	q.Lang = "fr"
	p2.BeginTag(q)
	_ = p2.Text(72, 700, "Bonjour")
	p2.BeginArtifact(Artifact{Type: "Pagination", Subtype: "Footer"}) // closes the paragraph's sequence
	_ = p2.Text(500, 30, "2")
	p2.EndMarkedContent()
	p2.AddTextAnnotation(TextAnnotation{Rect: Rect{X: 10, Y: 10, Width: 20, Height: 20}, Contents: "A note"})
	return d
}

// structWalk is what walking a written structure tree found.
type structWalk struct {
	types []string          // structure types, depth first
	mcids map[[2]int]string // (page, MCID) -> type of the element that claims it
	objrs int
}

// walkStructure checks a written file's structure tree against ISO
// 32000-1:2008, 14.7: every element's /P is its parent, every marked
// content its element names is in its page's content stream and maps back
// to the element through the parent tree, as every object reference does;
// every MCID in a content stream is claimed by exactly one element; tagged
// sequences do not nest, nor sit in an artifact, nor hold one; and every
// drawing is inside a tagged sequence or an artifact.
func walkStructure(t *testing.T, b []byte) structWalk {
	t.Helper()
	doc, err := reader.Open(b)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := doc.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	rootRef, ok := cat["StructTreeRoot"].(reader.Ref)
	if !ok {
		t.Fatal("no StructTreeRoot reference in the catalog")
	}
	root, _ := doc.GetDict(cat, "StructTreeRoot")
	if n, _ := reader.ToName(root["Type"]); n != "StructTreeRoot" {
		t.Errorf("structure tree root /Type %v", root["Type"])
	}
	if mi, ok := doc.GetDict(cat, "MarkInfo"); !ok || mi["Marked"] != reader.Bool(true) {
		t.Errorf("MarkInfo %v", mi)
	}

	// the parent tree, a flat number tree
	pt, ok := doc.GetDict(root, "ParentTree")
	if !ok {
		t.Fatal("no ParentTree")
	}
	nums, _ := reader.ToArray(pt["Nums"])
	parent := map[int64]reader.Object{}
	var maxKey int64 = -1
	for i := 0; i+1 < len(nums); i += 2 {
		k, _ := reader.ToInt(nums[i])
		parent[k] = nums[i+1]
		maxKey = max(maxKey, k)
	}
	if next, _ := reader.ToInt(root["ParentTreeNextKey"]); next <= maxKey {
		t.Errorf("ParentTreeNextKey %d not above the largest key %d", next, maxKey)
	}

	// pages count from 0 here, from 1 in the reader
	pageOf := map[reader.Ref]int{}
	for i := 0; i < doc.PageCount(); i++ {
		r, _ := doc.PageRef(i + 1)
		pageOf[r] = i
	}
	// parentOfMCID returns the element the parent tree gives for an MCID.
	parentOfMCID := func(page, mcid int) reader.Object {
		pd, _ := doc.Page(page + 1)
		key, ok := reader.ToInt(pd["StructParents"])
		if !ok {
			return nil
		}
		arr, _ := doc.Resolve(parent[key])
		a, _ := reader.ToArray(arr)
		if mcid >= len(a) {
			return nil
		}
		return a[mcid]
	}

	w := structWalk{mcids: map[[2]int]string{}}
	var walk func(ref, up reader.Ref)
	walk = func(ref, up reader.Ref) {
		o, err := doc.Get(ref)
		if err != nil {
			t.Fatal(err)
		}
		e, _ := reader.ToDict(o)
		s, _ := reader.ToName(e["S"])
		w.types = append(w.types, string(s))
		if p, _ := e["P"].(reader.Ref); p != up {
			t.Errorf("%s %v: /P %v, want %v", s, ref, e["P"], up)
		}
		pg := -1
		if r, ok := e["Pg"].(reader.Ref); ok {
			pg = pageOf[r]
		}
		kids := []reader.Object{e["K"]}
		if a, ok := e["K"].(reader.Array); ok {
			kids = a
		}
		claim := func(page, mcid int) {
			key := [2]int{page, mcid}
			if prev, dup := w.mcids[key]; dup {
				t.Errorf("page %d MCID %d claimed by %s and %s", page, mcid, prev, s)
			}
			w.mcids[key] = string(s)
			if got := parentOfMCID(page, mcid); got != ref {
				t.Errorf("page %d MCID %d: the parent tree gives %v, the element is %v", page, mcid, got, ref)
			}
		}
		for _, k := range kids {
			switch k := k.(type) {
			case nil:
			case reader.Integer:
				if pg < 0 {
					t.Errorf("%s: an MCID with no /Pg", s)
				}
				claim(pg, int(k))
			case reader.Ref:
				walk(k, ref)
			case reader.Dict:
				typ, _ := reader.ToName(k["Type"])
				page := pageOf[k["Pg"].(reader.Ref)]
				switch typ {
				case "MCR":
					m, _ := reader.ToInt(k["MCID"])
					claim(page, int(m))
				case "OBJR":
					w.objrs++
					a, _ := doc.Get(k["Obj"].(reader.Ref))
					ad, _ := reader.ToDict(a)
					key, ok := reader.ToInt(ad["StructParent"])
					if !ok || parent[key] != ref {
						t.Errorf("%s: its annotation's StructParent %v maps to %v", s, ad["StructParent"], parent[key])
					}
				default:
					t.Errorf("%s: a kid of type %q", s, typ)
				}
			default:
				t.Errorf("%s: a kid %T", s, k)
			}
		}
	}
	top, ok := root["K"].(reader.Ref)
	if !ok {
		t.Fatalf("StructTreeRoot /K %v", root["K"])
	}
	walk(top, rootRef)

	// the content streams
	drawing := map[string]bool{"Tj": true, "TJ": true, "'": true, "\"": true, "f": true, "F": true, "f*": true,
		"S": true, "s": true, "B": true, "B*": true, "b": true, "b*": true, "sh": true, "Do": true, "BI": true}
	for i := 0; i < doc.PageCount(); i++ {
		ops, err := doc.PageOperations(i + 1)
		if err != nil {
			t.Fatal(err)
		}
		type mark struct {
			tagged, artifact bool
		}
		var stack []mark
		inside := func(f func(mark) bool) bool {
			for _, m := range stack {
				if f(m) {
					return true
				}
			}
			return false
		}
		seen := 0
		for _, op := range ops {
			switch op.Operator {
			case "BMC", "BDC":
				tag, _ := reader.ToName(op.Operands[0])
				m := mark{artifact: tag == "Artifact"}
				if op.Operator == "BDC" {
					if props, ok := op.Operands[1].(reader.Dict); ok {
						if mcid, ok := reader.ToInt(props["MCID"]); ok {
							m.tagged = true
							seen++
							if _, claimed := w.mcids[[2]int{i, int(mcid)}]; !claimed {
								t.Errorf("page %d MCID %d is claimed by no element", i, mcid)
							}
						}
					}
				}
				if m.tagged && inside(func(o mark) bool { return o.tagged || o.artifact }) {
					t.Errorf("page %d: tagged content inside tagged content or an artifact", i)
				}
				if m.artifact && inside(func(o mark) bool { return o.tagged }) {
					t.Errorf("page %d: an artifact inside tagged content", i)
				}
				stack = append(stack, m)
			case "EMC":
				if len(stack) == 0 {
					t.Fatalf("page %d: EMC with no open sequence", i)
				}
				stack = stack[:len(stack)-1]
			default:
				if drawing[op.Operator] && !inside(func(o mark) bool { return o.tagged || o.artifact }) {
					t.Errorf("page %d: %s drawn neither tagged nor as an artifact", i, op.Operator)
				}
			}
		}
		if len(stack) != 0 {
			t.Errorf("page %d: %d sequences left open", i, len(stack))
		}
		claimed := 0
		for k := range w.mcids {
			if k[0] == i {
				claimed++
			}
		}
		if seen != claimed {
			t.Errorf("page %d: %d tagged sequences, %d claimed", i, seen, claimed)
		}
	}
	return w
}

func TestStructureTree(t *testing.T) {
	d := taggedDoc(t, Options{Title: "A <tagged> deck", Lang: "en", DisplayDocTitle: true, PDFUA: true})
	b := writeDoc(t, d)
	w := walkStructure(t, b)
	// depth first; the empty paragraph is left out, the note gets an Annot
	if got := strings.Join(w.types, " "); got != "Document Sect H1 P Link Figure Sect P Annot" {
		t.Errorf("structure %s", got)
	}
	want := map[[2]int]string{{0, 0}: "H1", {0, 1}: "P", {0, 2}: "Link", {0, 3}: "P", {0, 4}: "Figure", {1, 0}: "P", {1, 1}: "P"}
	if fmt.Sprint(w.mcids) != fmt.Sprint(want) {
		t.Errorf("MCIDs %v, want %v", w.mcids, want)
	}
	if w.objrs != 2 { // the link and the note
		t.Errorf("%d object references, want 2", w.objrs)
	}
	for _, s := range []string{
		"/Lang (en)", "/DisplayDocTitle true", "/Alt (A red square)", "/Lang (fr)",
		"/Contents (the site)", "/Tabs /S", "/Type /MCR",
		"/Artifact <</Type /Background /BBox [0 0 595.",
		"/Artifact <</Type /Pagination /Subtype /Footer>> BDC",
		"<pdfuaid:part>1</pdfuaid:part>", `<rdf:li xml:lang="x-default">A &lt;tagged&gt; deck</rdf:li>`,
	} {
		if !bytes.Contains(b, []byte(s)) {
			t.Errorf("no %q in the file", s)
		}
	}
	// Write is repeatable: the notes' Annot elements are made per write
	if again := writeDoc(t, d); !bytes.Equal(again, b) {
		t.Error("a second Write differs")
	}
	// packed into object streams, the tree is the same
	d2 := taggedDoc(t, Options{Lang: "en", ObjectStreams: true, Compress: true})
	if w2 := walkStructure(t, writeDoc(t, d2)); strings.Join(w2.types, " ") != strings.Join(w.types, " ") {
		t.Errorf("object streams: %v", w2.types)
	}
}

// An untagged document carries none of it.
func TestUntaggedHasNoStructure(t *testing.T) {
	d := New(Options{})
	p := d.AddPage(A4)
	p.Rectangle(Rect{X: 1, Y: 1, Width: 2, Height: 2})
	p.Fill()
	b := writeDoc(t, d)
	for _, s := range []string{"StructTreeRoot", "MarkInfo", "StructParents", "/Lang", "Metadata", "ViewerPreferences"} {
		if bytes.Contains(b, []byte(s)) {
			t.Errorf("%s in an untagged document", s)
		}
	}
}

// BeginTag closes an open tagged sequence (with what was opened inside it)
// rather than nesting one in it; foreign or nil elements are ignored, and so
// is a tagged link to one.
func TestBeginTagDoesNotNest(t *testing.T) {
	d := New(Options{})
	other := New(Options{}).Structure()
	p := d.AddPage(A4)
	a, b := d.Structure().Add(StructP), d.Structure().Add(StructP)
	p.BeginTag(a)
	p.BeginActualText("")
	p.BeginTag(b)
	p.BeginTag(nil)
	p.BeginTag(other)
	p.EndMarkedContent()
	p.AddTaggedNamedLink(other, Rect{Width: 1, Height: 1}, "x", "")
	got := p.buf.String()
	want := "/P <</MCID 0>> BDC\n/Span <</ActualText ()>> BDC\nEMC\nEMC\n/P <</MCID 1>> BDC\nEMC\n"
	if got != want {
		t.Errorf("content\n%q\nwant\n%q", got, want)
	}
	if p.tagOpen || p.marked != 0 {
		t.Errorf("open %v, marked %d", p.tagOpen, p.marked)
	}
	p.BeginArtifact(Artifact{})
	p.EndMarkedContent()
	if !strings.HasSuffix(p.buf.String(), "/Artifact BMC\nEMC\n") {
		t.Errorf("plain artifact: %q", p.buf.String())
	}
	if len(p.links) != 1 || p.links[0].elem != nil {
		t.Error("a link to a foreign element is an untagged link")
	}
	if a.Parent() != d.Structure() || d.Structure().Parent() != nil {
		t.Error("Parent")
	}
}

// An element with no content writes nothing, not even the root's /K
// target's kids; text that XML cannot carry is dropped from the metadata.
func TestStructureEdges(t *testing.T) {
	d := New(Options{Title: "a\x01b & \"c\"", Author: "Ann", Subject: "S", Keywords: "k", PDFUA: true})
	d.Structure().Add(StructP)
	p := d.AddPage(A4)
	p.BeginArtifact(Artifact{Type: "Layout"})
	p.Rectangle(Rect{Width: 1, Height: 1})
	p.Fill()
	b := writeDoc(t, d)
	w := walkStructure(t, b)
	if strings.Join(w.types, " ") != "Document" {
		t.Errorf("structure %v", w.types)
	}
	for _, s := range []string{"ab &amp; &quot;c&quot;", "<dc:creator><rdf:Seq><rdf:li>Ann", "<dc:description>", "<pdf:Keywords>k"} {
		if !bytes.Contains(b, []byte(s)) {
			t.Errorf("no %q", s)
		}
	}
	// a tagged element, a title, actual text, on several pages through MCRs
	d = New(Options{})
	e := d.Structure().Add(StructSpan)
	e.Title, e.ActualText = "T", "A"
	for range 3 {
		pg := d.AddPage(A4)
		pg.BeginTag(e)
		pg.Rectangle(Rect{Width: 1, Height: 1})
		pg.Fill()
	}
	b = writeDoc(t, d)
	w = walkStructure(t, b)
	if len(w.mcids) != 3 || !bytes.Contains(b, []byte("/T (T)")) || !bytes.Contains(b, []byte("/ActualText (A)")) {
		t.Errorf("mcids %v", w.mcids)
	}
}
