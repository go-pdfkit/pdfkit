// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"rsc.io/pdf"
)

// sampleDoc is the small document the classic-writer golden hash was captured
// from: a filled rectangle, a URI link, a named destination with an internal
// link to it, an outline entry and a title. opts.Title is forced so the golden
// bytes do not depend on the caller.
func sampleDoc(opts Options) *Document {
	opts.Title = "Golden"
	doc := New(opts)
	p := doc.AddPage(A4)
	p.SetFillColor(Gray{0.5})
	p.Rectangle(Rect{X: 1, Y: 2, Width: 3, Height: 4})
	p.Fill()
	p.AddLink(Rect{X: 100, Y: 200, Width: 80, Height: 12}, "https://example.org/")
	p.AddNamedDest("top", 0, 800)
	p.AddNamedLink(Rect{X: 100, Y: 100, Width: 80, Height: 12}, "top")
	doc.AddOutlineItem("Start", 1, 0)
	return doc
}

// reopenBytes parses b with the independent rsc.io/pdf reader, which
// understands cross-reference streams and object streams.
func reopenBytes(t *testing.T, b []byte) *pdf.Reader {
	t.Helper()
	r, err := pdf.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("rsc.io/pdf reopen: %v", err)
	}
	return r
}

// With ObjectStreams off the writer must produce exactly the bytes it produced
// before object streams existed: the hash was captured from main at 2394e07,
// before objstm.go was added. A deliberate change to the classic layout must
// update it; an accidental one fails here.
func TestClassicWriterUnchanged(t *testing.T) {
	const want = "2d364ab3b74afedc94e708d8fd823a9031eeddb1343c4d4fa4fbbafddaeca9ed"
	b := writeDoc(t, sampleDoc(Options{}))
	sum := sha256.Sum256(b)
	if got := hex.EncodeToString(sum[:]); got != want || len(b) != 1313 {
		t.Fatalf("classic output changed: sha256 %s, %d bytes; want %s, 1313 bytes\n%s", got, len(b), want, b)
	}
	for _, want := range []string{"%PDF-1.7\n", "\nxref\n", "\ntrailer\n"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("classic output lacks %q", want)
		}
	}
	for _, bad := range []string{"/ObjStm", "/XRef"} {
		if bytes.Contains(b, []byte(bad)) {
			t.Errorf("classic output contains %q", bad)
		}
	}
}

// With ObjectStreams on the file declares PDF 1.5, has no xref table or
// trailer keyword, packs the dictionaries into an /ObjStm, points startxref at
// an /XRef stream carrying the trailer entries, is reproducible, and reads back
// through the independent oracle with every object reachable.
func TestObjectStreamsLayout(t *testing.T) {
	b := writeDoc(t, sampleDoc(Options{ObjectStreams: true}))
	if !bytes.Equal(b, writeDoc(t, sampleDoc(Options{ObjectStreams: true}))) {
		t.Fatal("object-stream output is not reproducible")
	}
	if !bytes.HasPrefix(b, []byte("%PDF-1.5\n")) {
		t.Errorf("header = %q, want %%PDF-1.5", b[:9])
	}
	for _, want := range []string{"/Type /ObjStm", "/Type /XRef", "/W [1 2 1]", "/Root 1 0 R", "/Info ", "/ID [<"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("output lacks %q", want)
		}
	}
	for _, bad := range []string{"\nxref\n", "trailer"} {
		if bytes.Contains(b, []byte(bad)) {
			t.Errorf("output contains %q", bad)
		}
	}

	// startxref names the byte where the cross-reference stream object starts.
	m := regexp.MustCompile(`startxref\n(\d+)\n%%EOF\n$`).FindSubmatch(b)
	if m == nil {
		t.Fatalf("no startxref at the end of the file:\n%s", b)
	}
	off, _ := strconv.Atoi(string(m[1]))
	if !regexp.MustCompile(`^\d+ 0 obj\n<</Type /XRef `).Match(b[off:]) {
		t.Errorf("startxref %d does not point at the /XRef stream: %q", off, b[off:min(off+40, len(b))])
	}

	r := reopenBytes(t, b)
	if r.NumPage() != 1 {
		t.Fatalf("NumPage = %d, want 1", r.NumPage())
	}
	if got := r.Trailer().Key("Info").Key("Title").Text(); got != "Golden" {
		t.Errorf("/Info /Title via the xref stream = %q", got)
	}
	annots := r.Page(1).V.Key("Annots")
	if annots.Len() != 2 {
		t.Fatalf("Annots len = %d, want 2", annots.Len())
	}
	if got := annots.Index(0).Key("A").Key("URI").Text(); got != "https://example.org/" {
		t.Errorf("link URI = %q", got)
	}
	if got := annots.Index(1).Key("A").Key("D").Text(); got != "top" {
		t.Errorf("named link dest = %q", got)
	}
	cat := r.Trailer().Key("Root")
	if got := cat.Key("Outlines").Key("First").Key("Title").Text(); got != "Start" {
		t.Errorf("outline title = %q", got)
	}
	if got := cat.Key("Names").Key("Dests").Key("Names").Index(0).Text(); got != "top" {
		t.Errorf("first named destination = %q", got)
	}
	if err := sampleDoc(Options{ObjectStreams: true}).Write(&failWriter{}); err == nil {
		t.Error("expected write error")
	}
}

