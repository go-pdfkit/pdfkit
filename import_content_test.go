// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"compress/zlib"
	"errors"
	"math/rand"
	"strings"
	"testing"

	"github.com/go-pdfkit/reader"
)

func deflate(b []byte) []byte {
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	_, _ = w.Write(b)
	_ = w.Close()
	return z.Bytes()
}

// contents builds a page whose /Contents is the given streams (one stream when
// there is one, an array otherwise).
func contents(t testing.TB, ss ...*reader.Stream) []byte {
	return makePDF(t, func(w *reader.Writer) reader.Dict {
		d := reader.Dict{"MediaBox": box(0, 0, 10, 10), "Resources": reader.Dict{}}
		if len(ss) == 1 {
			d["Contents"] = w.Add(ss[0])
			return d
		}
		var arr reader.Array
		for _, s := range ss {
			arr = append(arr, w.Add(s))
		}
		d["Contents"] = arr
		return d
	}, nil)
}

func filtered(filter string, raw []byte) *reader.Stream {
	return &reader.Stream{Dict: reader.Dict{"Filter": reader.Name(filter)}, Raw: raw}
}

// written imports src and returns the decoded content of the form it makes.
func written(t *testing.T, d *Document, src []byte) string {
	t.Helper()
	f, err := d.ImportPDFPage(src, 1)
	if err != nil {
		t.Fatal(err)
	}
	_ = d.AddPage(A4).DrawForm(f, Rect{Width: 10, Height: 10})
	var buf bytes.Buffer
	if err := d.Write(&buf); err != nil {
		t.Fatal(err)
	}
	doc, _, res := outPage(t, buf.Bytes())
	body, _, err := doc.DecodeStream(formOf(t, doc, res, "Fm0"))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// noPanic fails the test, as an assertion, if f panics: a refusal must be an
// error, never a crash that a recover further in happens to turn into one.
func noPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s: panicked: %v", what, r)
		}
	}()
	f()
}

func TestRefusalsNeverPanic(t *testing.T) {
	for _, src := range [][]byte{[]byte("not a pdf"), nil, figure(t)[:100]} {
		noPanic(t, "PDFPageInfo", func() {
			if info, err := New(Options{}).PDFPageInfo(src, 1); err == nil || info != (PageInfo{}) {
				t.Errorf("%+v, %v", info, err)
			}
		})
		noPanic(t, "ImportPDFPage", func() {
			if f, err := New(Options{}).ImportPDFPage(src, 1); err == nil || f != nil {
				t.Errorf("%v, %v", f, err)
			}
		})
	}
	// A deflate header with no body: the refusal is the reader's, not a nil.
	noPanic(t, "a Flate stream that is not Flate", func() {
		_, err := New(Options{}).ImportPDFPage(contents(t, filtered("FlateDecode", []byte("garbage!")), stream("q")), 1)
		if err == nil {
			t.Error("imported")
		}
	})
}

func TestRefusalsSayWhy(t *testing.T) {
	noBox := makePDF(t, func(w *reader.Writer) reader.Dict { return reader.Dict{"Resources": reader.Dict{}} }, nil)
	threeNumbers := makePDF(t, func(w *reader.Writer) reader.Dict {
		return reader.Dict{"MediaBox": box(0, 0, 10), "Resources": reader.Dict{}}
	}, nil)
	loop := makePDF(t, func(w *reader.Writer) reader.Dict {
		a := w.Reserve()
		w.Put(a, a)
		return reader.Dict{"MediaBox": a, "Resources": reader.Dict{}}
	}, nil)
	loopInBox := makePDF(t, func(w *reader.Writer) reader.Dict {
		a := w.Reserve()
		w.Put(a, a)
		return reader.Dict{"MediaBox": reader.Array{reader.Integer(0), reader.Integer(0), reader.Integer(10), a}, "Resources": reader.Dict{}}
	}, nil)
	for name, src := range map[string][]byte{"no box": noBox, "three numbers": threeNumbers, "loop": loop, "loop in box": loopInBox} {
		_, err := New(Options{}).PDFPageInfo(src, 1)
		if err == nil || !strings.Contains(err.Error(), "MediaBox") {
			t.Errorf("%s: %v, want a refusal that names /MediaBox", name, err)
		}
	}
	for name, c := range map[string]struct {
		src  []byte
		page int
	}{"garbage": {[]byte("not a pdf at all"), 1}, "page 2": {figure(t), 2}} {
		_, err := New(Options{}).PDFPageInfo(c.src, c.page)
		if err == nil || strings.Contains(err.Error(), "MediaBox") || strings.Contains(err.Error(), "runtime error") {
			t.Errorf("%s: %v, want the reader's own refusal", name, err)
		}
	}
}

func TestRotations(t *testing.T) {
	for rot, want := range map[int64][]float64{180: {-1, 0, 0, -1, 0, 0}, 270: {0, 1, -1, 0, 0, 0}, -90: {0, 1, -1, 0, 0, 0}, 450: {0, -1, 1, 0, 0, 0}} {
		src := makePDF(t, func(w *reader.Writer) reader.Dict {
			return reader.Dict{"MediaBox": box(0, 0, 200, 100), "Rotate": reader.Integer(rot), "Resources": reader.Dict{}}
		}, nil)
		d := New(Options{})
		f, err := d.ImportPDFPage(src, 1)
		if err != nil {
			t.Fatal(err)
		}
		_ = d.AddPage(A4).DrawForm(f, Rect{Width: 10, Height: 10})
		var buf bytes.Buffer
		if err := d.Write(&buf); err != nil {
			t.Fatal(err)
		}
		doc, _, res := outPage(t, buf.Bytes())
		if got := floats(t, doc, formOf(t, doc, res, "Fm0").Dict.Get("Matrix")); !equalFloats(got, want) {
			t.Errorf("/Rotate %d: /Matrix = %v, want %v", rot, got, want)
		}
	}
}

