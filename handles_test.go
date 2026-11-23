// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"testing"
)

// alphaDoc is the document the golden hash below was taken from, on the
// linear-scan SetAlpha of v0.25.0, before it became a map.
func alphaDoc() *Document {
	doc := New(Options{})
	p := doc.AddPage(A4)
	for _, a := range [][2]float64{{0.5, 0.7}, {0.5, 0.7}, {0.2, 0.2}, {0.7, 0.5}, {0.2, 0.2}, {1, 1}, {0.5, 0.7}, {0, 0}, {math.Copysign(0, -1), 0}} {
		p.SetAlpha(a[0], a[1])
		p.Rectangle(Rect{Width: 10, Height: 10})
		p.Fill()
	}
	q := doc.AddPage(A4)
	q.SetAlpha(0.3, 0.3)
	q.SetAlpha(0.5, 0.7)
	q.SetAlpha(0.3, 0.3)
	return doc
}

// goldenAlphaDoc is the SHA-256 of alphaDoc's bytes on the linear scan.
const goldenAlphaDoc = "bf88c2424388bdfaf2b2e0ef1b206783fb93c16b2df24ef773c2e1a48b11ea47"

func TestSetAlphaOutputIsByteIdenticalToTheLinearScan(t *testing.T) {
	sum := sha256.Sum256(writeDoc(t, alphaDoc()))
	if got := hex.EncodeToString(sum[:]); got != goldenAlphaDoc {
		t.Errorf("document hash = %s, want %s", got, goldenAlphaDoc)
	}
}

func TestSetAlphaIndicesMatchFirstUse(t *testing.T) {
	p := New(Options{}).AddPage(A4)
	var want []string
	order := [][2]float64{{.1, .1}, {.2, .2}, {.1, .1}, {.1, .2}, {.2, .2}, {.2, .1}}
	idx := []int{0, 1, 0, 2, 1, 3}
	for i, a := range order {
		p.SetAlpha(a[0], a[1])
		want = append(want, fmt.Sprintf("/GS%d gs\n", idx[i]))
	}
	if got := content(p); got != strings.Join(want, "") {
		t.Errorf("content = %q, want %q", got, strings.Join(want, ""))
	}
	if len(p.extGStates) != 4 || len(p.extGIndex) != 4 {
		t.Errorf("states=%d index=%d, want 4 and 4", len(p.extGStates), len(p.extGIndex))
	}
}

func BenchmarkSetAlphaDistinct100k(b *testing.B) {
	for i := 0; i < b.N; i++ {
		p := New(Options{}).AddPage(A4)
		for k := 0; k < 100_000; k++ {
			p.SetAlpha(float64(k)/100_000, 1)
		}
	}
}

func TestContentLenTracksTheStream(t *testing.T) {
	p := New(Options{}).AddPage(A4)
	if n := p.ContentLen(); n != 0 {
		t.Fatalf("empty page ContentLen = %d, want 0", n)
	}
	p.Save()
	p.Rectangle(Rect{Width: 1, Height: 2})
	p.Fill()
	p.Restore()
	if got, want := p.ContentLen(), int64(len(content(p))); got != want || got == 0 {
		t.Errorf("ContentLen = %d, want %d (the stream's length)", got, want)
	}
	before := p.ContentLen()
	p.Save()
	if p.ContentLen() != before+int64(len("q\n")) {
		t.Errorf("ContentLen did not grow by exactly the bytes written")
	}
}