// linkDoc is a page carrying n URI links, each over its own strip of the page.
func linkDoc(opts Options, n int, uri func(i int) string) *Document {
	doc := New(opts)
	p := doc.AddPage(A4)
	for i := 0; i < n; i++ {
		p.AddLink(Rect{X: 72, Y: float64(i % 60 * 12), Width: 400, Height: 12}, uri(i))
	}
	return doc
}

// Object streams are chunked on both bounds. 1 200 links plus the catalog,
// pages, page and info dictionaries are 1 204 eligible objects: one full stream
// of 1 000 and one of 204. A 1.2 MB link is over the byte bound on its own, so
// it closes the stream before it and gets one to itself, and the objects after
// it open a third.
func TestObjectStreamsChunking(t *testing.T) {
	b := writeDoc(t, linkDoc(Options{ObjectStreams: true}, 1200, func(i int) string {
		return "https://example.org/" + strconv.Itoa(i)
	}))
	if got := bytes.Count(b, []byte("/Type /ObjStm")); got != 2 {
		t.Errorf("1 204 objects packed into %d object streams, want 2", got)
	}
	if !bytes.Contains(b, []byte("/N 1000 ")) || !bytes.Contains(b, []byte("/N 204 ")) {
		t.Error("object streams are not 1000 + 204 objects")
	}
	r := reopenBytes(t, b)
	if got := r.Page(1).V.Key("Annots").Len(); got != 1200 {
		t.Errorf("Annots len = %d, want 1200", got)
	}

	huge := strings.Repeat("a", objStmMaxBytes+objStmMaxBytes/5)
	b = writeDoc(t, linkDoc(Options{ObjectStreams: true}, 3, func(i int) string {
		if i == 1 {
			return "https://example.org/" + huge
		}
		return "https://example.org/small"
	}))
	if got := bytes.Count(b, []byte("/Type /ObjStm")); got != 3 {
		t.Errorf("an over-bound object split the packing into %d object streams, want 3", got)
	}
	r = reopenBytes(t, b)
	if got := r.Page(1).V.Key("Annots").Index(1).Key("A").Key("URI").Text(); len(got) != len(huge)+20 {
		t.Errorf("huge URI read back with %d bytes, want %d", len(got), len(huge)+20)
	}
}

