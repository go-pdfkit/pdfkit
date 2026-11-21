// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"compress/zlib"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/go-pdfkit/reader"
)

// makePDF writes a one-page PDF with the reader's own writer: an oracle that
// is not this package's. page is called with the writer and returns the page
// dictionary's entries; pagesExtra goes on the /Pages node.
func makePDF(t testing.TB, page func(w *reader.Writer) reader.Dict, pagesExtra reader.Dict) []byte {
	t.Helper()
	w := reader.NewWriter("1.7")
	cat, pages, pg := w.Reserve(), w.Reserve(), w.Reserve()
	d := page(w)
	d["Type"] = reader.Name("Page")
	d["Parent"] = pages
	w.Put(pg, d)
	p := reader.Dict{"Type": reader.Name("Pages"), "Kids": reader.Array{pg}, "Count": reader.Integer(1)}
	for k, v := range pagesExtra {
		p[k] = v
	}
	w.Put(pages, p)
	w.Put(cat, reader.Dict{"Type": reader.Name("Catalog"), "Pages": pages})
	out, err := w.Finish(reader.Dict{"Root": cat})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func box(v ...float64) reader.Array {
	a := make(reader.Array, len(v))
	for i, x := range v {
		a[i] = reader.Real(x)
	}
	return a
}

func stream(s string) *reader.Stream {
	return &reader.Stream{Dict: reader.Dict{}, Raw: []byte(s)}
}

// figure is a page with a crop box smaller than its media box, an image in its
// resources (an indirect stream), and a font dictionary that refers to itself.
func figure(t testing.TB) []byte {
	return makePDF(t, func(w *reader.Writer) reader.Dict {
		img := w.Add(&reader.Stream{Dict: reader.Dict{
			"Type": reader.Name("XObject"), "Subtype": reader.Name("Image"),
			"Width": reader.Integer(2), "Height": reader.Integer(1),
			"ColorSpace": reader.Name("DeviceGray"), "BitsPerComponent": reader.Integer(8),
			"StructParent": reader.Integer(3), "Thumb": w.Add(stream("thumbnail")),
		}, Raw: []byte{0, 255}})
		loop := w.Reserve() // a reference that is only itself: it does not resolve
		w.Put(loop, loop)
		font := w.Reserve()
		w.Put(font, reader.Dict{"Type": reader.Name("Font"), "Subtype": reader.Name("Type1"),
			"BaseFont": reader.Name("Helvetica"), "Self": font})
		content := w.Add(stream("0 0 1 rg 10 20 100 50 re f /Im1 Do"))
		return reader.Dict{
			"MediaBox": box(0, 0, 300, 200),
			"CropBox":  box(10, 20, 110, 70),
			"Resources": reader.Dict{
				"XObject": reader.Dict{"Im1": img},
				"Font":    reader.Dict{"F1": font},
				"ProcSet": reader.Array{reader.Name("PDF"), reader.Name("Text")},
				"Properties": reader.Dict{"On": reader.Bool(true), "Title": reader.String("hi"),
					"Gone": reader.Ref{Num: 9999}, "Loop": loop},
			},
			"Group":    reader.Dict{"S": reader.Name("Transparency"), "CS": reader.Name("DeviceRGB")},
			"Contents": content,
		}
	}, nil)
}

// outPage opens the written file and returns its first page's resources.
func outPage(t *testing.T, out []byte) (*reader.Document, reader.Dict, reader.Dict) {
	t.Helper()
	doc, err := reader.Open(out)
	if err != nil {
		t.Fatalf("the output does not open: %v", err)
	}
	pg, err := doc.Page(1)
	if err != nil {
		t.Fatal(err)
	}
	res, _ := doc.GetDict(pg, "Resources")
	return doc, pg, res
}

func formOf(t *testing.T, doc *reader.Document, res reader.Dict, name reader.Name) *reader.Stream {
	t.Helper()
	xo, ok := doc.GetDict(res, "XObject")
	if !ok {
		t.Fatal("the page has no /XObject resources")
	}
	o, err := doc.Resolve(xo.Get(name))
	if err != nil {
		t.Fatal(err)
	}
	s, ok := reader.ToStream(o)
	if !ok {
		t.Fatalf("/%s is not a stream", name)
	}
	return s
}

func floats(t *testing.T, doc *reader.Document, o reader.Object) []float64 {
	t.Helper()
	o, _ = doc.Resolve(o)
	arr, ok := reader.ToArray(o)
	if !ok {
		t.Fatalf("not an array: %v", o)
	}
	var v []float64
	for _, e := range arr {
		f, _ := reader.ToFloat(e)
		v = append(v, f)
	}
	return v
}

func equalFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestImportedPageIsAFormXObject(t *testing.T) {
	d := New(Options{})
	f, err := d.ImportPDFPage(figure(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	// The default box is the crop box clamped to the media box (what pdfTeX
	// takes), 100 x 50 at (10, 20): not the 300 x 200 media box.
	if got := f.BBox(); got != (Rect{X: 10, Y: 20, Width: 100, Height: 50}) {
		t.Fatalf("form box = %+v", got)
	}
	pg := d.AddPage(A4)
	if err := pg.DrawForm(f, Rect{X: 72, Y: 72, Width: 200, Height: 100}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := d.Write(&buf); err != nil {
		t.Fatal(err)
	}
	doc, page, res := outPage(t, buf.Bytes())
	form := formOf(t, doc, res, "Fm0")
	if n, _ := reader.ToName(form.Dict.Get("Subtype")); n != "Form" {
		t.Errorf("/Subtype = %v", form.Dict.Get("Subtype"))
	}
	if got := floats(t, doc, form.Dict.Get("BBox")); !equalFloats(got, []float64{10, 20, 110, 70}) {
		t.Errorf("/BBox = %v", got)
	}
	if !isNull(form.Dict.Get("Matrix")) {
		t.Errorf("an unrotated page got a /Matrix: %v", form.Dict.Get("Matrix"))
	}
	body, _, err := doc.DecodeStream(form)
	if err != nil || !strings.Contains(string(body), "10 20 100 50 re f") {
		t.Errorf("the page content was not kept: %q, %v", body, err)
	}
	// The resources came with what they reach: the image, and the font that
	// refers to itself (written once, not followed forever).
	fres, ok := doc.GetDict(form.Dict, "Resources")
	if !ok {
		t.Fatal("the form has no /Resources")
	}
	img := formOf(t, doc, fres, "Im1")
	if w, _ := reader.ToInt(img.Dict.Get("Width")); w != 2 || !bytes.Equal(img.Raw, []byte{0, 255}) {
		t.Errorf("image not copied: width %v raw %v", img.Dict.Get("Width"), img.Raw)
	}
	if !isNull(img.Dict.Get("Thumb")) || !isNull(img.Dict.Get("StructParent")) {
		t.Errorf("the source's thumbnail and structure keys were followed: %v", img.Dict)
	}
	props, _ := doc.GetDict(fres, "Properties")
	if b, _ := props.Get("On").(reader.Bool); !bool(b) {
		t.Errorf("a boolean did not survive: %v", props)
	}
	if s, _ := props.Get("Title").(reader.String); string(s) != "hi" {
		t.Errorf("a string did not survive: %v", props)
	}
	if o, _ := doc.Resolve(props.Get("Gone")); !isNull(o) {
		t.Errorf("a dangling reference became %v, want null", o)
	}
	if o, _ := doc.Resolve(props.Get("Loop")); !isNull(o) {
		t.Errorf("a reference to itself became %v, want null", o)
	}
	if ps, _ := doc.Resolve(fres.Get("ProcSet")); len(ps.(reader.Array)) != 2 {
		t.Errorf("ProcSet = %v", ps)
	}
	if g, ok := doc.GetDict(form.Dict, "Group"); !ok || g.Get("S") != reader.Name("Transparency") {
		t.Errorf("the transparency group was lost: %v", form.Dict)
	}
	fonts, _ := doc.GetDict(fres, "Font")
	fo, err := doc.Resolve(fonts.Get("F1"))
	if err != nil {
		t.Fatal(err)
	}
	fd, _ := reader.ToDict(fo)
	if isNull(fd.Get("Self")) {
		t.Errorf("the font lost its self reference: %v", fd)
	}
	if ref, ok := fd.Get("Self").(reader.Ref); !ok || ref != fonts.Get("F1") {
		t.Errorf("the self reference does not lead back to the font: %v vs %v", fd.Get("Self"), fonts.Get("F1"))
	}
	// Placement: the box (10,20,100,50) onto (72,72,200,100) is a scale of 2
	// and a translation that puts (10,20) at (72,72): e = 72-10*2, f = 72-20*2.
	pc, _, _ := doc.DecodeStream(streamOf(t, doc, page))
	if !strings.Contains(string(pc), "2 0 0 2 52 32 cm") || !strings.Contains(string(pc), "/Fm0 Do") {
		t.Errorf("placement not as computed:\n%s", pc)
	}
}

func streamOf(t *testing.T, doc *reader.Document, page reader.Dict) *reader.Stream {
	t.Helper()
	o, err := doc.Resolve(page.Get("Contents"))
	if err != nil {
		t.Fatal(err)
	}
	s, ok := reader.ToStream(o)
	if !ok {
		t.Fatalf("/Contents is not one stream: %T", o)
	}
	return s
}

func TestImportedRotatedPage(t *testing.T) {
	src := makePDF(t, func(w *reader.Writer) reader.Dict {
		return reader.Dict{"MediaBox": box(0, 0, 200, 100), "Rotate": reader.Integer(90),
			"Resources": reader.Dict{}, "Contents": w.Add(stream("0 0 50 50 re f"))}
	}, nil)
	d := New(Options{})
	info, err := d.PDFPageInfo(src, 1)
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 100 || info.Height != 200 {
		t.Fatalf("a 200x100 page turned 90 degrees measures %+v, want 100x200", info)
	}
	f, err := d.ImportPDFPage(src, 1)
	if err != nil {
		t.Fatal(err)
	}
	pg := d.AddPage(A4)
	_ = pg.DrawForm(f, Rect{X: 0, Y: 0, Width: 100, Height: 200})
	var buf bytes.Buffer
	if err := d.Write(&buf); err != nil {
		t.Fatal(err)
	}
	doc, page, res := outPage(t, buf.Bytes())
	form := formOf(t, doc, res, "Fm0")
	if got := floats(t, doc, form.Dict.Get("Matrix")); !equalFloats(got, []float64{0, -1, 1, 0, 0, 0}) {
		t.Errorf("/Matrix = %v", got)
	}
	// The turned box spans y from -200 to 0, so reaching y 0..200 takes a
	// translation of 200.
	pc, _, _ := doc.DecodeStream(streamOf(t, doc, page))
	if !strings.Contains(string(pc), "1 0 0 1 0 200 cm") {
		t.Errorf("placement of a turned page:\n%s", pc)
	}
}

func TestImportInheritsTheMediaBox(t *testing.T) {
	src := makePDF(t, func(w *reader.Writer) reader.Dict {
		return reader.Dict{"Contents": w.Add(stream("0 0 1 1 re f"))}
	}, reader.Dict{"MediaBox": box(0, 0, 50, 40), "Resources": reader.Dict{}})
	info, err := New(Options{}).PDFPageInfo(src, 1)
	if err != nil || info.Width != 50 || info.Height != 40 {
		t.Fatalf("%+v, %v", info, err)
	}
}

func TestACropBoxLargerThanTheMediaBoxIsClamped(t *testing.T) {
	src := makePDF(t, func(w *reader.Writer) reader.Dict {
		return reader.Dict{"MediaBox": box(0, 0, 100, 100), "CropBox": box(-50, -50, 70, 500),
			"Resources": reader.Dict{}, "Contents": w.Add(stream(""))}
	}, nil)
	info, err := New(Options{}).PDFPageInfo(src, 1)
	if err != nil || info.Width != 70 || info.Height != 100 {
		t.Fatalf("%+v, %v", info, err)
	}
}

func TestSeveralContentStreamsAreJoined(t *testing.T) {
	src := makePDF(t, func(w *reader.Writer) reader.Dict {
		return reader.Dict{"MediaBox": box(0, 0, 10, 10), "Resources": reader.Dict{},
			"Contents": reader.Array{w.Add(stream("q 1 0 0 rg")), w.Add(stream("0 0 5 5 re f Q"))}}
	}, nil)
	d := New(Options{Compress: true})
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
	if err != nil || !strings.Contains(string(body), "q 1 0 0 rg\n0 0 5 5 re f Q") {
		t.Errorf("joined content = %q, %v", body, err)
	}
}

func TestTheSamePageIsImportedOnce(t *testing.T) {
	src := figure(t)
	d := New(Options{})
	a, err := d.ImportPDFPage(src, 1)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := d.ImportPDFPage(append([]byte(nil), src...), 1)
	if a != b {
		t.Fatal("two imports of the same bytes made two forms")
	}
	_ = d.AddPage(A4).DrawForm(a, Rect{Width: 10, Height: 10})
	_ = d.AddPage(A4).DrawForm(b, Rect{Width: 10, Height: 10})
	var buf bytes.Buffer
	if err := d.Write(&buf); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(buf.String(), "/Subtype /Form"); n != 1 {
		t.Errorf("%d form XObjects in the file, want 1", n)
	}
}

func TestImportWritesTheSameBytesEveryTime(t *testing.T) {
	build := func() []byte {
		d := New(Options{})
		f, err := d.ImportPDFPage(figure(t), 1)
		if err != nil {
			t.Fatal(err)
		}
		_ = d.AddPage(A4).DrawForm(f, Rect{Width: 10, Height: 10})
		var buf bytes.Buffer
		_ = d.Write(&buf)
		return buf.Bytes()
	}
	if !bytes.Equal(build(), build()) {
		t.Error("two writes of the same import differ")
	}
}

func TestImportOutputPassesQpdf(t *testing.T) {
	qpdf, err := exec.LookPath("qpdf")
	if err != nil {
		t.Skip("no qpdf on PATH")
	}
	d := New(Options{Compress: true})
	f, _ := d.ImportPDFPage(figure(t), 1)
	_ = d.AddPage(A4).DrawForm(f, Rect{Width: 10, Height: 10})
	path := filepath.Join(t.TempDir(), "o.pdf")
	var buf bytes.Buffer
	if err := d.Write(&buf); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(qpdf, "--check", path).CombinedOutput(); err != nil {
		t.Errorf("qpdf --check: %v\n%s", err, out)
	}
}

func TestImportRefusals(t *testing.T) {
	good := figure(t)
	cases := map[string][]byte{
		"not a pdf":   []byte("hello, world"),
		"empty":       nil,
		"truncated":   good[:len(good)/2],
		"no mediabox": makePDF(t, func(w *reader.Writer) reader.Dict { return reader.Dict{"Resources": reader.Dict{}} }, nil),
		"empty box": makePDF(t, func(w *reader.Writer) reader.Dict {
			return reader.Dict{"MediaBox": box(0, 0, 0, 10), "Resources": reader.Dict{}}
		}, nil),
		"nan box": makePDF(t, func(w *reader.Writer) reader.Dict {
			return reader.Dict{"MediaBox": box(0, 0, 1e300, 10), "Resources": reader.Dict{}}
		}, nil),
	}
	for name, src := range cases {
		d := New(Options{})
		if f, err := d.ImportPDFPage(src, 1); err == nil || f != nil {
			t.Errorf("%s: imported (%v)", name, err)
		}
		if len(d.forms) != 0 {
			t.Errorf("%s: a refused import left a form behind", name)
		}
	}
	if _, err := New(Options{}).ImportPDFPage(good, 2); err == nil {
		t.Error("page 2 of a one-page file imported")
	}
	if _, err := New(Options{}).ImportPDFPage(good, 0); err == nil {
		t.Error("page 0 imported")
	}
}

func TestAnEncryptedSourceIsRefused(t *testing.T) {
	w := reader.NewWriter("1.7")
	w.Encrypt(reader.Encryption{OwnerPassword: "owner", Permissions: reader.PermPrint}) // empty user password: it opens
	cat, pages, pg := w.Reserve(), w.Reserve(), w.Reserve()
	w.Put(pg, reader.Dict{"Type": reader.Name("Page"), "Parent": pages, "MediaBox": box(0, 0, 10, 10), "Resources": reader.Dict{}})
	w.Put(pages, reader.Dict{"Type": reader.Name("Pages"), "Kids": reader.Array{pg}, "Count": reader.Integer(1)})
	w.Put(cat, reader.Dict{"Type": reader.Name("Catalog"), "Pages": pages})
	out, err := w.Finish(reader.Dict{"Root": cat})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{}).ImportPDFPage(out, 1); !errors.Is(err, ErrImportEncrypted) {
		t.Fatalf("an encrypted source: %v, want ErrImportEncrypted", err)
	}
}

// ---- bombs: each asserts a REFUSAL, and that it cost little memory ----

// allocated runs f and returns how many bytes the process allocated meanwhile.
func allocated(f func()) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	f()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

func TestBombManyObjectsIsRefused(t *testing.T) {
	src := makePDF(t, func(w *reader.Writer) reader.Dict {
		fonts := reader.Dict{}
		for i := 0; i < 5000; i++ {
			fonts[reader.Name("F"+itoa(i))] = w.Add(reader.Dict{"Type": reader.Name("Font")})
		}
		return reader.Dict{"MediaBox": box(0, 0, 10, 10), "Resources": reader.Dict{"Font": fonts}}
	}, nil)
	d := New(Options{MaxImportObjects: 100})
	if _, err := d.ImportPDFPage(src, 1); !errors.Is(err, ErrImportLimit) {
		t.Fatalf("5000 objects against a limit of 100: %v", err)
	}
	// The same file passes under a limit that fits it: the refusal is the limit's.
	if _, err := New(Options{MaxImportObjects: 6000}).ImportPDFPage(src, 1); err != nil {
		t.Fatalf("control: %v", err)
	}
}

func TestBombManyStreamBytesIsRefused(t *testing.T) {
	src := makePDF(t, func(w *reader.Writer) reader.Dict {
		big := w.Add(&reader.Stream{Dict: reader.Dict{"Subtype": reader.Name("Image")}, Raw: make([]byte, 1<<20)})
		return reader.Dict{"MediaBox": box(0, 0, 10, 10), "Resources": reader.Dict{"XObject": reader.Dict{"I": big}}}
	}, nil)
	if _, err := New(Options{MaxImportBytes: 1 << 10}).ImportPDFPage(src, 1); !errors.Is(err, ErrImportLimit) {
		t.Fatalf("a 1 MiB stream against a limit of 1 KiB: %v", err)
	}
	if _, err := New(Options{MaxImportBytes: 4 << 20}).ImportPDFPage(src, 1); err != nil {
		t.Fatalf("control: %v", err)
	}
}

// A few kilobytes of Flate that inflate to 256 MiB, as one of two content
// streams (the case that must be inflated to be joined).
func TestBombInflatedContentIsRefused(t *testing.T) {
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	chunk := bytes.Repeat([]byte{' '}, 1<<20)
	for i := 0; i < 256; i++ {
		_, _ = zw.Write(chunk)
	}
	_ = zw.Close()
	if z.Len() > 1<<20 {
		t.Fatalf("the bomb is %d bytes: not a bomb", z.Len())
	}
	src := makePDF(t, func(w *reader.Writer) reader.Dict {
		bomb := &reader.Stream{Dict: reader.Dict{"Filter": reader.Name("FlateDecode")}, Raw: z.Bytes()}
		return reader.Dict{"MediaBox": box(0, 0, 10, 10), "Resources": reader.Dict{},
			"Contents": reader.Array{w.Add(stream("q")), w.Add(bomb)}}
	}, nil)
	var err error
	got := allocated(func() { _, err = New(Options{MaxImportBytes: 1 << 20}).ImportPDFPage(src, 1) })
	if !errors.Is(err, ErrImportLimit) {
		t.Fatalf("256 MiB of inflated content against 1 MiB: %v", err)
	}
	if got > 16<<20 {
		t.Errorf("refusing it allocated %d MiB: the bound is applied after the damage", got>>20)
	}
}

func TestBombDeepNestingIsRefused(t *testing.T) {
	var deep reader.Object = reader.Integer(1)
	for i := 0; i < 5000; i++ {
		deep = reader.Array{deep}
	}
	src := makePDF(t, func(w *reader.Writer) reader.Dict {
		return reader.Dict{"MediaBox": box(0, 0, 10, 10), "Resources": reader.Dict{"Deep": deep}}
	}, nil)
	if _, err := New(Options{}).ImportPDFPage(src, 1); err == nil {
		t.Fatal("5000 levels of nesting imported")
	}
}

func TestACycleOfReferencesTerminates(t *testing.T) {
	src := makePDF(t, func(w *reader.Writer) reader.Dict {
		a, b := w.Reserve(), w.Reserve()
		w.Put(a, reader.Dict{"Next": b})
		w.Put(b, reader.Dict{"Next": a})
		return reader.Dict{"MediaBox": box(0, 0, 10, 10), "Resources": reader.Dict{"Properties": reader.Dict{"A": a}}}
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
}

func itoa(i int) string { return strconv.Itoa(i) }

// FuzzImportPDFPage feeds arbitrary bytes to the importer. It must never
// panic, hang, or allocate without bound, and a page it accepts must write.
// The seeds are the fixtures above and the committed corpus under
// testdata/fuzz/FuzzImportPDFPage.
func FuzzImportPDFPage(f *testing.F) {
	f.Add(figure(f))
	f.Add(makePDF(f, func(w *reader.Writer) reader.Dict {
		return reader.Dict{"MediaBox": box(0, 0, 200, 100), "Rotate": reader.Integer(270), "Resources": reader.Dict{},
			"Contents": reader.Array{w.Add(stream("q")), w.Add(stream("Q"))}}
	}, reader.Dict{"CropBox": box(1, 1, 50, 50)}))
	f.Add([]byte("%PDF-1.4\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		d := New(Options{MaxImportObjects: 2000, MaxImportBytes: 8 << 20})
		form, err := d.ImportPDFPage(data, 1)
		if err != nil {
			if form != nil {
				t.Fatal("a form with an error")
			}
			return
		}
		if !(form.BBox().Width > 0 && form.BBox().Height > 0) {
			t.Fatalf("an accepted page with box %+v", form.BBox())
		}
		_ = d.AddPage(A4).DrawForm(form, Rect{Width: 100, Height: 100})
		var buf bytes.Buffer
		if err := d.Write(&buf); err != nil {
			t.Fatalf("an accepted page does not write: %v", err)
		}
		if _, err := reader.Open(buf.Bytes()); err != nil {
			t.Fatalf("what was written does not open: %v", err)
		}
	})
}