func TestImageHandleIsOneXObjectForManyDraws(t *testing.T) {
	doc := New(Options{})
	h := doc.AddImage(makeOpaqueImage())
	if h.Width() != makeOpaqueImage().Bounds().Dx() || h.Height() != makeOpaqueImage().Bounds().Dy() {
		t.Errorf("handle is %dx%d", h.Width(), h.Height())
	}
	p1, p2 := doc.AddPage(A4), doc.AddPage(A4)
	for i := 0; i < 5; i++ {
		if err := p1.DrawImageHandle(h, Rect{X: float64(i), Width: 10, Height: 10}); err != nil {
			t.Fatal(err)
		}
	}
	if err := p2.DrawImageHandleMatrix(h, 0, 10, -10, 0, 50, 60); err != nil {
		t.Fatal(err)
	}
	if n := countImageObjects(t, doc); n != 1 {
		t.Errorf("emitted %d image objects, want 1", n)
	}
	if got := strings.Count(content(p1), "/Im0 Do"); got != 5 {
		t.Errorf("page 1 paints %d times, want 5", got)
	}
	if c := content(p2); !strings.Contains(c, "0 10 -10 0 50 60 cm\n/Im0 Do\n") {
		t.Errorf("matrix draw wrote %q", c)
	}
	// Both pages list the one XObject as a resource.
	out := string(writeDoc(t, doc))
	if strings.Count(out, "/Im0 ") < 2 {
		t.Errorf("each page should reference /Im0 in its resources")
	}
}

func TestImageHandleMatchesDrawImageOutput(t *testing.T) {
	a := New(Options{})
	a.AddPage(A4).DrawImage(makeTestImage(), Rect{X: 3, Y: 4, Width: 10, Height: 20})
	b := New(Options{})
	if err := b.AddPage(A4).DrawImageHandle(b.AddImage(makeTestImage()), Rect{X: 3, Y: 4, Width: 10, Height: 20}); err != nil {
		t.Fatal(err)
	}
	if string(writeDoc(t, a)) != string(writeDoc(t, b)) {
		t.Error("a handle draw differs from DrawImage")
	}
}

func TestImageHandleSharesWithDrawImage(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	p.DrawImage(makeOpaqueImage(), Rect{Width: 1, Height: 1})
	if doc.AddImage(makeOpaqueImage()).x != doc.images[0] || len(doc.images) != 1 {
		t.Error("a handle of a drawn image should share its XObject")
	}
}

func TestAddJPEGHandle(t *testing.T) {
	doc := New(Options{})
	h, err := doc.AddJPEG(jpegBytes())
	if err != nil {
		t.Fatal(err)
	}
	if h.Width() != 4 || h.Height() != 4 {
		t.Errorf("jpeg handle is %dx%d, want 4x4", h.Width(), h.Height())
	}
	p := doc.AddPage(A4)
	for i := 0; i < 3; i++ {
		if err := p.DrawImageHandle(h, Rect{Width: 5, Height: 5}); err != nil {
			t.Fatal(err)
		}
	}
	if n := countImageObjects(t, doc); n != 1 {
		t.Errorf("emitted %d image objects, want 1", n)
	}
	if _, err := doc.AddJPEG([]byte("not a jpeg")); err == nil || !strings.Contains(err.Error(), "not a JPEG") {
		t.Errorf("AddJPEG(garbage) = %v, want the bad-SOI error", err)
	}
}

func TestImageHandleOfAnotherDocumentIsRefused(t *testing.T) {
	doc, other := New(Options{}), New(Options{})
	p := doc.AddPage(A4)
	foreign := other.AddImage(makeOpaqueImage())
	if err := p.DrawImageHandle(foreign, Rect{}); err == nil {
		t.Error("a handle of another document accepted")
	}
	if p.ContentLen() != 0 || len(p.usedImages) != 0 {
		t.Error("a refused draw left a trace on the page")
	}
}

func TestNilImageHandleIsRefused(t *testing.T) {
	p := New(Options{}).AddPage(A4)
	if err := p.DrawImageHandle(nil, Rect{}); err == nil {
		t.Error("nil handle accepted")
	}
	if p.ContentLen() != 0 {
		t.Error("a refused draw left a trace on the page")
	}
}

func TestDrawJPEGRefusalLeavesNoTrace(t *testing.T) {
	doc := New(Options{})
	p := doc.AddPage(A4)
	if err := p.DrawJPEG([]byte("not a jpeg"), Rect{Width: 1, Height: 1}); err == nil || !strings.Contains(err.Error(), "not a JPEG") {
		t.Errorf("DrawJPEG(garbage) = %v, want the bad-SOI error", err)
	}
	if p.ContentLen() != 0 || len(doc.images) != 0 {
		t.Error("a refused JPEG left a trace")
	}
}