// loadTestFont loads the OpenType face under testdata.
func loadTestFont(t *testing.T) *Font {
	t.Helper()
	otf, err := os.ReadFile("testdata/SourceSerif4-Regular.otf")
	if err != nil {
		t.Fatal(err)
	}
	f, err := LoadFont(otf)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// manyLinksDoc is the RFC-shaped case: pages of text with, when links is set,
// a URI link over every line — 100 pages of 50 lines, 5 000 links.
func manyLinksDoc(t *testing.T, f *Font, opts Options, links bool) *Document {
	t.Helper()
	doc := New(opts)
	for pg := 0; pg < 100; pg++ {
		p := doc.AddPage(A4)
		p.SetFont(f, 10)
		for ln := 0; ln < 50; ln++ {
			y := 800 - float64(ln)*14
			uri := fmt.Sprintf("https://www.rfc-editor.org/rfc/rfc9110#section-%d.%d", pg+1, ln+1)
			if err := p.Text(72, y, "See "+uri); err != nil {
				t.Fatal(err)
			}
			if links {
				p.AddLink(Rect{X: 72, Y: y - 3, Width: 300, Height: 12}, uri)
			}
		}
	}
	return doc
}

// Five thousand link annotations, compressed: the object streams must undo
// most of what the annotations add, and every one must still be there. The
// sizes are logged for the record; run with -v to see them.
func TestObjectStreamsFiveThousandLinks(t *testing.T) {
	f := loadTestFont(t)
	size := map[string]int{}
	out := map[string][]byte{}
	for _, c := range []struct {
		name  string
		links bool
		objs  bool
	}{
		{"text/classic", false, false},
		{"text/objstm", false, true},
		{"links/classic", true, false},
		{"links/objstm", true, true},
	} {
		b := writeDoc(t, manyLinksDoc(t, f, Options{Compress: true, ObjectStreams: c.objs}, c.links))
		size[c.name] = len(b)
		out[c.name] = b
		t.Logf("%-14s %8d bytes", c.name, len(b))
	}
	addedClassic := size["links/classic"] - size["text/classic"]
	addedObjstm := size["links/objstm"] - size["text/objstm"]
	t.Logf("5 000 links add %d bytes classic, %d bytes packed (%.1f%%)",
		addedClassic, addedObjstm, 100*float64(addedObjstm)/float64(addedClassic))
	if 4*addedObjstm > addedClassic {
		t.Errorf("packed annotations cost %d bytes, more than a quarter of the %d bare", addedObjstm, addedClassic)
	}
	if size["text/objstm"] >= size["text/classic"] {
		t.Errorf("object streams grew a link-free document: %d >= %d", size["text/objstm"], size["text/classic"])
	}

	for _, name := range []string{"links/classic", "links/objstm"} {
		r := reopenBytes(t, out[name])
		if r.NumPage() != 100 {
			t.Errorf("%s: NumPage = %d, want 100", name, r.NumPage())
		}
		for _, pg := range []int{1, 50, 100} {
			annots := r.Page(pg).V.Key("Annots")
			if annots.Len() != 50 {
				t.Errorf("%s: page %d has %d annotations, want 50", name, pg, annots.Len())
				continue
			}
			want := fmt.Sprintf("https://www.rfc-editor.org/rfc/rfc9110#section-%d.50", pg)
			if got := annots.Index(49).Key("A").Key("URI").Text(); got != want {
				t.Errorf("%s: page %d last link = %q, want %q", name, pg, got, want)
			}
		}
	}
}

// richDoc uses every feature that lands objects in the file: embedded font
// text, two images (one with a soft mask), a two-level outline, named
// destinations, internal and external links.
func richDoc(t *testing.T, opts Options) *Document {
	t.Helper()
	f := loadTestFont(t)
	doc := New(opts)
	p1 := doc.AddPage(A4)
	p1.SetFont(f, 14)
	if err := p1.Text(72, 760, "Chapter one"); err != nil {
		t.Fatal(err)
	}
	p1.DrawImage(makeTestImage(), Rect{X: 72, Y: 600, Width: 100, Height: 100})
	p1.DrawImage(makeOpaqueImage(), Rect{X: 200, Y: 600, Width: 100, Height: 100})
	p1.AddNamedDest("intro", 0, 780)
	p1.AddLink(Rect{X: 72, Y: 500, Width: 200, Height: 14}, "https://example.org/")
	p2 := doc.AddPage(A4)
	p2.SetFont(f, 12)
	if err := p2.Text(72, 760, "Section"); err != nil {
		t.Fatal(err)
	}
	p2.AddNamedDest("section", 0, 780)
	p2.AddNamedLink(Rect{X: 72, Y: 700, Width: 200, Height: 14}, "intro")
	doc.AddOutlineItem("Chapter", 1, 0)
	doc.AddOutlineItem("Section", 2, 1)
	return doc
}

// Compression and object streams together must not lose anything: the font,
// both images, the outline tree, the name tree and the links all read back.
func TestObjectStreamsRichDocument(t *testing.T) {
	r := reopenBytes(t, writeDoc(t, richDoc(t, Options{Compress: true, ObjectStreams: true, Title: "Rich"})))
	if r.NumPage() != 2 {
		t.Fatalf("NumPage = %d, want 2", r.NumPage())
	}
	if got := firstFontDict(r).Key("Subtype").Name(); got != "Type0" {
		t.Errorf("font Subtype = %q", got)
	}
	xobj := r.Page(1).V.Key("Resources").Key("XObject")
	for _, name := range []string{"Im0", "Im1"} {
		img := xobj.Key(name)
		if got := img.Key("Subtype").Name(); got != "Image" {
			t.Errorf("%s Subtype = %q", name, got)
		}
		if got := len(readStream(t, img)); got != 2*2*3 {
			t.Errorf("%s decodes to %d bytes, want 12", name, got)
		}
	}
	if got := len(readStream(t, xobj.Key("Im0").Key("SMask"))); got != 2*2 {
		t.Errorf("soft mask decodes to %d bytes, want 4", got)
	}
	cat := r.Trailer().Key("Root")
	ch := cat.Key("Outlines").Key("First")
	if got := ch.Key("Title").Text() + "/" + ch.Key("First").Key("Title").Text(); got != "Chapter/Section" {
		t.Errorf("outline = %q", got)
	}
	names := cat.Key("Names").Key("Dests").Key("Names")
	if got := names.Index(0).Text() + "," + names.Index(2).Text(); got != "intro,section" {
		t.Errorf("named destinations = %q", got)
	}
	if got := r.Page(2).V.Key("Annots").Index(0).Key("A").Key("D").Text(); got != "intro" {
		t.Errorf("internal link dest = %q", got)
	}
	if got := r.Trailer().Key("Info").Key("Title").Text(); got != "Rich" {
		t.Errorf("Title = %q", got)
	}
}

// qpdf is a second, independent oracle when it is installed: the file must
// check clean with no warning, and its cross-reference must list every link
// annotation as compressed (type 2) and every stream as uncompressed (type 1).
func TestObjectStreamsQPDF(t *testing.T) {
	qpdf, err := exec.LookPath("qpdf")
	if err != nil {
		t.Skip("qpdf not installed")
	}
	dir := t.TempDir()
	for name, doc := range map[string]*Document{
		"sample": sampleDoc(Options{Compress: true, ObjectStreams: true}),
		"rich":   richDoc(t, Options{Compress: true, ObjectStreams: true}),
	} {
		path := filepath.Join(dir, name+".pdf")
		if err := os.WriteFile(path, writeDoc(t, doc), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(qpdf, "--check", path).CombinedOutput()
		if err != nil || !bytes.Contains(out, []byte("No syntax or stream encoding errors found")) ||
			bytes.Contains(bytes.ToLower(out), []byte("warning")) {
			t.Errorf("%s: qpdf --check: %v\n%s", name, err, out)
		}

		out, err = exec.Command(qpdf, "--show-xref", path).Output()
		if err != nil {
			t.Fatalf("%s: qpdf --show-xref: %v", name, err)
		}
		compressed := map[string]bool{}
		for _, m := range regexp.MustCompile(`(?m)^(\d+)/0: (compressed|uncompressed)`).FindAllStringSubmatch(string(out), -1) {
			compressed[m[1]] = m[2] == "compressed"
		}

		out, err = exec.Command(qpdf, "--json", "--json-key=qpdf", path).Output()
		if err != nil {
			t.Fatalf("%s: qpdf --json: %v", name, err)
		}
		var doc struct {
			QPDF []map[string]json.RawMessage `json:"qpdf"`
		}
		if err := json.Unmarshal(out, &doc); err != nil || len(doc.QPDF) != 2 {
			t.Fatalf("%s: qpdf --json: %v (%d parts)", name, err, len(doc.QPDF))
		}
		links, streams := 0, 0
		for key, raw := range doc.QPDF[1] {
			num, ok := strings.CutPrefix(key, "obj:")
			if !ok {
				continue
			}
			num, _, _ = strings.Cut(num, " ")
			var obj struct {
				Value  map[string]any `json:"value"`
				Stream map[string]any `json:"stream"`
			}
			if err := json.Unmarshal(raw, &obj); err != nil {
				t.Fatal(err)
			}
			switch {
			case obj.Stream != nil:
				streams++
				if compressed[num] {
					t.Errorf("%s: stream object %s is inside an object stream", name, num)
				}
			case obj.Value["/Subtype"] == "/Link":
				links++
				if !compressed[num] {
					t.Errorf("%s: link annotation %s is not in an object stream", name, num)
				}
			}
		}
		if links != 2 || streams < 2 {
			t.Errorf("%s: qpdf saw %d link annotations and %d streams", name, links, streams)
		}
	}
}