func TestAContentsArrayWithAnUnreadableMember(t *testing.T) {
	// One member refers to itself (it does not resolve), one is a one-element
	// Filter array: both are read past, the page still imports.
	src := makePDF(t, func(w *reader.Writer) reader.Dict {
		a := w.Reserve()
		w.Put(a, a)
		s := &reader.Stream{Dict: reader.Dict{"Filter": reader.Array{reader.Name("FlateDecode")}}, Raw: deflate([]byte("q Q"))}
		return reader.Dict{"MediaBox": box(0, 0, 10, 10), "Resources": reader.Dict{},
			"Contents": reader.Array{a, w.Add(s), w.Add(stream("0 0 1 1 re"))}}
	}, nil)
	if got := written(t, New(Options{}), src); !strings.HasPrefix(got, "q Q\n0 0 1 1 re") {
		t.Errorf("content = %q", got)
	}
}

func TestADisjointCropBoxIsIgnored(t *testing.T) {
	src := makePDF(t, func(w *reader.Writer) reader.Dict {
		return reader.Dict{"MediaBox": box(0, 0, 100, 80), "CropBox": box(200, 200, 300, 300), "Resources": reader.Dict{}}
	}, nil)
	info, err := New(Options{}).PDFPageInfo(src, 1)
	if err != nil || info.Width != 100 || info.Height != 80 {
		t.Fatalf("%+v, %v", info, err)
	}
}

func TestContentLimits(t *testing.T) {
	big := bytes.Repeat([]byte("q Q "), 150) // 600 bytes
	hex := func(b []byte) []byte {
		const digits = "0123456789abcdef"
		out := make([]byte, 0, 2*len(b)+1)
		for _, c := range b {
			out = append(out, digits[c>>4], digits[c&15])
		}
		return append(out, '>')
	}
	cases := []struct {
		name string
		src  []byte
		opt  Options
	}{
		{"one raw stream over the limit", contents(t, stream(string(big))), Options{MaxImportBytes: 100}},
		{"second raw stream over what is left", contents(t, stream(string(big)), stream(string(big))), Options{MaxImportBytes: 1000}},
		{"decoded hex over the limit", contents(t, filtered("ASCIIHexDecode", hex(big)), filtered("ASCIIHexDecode", hex(big))), Options{MaxImportBytes: 500}},
		{"many content streams", func() []byte {
			ss := make([]*reader.Stream, 200)
			for i := range ss {
				ss[i] = stream("q Q")
			}
			return contents(t, ss...)
		}(), Options{MaxImportObjects: 100}},
	}
	for _, c := range cases {
		if _, err := New(c.opt).ImportPDFPage(c.src, 1); !errors.Is(err, ErrImportLimit) {
			t.Errorf("%s: %v, want ErrImportLimit", c.name, err)
		}
	}
	// The same pages pass under limits that fit them, so each refusal above is
	// the limit's and not the fixture's.
	for _, c := range cases {
		if _, err := New(Options{}).ImportPDFPage(c.src, 1); err != nil {
			t.Errorf("%s: control: %v", c.name, err)
		}
	}
}

func TestContentDecoding(t *testing.T) {
	hexStream := filtered("ASCIIHexDecode", []byte("712051>")) // "q Q"
	if got := written(t, New(Options{}), contents(t, hexStream, stream("0 0 1 1 re"))); !strings.HasPrefix(got, "q Q\n0 0 1 1 re") {
		t.Errorf("hex stream joined as %q", got)
	}
	// Flate that is not Flate, a header with no body, and a body cut short.
	if _, err := New(Options{}).ImportPDFPage(contents(t, filtered("FlateDecode", []byte("garbage!")), stream("q")), 1); err == nil {
		t.Error("a Flate stream that is not Flate imported")
	}
	if _, err := New(Options{}).ImportPDFPage(contents(t, filtered("FlateDecode", []byte{0x78, 0x9c}), stream("q")), 1); err == nil {
		t.Error("a Flate stream with no body imported")
	}
	if _, err := New(Options{}).ImportPDFPage(contents(t, filtered("NoSuchDecode", []byte("x")), stream("q")), 1); err == nil {
		t.Error("an unknown filter imported")
	}
	r := rand.New(rand.NewSource(1))
	var text bytes.Buffer
	for text.Len() < 20000 {
		text.WriteString(strings.Repeat("q ", r.Intn(5)+1) + "0 0 1 1 re f ")
	}
	z := deflate(text.Bytes())
	got := written(t, New(Options{}), contents(t, filtered("FlateDecode", z[:len(z)*6/10]), stream("Q")))
	if len(got) == 0 || !strings.HasPrefix(got, "q ") || !strings.HasSuffix(got, "Q\n") {
		t.Errorf("a truncated stream gave %d bytes %q...", len(got), got[:min(len(got), 20)])
	}
}
