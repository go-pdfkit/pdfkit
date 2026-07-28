// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/go-opentype/opentype"
	"rsc.io/pdf"
)

// reopen writes doc and reparses it with the independent rsc.io/pdf reader,
// which acts as a correctness oracle for the generated file structure.
func reopen(t *testing.T, doc *Document) *pdf.Reader {
	t.Helper()
	var buf bytes.Buffer
	if err := doc.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	r, err := pdf.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("rsc.io/pdf reopen: %v", err)
	}
	return r
}

// firstFontDict returns the /F0 Type0 font dictionary of page 1 via the object
// graph rsc.io/pdf parsed independently from the bytes.
func firstFontDict(r *pdf.Reader) pdf.Value {
	return r.Page(1).V.Key("Resources").Key("Font").Key("F0")
}

func readStream(t *testing.T, v pdf.Value) []byte {
	t.Helper()
	rc := v.Reader()
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	return b
}

func TestOracleTrueType(t *testing.T) {
	ttf := synthTTF(defaultSynth())
	f, err := LoadFont(ttf)
	if err != nil {
		t.Fatal(err)
	}
	doc := New(Options{Title: "T", Author: "A"})
	p := doc.AddPage(A4)
	p.SetFont(f, 24)
	if err := p.Text(72, 700, "HiA"); err != nil {
		t.Fatal(err)
	}
	r := reopen(t, doc)

	if r.NumPage() != 1 {
		t.Fatalf("NumPage = %d, want 1", r.NumPage())
	}
	fd := firstFontDict(r)
	if got := fd.Key("Subtype").Name(); got != "Type0" {
		t.Errorf("Subtype = %q", got)
	}
	if got := fd.Key("Encoding").Name(); got != "Identity-H" {
		t.Errorf("Encoding = %q", got)
	}
	df := fd.Key("DescendantFonts").Index(0)
	if got := df.Key("Subtype").Name(); got != "CIDFontType2" {
		t.Errorf("descendant Subtype = %q", got)
	}

	// The subset renumbers glyphs, so /CIDToGIDMap is a stream mapping each CID
	// (the original glyph id) to its subset glyph id. Read it back as the oracle's
	// own view of the mapping.
	c2g := readStream(t, df.Key("CIDToGIDMap"))
	cidToGID := func(cid int) opentype.GlyphIndex {
		if 2*cid+1 >= len(c2g) {
			return 0
		}
		return opentype.GlyphIndex(int(c2g[2*cid])<<8 | int(c2g[2*cid+1]))
	}

	// The embedded, subsetted TrueType program must itself be a parseable font in
	// which each used glyph, at its remapped id, is contour-identical to the
	// original — proving the subset kept the outlines intact.
	prog := readStream(t, df.Key("FontDescriptor").Key("FontFile2"))
	sub, err := opentype.Parse(prog)
	if err != nil {
		t.Fatalf("re-parse embedded subset: %v", err)
	}
	orig := mustLoadOT(t, synthTTF(defaultSynth()))
	for _, r := range "HiA" {
		oldGID, ok := orig.GlyphIndex(r)
		if !ok || oldGID == 0 {
			t.Fatalf("original missing glyph for %q", r)
		}
		newGID := cidToGID(int(oldGID))
		if newGID == 0 {
			t.Errorf("CIDToGIDMap maps %q (cid %d) to .notdef", r, oldGID)
			continue
		}
		if !sameGlyphRender(orig, oldGID, sub, newGID) {
			t.Errorf("subset glyph for %q (old %d -> new %d) not contour-intact", r, oldGID, newGID)
		}
	}

	// The /ToUnicode CMap must map each glyph code back to its source text so a
	// reader can recover "HiA".
	tu := string(readStream(t, fd.Key("ToUnicode")))
	for code, want := range map[string]string{"<0001> <0048>": "H", "<0002> <0069>": "i", "<0003> <0041>": "A"} {
		if !strings.Contains(tu, code) {
			t.Errorf("ToUnicode missing %q (for %s)", code, want)
		}
	}
}

