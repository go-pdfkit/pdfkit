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
	if got := df.Key("CIDToGIDMap").Name(); got != "Identity" {
		t.Errorf("CIDToGIDMap = %q", got)
	}

	// The embedded, subsetted TrueType program must itself be a parseable font
	// carrying the glyphs we used (H=1, i=2, A=3, plus A's components).
	prog := readStream(t, df.Key("FontDescriptor").Key("FontFile2"))
	sub, err := opentype.Parse(prog)
	if err != nil {
		t.Fatalf("re-parse embedded subset: %v", err)
	}
	for _, r := range "HiA" {
		if gid, ok := sub.GlyphIndex(r); !ok || gid == 0 {
			t.Errorf("subset missing glyph for %q", r)
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
	// FontFile3 carries the CFF program; its subtype must mark it CIDFontType0C.
	ff := df.Key("FontDescriptor").Key("FontFile3")
	if got := ff.Key("Subtype").Name(); got != "CIDFontType0C" {
		t.Errorf("FontFile3 Subtype = %q", got)
	}
	if len(readStream(t, ff)) == 0 {
		t.Error("empty embedded CFF program")
	}
	tu := string(readStream(t, fd.Key("ToUnicode")))
	if !strings.Contains(tu, "beginbfchar") {
		t.Error("ToUnicode has no bfchar section")
	}
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