func TestOracleCFF(t *testing.T) {
	otf, err := os.ReadFile("testdata/SourceSerif4-Regular.otf")
	if err != nil {
		t.Fatal(err)
	}
	f, err := LoadFont(otf)
	if err != nil {
		t.Fatal(err)
	}
	if !f.IsCFF() {
		t.Fatal("expected CFF font")
	}
	doc := New(Options{Compress: true})
	p := doc.AddPage(Letter.Landscape())
	p.SetFont(f, 18)
	if err := p.Text(72, 400, "Hello"); err != nil {
		t.Fatal(err)
	}
	r := reopen(t, doc)
	fd := firstFontDict(r)
	df := fd.Key("DescendantFonts").Index(0)
	if got := df.Key("Subtype").Name(); got != "CIDFontType0" {
		t.Errorf("descendant Subtype = %q", got)
	}
	// A CFF program keeps its glyph numbering, so an Identity /CIDToGIDMap suffices.
	if got := df.Key("CIDToGIDMap").Name(); got != "Identity" {
		t.Errorf("CFF CIDToGIDMap = %q, want Identity", got)
	}
	// FontFile3 carries the CFF program; its subtype must mark it CIDFontType0C.
	ff := df.Key("FontDescriptor").Key("FontFile3")
	if got := ff.Key("Subtype").Name(); got != "CIDFontType0C" {
		t.Errorf("FontFile3 Subtype = %q", got)
	}
	prog := readStream(t, ff)
	if len(prog) == 0 {
		t.Fatal("empty embedded CFF program")
	}

	// pdfkit now truly subsets CFF charstrings: the embedded 'CFF ' program must be
	// smaller than the whole table it was cut from.
	whole, ok := f.ot.Table("CFF ")
	if !ok {
		t.Fatal("original font has no CFF table")
	}
	if len(prog) >= len(whole) {
		t.Errorf("CFF subset not smaller: embedded %d bytes, whole table %d", len(prog), len(whole))
	}

	// The subset must re-parse (wrapped back into an OTF) and, because CFF
	// subsetting preserves glyph numbering, each used glyph must render identically
	// to the original at its original id — proving the kept glyphs are intact.
	subF := mustLoadOT(t, wrapCFFinOTF(t, f.ot, prog))
	for _, ru := range "Hello" {
		gid, okg := f.ot.GlyphIndex(ru)
		if !okg || gid == 0 {
			t.Fatalf("original missing glyph for %q", ru)
		}
		if !sameGlyphRender(f.ot, gid, subF, gid) {
			t.Errorf("subset CFF glyph for %q (gid %d) not contour-intact", ru, gid)
		}
	}

	tu := string(readStream(t, fd.Key("ToUnicode")))
	if !strings.Contains(tu, "beginbfchar") {
		t.Error("ToUnicode has no bfchar section")
	}
}

// mustLoadOT parses raw font bytes with go-opentype, failing the test on error.
func mustLoadOT(t *testing.T, data []byte) *opentype.Font {
	t.Helper()
	f, err := opentype.Parse(data)
	if err != nil {
		t.Fatalf("opentype.Parse: %v", err)
	}
	return f
}

// wrapCFFinOTF rebuilds an OTTO sfnt from src's container tables with cff swapped
// in for the 'CFF ' table, so a bare subset CFF program can be re-parsed by
// go-opentype. src's other tables (head, hhea, maxp, hmtx, cmap, ...) are copied
// verbatim; the subset preserves glyph numbering, so they stay valid.
func wrapCFFinOTF(t *testing.T, src *opentype.Font, cff []byte) []byte {
	t.Helper()
	tables := map[string][]byte{"CFF ": cff}
	for _, tag := range src.TableTags() {
		if tag == "CFF " {
			continue
		}
		b, _ := src.Table(tag)
		tables[tag] = b
	}
	return assembleSFNT(0x4F54544F, tables) // OTTO
}

// sameGlyphRender reports whether glyph a in font fa and glyph b in font fb
// rasterise to the same alpha mask at a common size — a contour-level equality
// check independent of glyph numbering.
func sameGlyphRender(fa *opentype.Font, a opentype.GlyphIndex, fb *opentype.Font, b opentype.GlyphIndex) bool {
	const size = 64
	ba, ma, _, _, oka := fa.NewFace(size).GlyphMaskIndex(a, 0, 0)
	bb, mb, _, _, okb := fb.NewFace(size).GlyphMaskIndex(b, 0, 0)
	if oka != okb {
		return false
	}
	if !oka { // both have no outline (e.g. a space): trivially equal
		return true
	}
	if ba != bb || len(ma.Pix) != len(mb.Pix) {
		return false
	}
	for i := range ma.Pix {
		if ma.Pix[i] != mb.Pix[i] {
			return false
		}
	}
	return true
}

func TestOracleGraphicsAndImage(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(NewPageSize(300, 300))
	p.SetFillColor(RGB8(255, 0, 0))
	p.Rectangle(Rect{X: 10, Y: 10, Width: 100, Height: 50})
	p.Fill()
	// A 2x2 image with one transparent pixel exercises the soft mask path.
	img := makeTestImage()
	p.DrawImage(img, Rect{X: 150, Y: 150, Width: 80, Height: 80})

	r := reopen(t, doc)
	if r.NumPage() != 1 {
		t.Fatalf("NumPage = %d", r.NumPage())
	}
	xobj := r.Page(1).V.Key("Resources").Key("XObject")
	im := xobj.Key("Im0")
	if got := im.Key("Width").Int64(); got != 2 {
		t.Errorf("image width = %d", got)
	}
	if im.Key("SMask").Key("Width").Int64() != 2 {
		t.Error("expected soft mask")
	}
}
