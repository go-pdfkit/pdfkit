// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"runtime"
	"strconv"
	"testing"
)

// streamDoc builds the documents the golden hashes below were taken from, on
// v0.26.0, which wrote the whole file into one buffer.
func streamDoc(opts Options) *Document {
	doc := New(opts)
	p := doc.AddPage(A4)
	p.DrawImage(makeTestImage(), Rect{Width: 10, Height: 10})
	p.DrawImage(makeOpaqueImage(), Rect{X: 20, Width: 10, Height: 10})
	_ = p.DrawJPEG(jpegBytes(), Rect{X: 40, Width: 10, Height: 10})
	p.SetAlpha(0.5, 0.5)
	p.AddLink(Rect{Width: 5, Height: 5}, "https://example.org/")
	q := doc.AddPage(Letter)
	q.Rectangle(Rect{Width: 3, Height: 3})
	q.Fill()
	return doc
}

func hashOf(t *testing.T, doc *Document) string {
	t.Helper()
	var b bytes.Buffer
	if err := doc.Write(&b); err != nil {
		t.Fatal(err)
	}
	s := sha256.Sum256(b.Bytes())
	return hex.EncodeToString(s[:])
}

func TestStreamedOutputIsByteIdenticalToTheBufferedWriter(t *testing.T) {
	id := [2][]byte{[]byte("0123456789abcdef"), []byte("fedcba9876543210")}
	for _, c := range []struct {
		name string
		opts Options
		want string
	}{
		{"classic", Options{Title: "t"}, "ea7c5adc9438d50e41684e5becb5e2768f7ceb9d7f27addf35bd30fdbf64cd78"},
		{"classic-compressed", Options{Compress: true}, "24e00e6934c4e223221e23355bb101181cc17c090e733cd79b26351140b9af63"},
		{"objstm", Options{ObjectStreams: true, Compress: true}, "f98d851ad88c0113ba2604cb9c82c21286a1c7faaa92d01aef33c2a93cbc9fd3"},
		{"objstm-plain", Options{ObjectStreams: true}, "1e2e71c621e02877d1b65285ee994fb3ec0a0b429d550ac9ba7c6c6ad192da3b"},
		{"fixed-id", Options{ID: id}, "de00e1900e9c5731d79d160e5ffbf2e47c5f0ea16a16ccc942107e67bafe049b"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := hashOf(t, streamDoc(c.opts))
			t.Logf("GOLD %s %s", c.name, got)
			if got != c.want {
				t.Errorf("hash = %s, want %s", got, c.want)
			}
		})
	}
}

// peakWriter forces a collection at every Write and records the most the heap
// holds above base: what the writer itself keeps alive while it works.
type peakWriter struct {
	base uint64
	peak uint64
	n    int64
}

func (p *peakWriter) Write(b []byte) (int, error) {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	if m.HeapAlloc > p.base && m.HeapAlloc-p.base > p.peak {
		p.peak = m.HeapAlloc - p.base
	}
	p.n += int64(len(b))
	runtime.KeepAlive(b) // the caller's buffer is what is being measured
	return len(b), nil
}

// bigJPEG is a valid JPEG header followed by size bytes of padding: DrawJPEG
// embeds the bytes untouched, which is how a small archive carries large
// images.
func bigJPEG(fill byte, size int) []byte {
	b := append(jpegBytes(), bytes.Repeat([]byte{fill}, size)...)
	return b
}

func TestWritingManyLargeImagesHoldsAboutOneObjectNotTheSum(t *testing.T) {
	const n, size = 8, 4 << 20
	for _, os := range []bool{false, true} {
		doc := New(Options{ObjectStreams: os})
		p := doc.AddPage(A4)
		for i := 0; i < n; i++ {
			if err := p.DrawJPEG(bigJPEG(byte(i+1), size), Rect{X: float64(i), Width: 1, Height: 1}); err != nil {
				t.Fatal(err)
			}
		}
		runtime.GC()
		var m0, m1 runtime.MemStats
		runtime.ReadMemStats(&m0)
		pw := &peakWriter{base: m0.HeapAlloc}
		if err := doc.Write(pw); err != nil {
			t.Fatal(err)
		}
		runtime.KeepAlive(doc) // the document stays live throughout, or the collector hides its share
		runtime.ReadMemStats(&m1)
		alloc := m1.TotalAlloc - m0.TotalAlloc
		t.Logf("objstm=%v: wrote %d bytes, peak above the document %d bytes, allocated %d bytes (one image is %d)",
			os, pw.n, pw.peak, alloc, size)
		if pw.n < n*size {
			t.Fatalf("wrote only %d bytes", pw.n)
		}
		if pw.peak > size {
			t.Errorf("peak heap while writing is %d bytes, want under one image (%d): the writer is holding the output", pw.peak, size)
		}
		if alloc > size {
			t.Errorf("writing allocated %d bytes, want under one image (%d)", alloc, size)
		}
	}
}

type failAfter struct{ left int }

var errFull = errors.New("disk full")

func (f *failAfter) Write(b []byte) (int, error) {
	if f.left <= 0 {
		return 0, errFull
	}
	if len(b) > f.left {
		n := f.left
		f.left = 0
		return n, errFull
	}
	f.left -= len(b)
	return len(b), nil
}

func TestWriteReturnsTheFirstWriteError(t *testing.T) {
	for _, os := range []bool{false, true} {
		// Every cut of the file, so the failure lands in each phase: header,
		// objects, object streams, cross-reference, trailer.
		var whole bytes.Buffer
		if err := streamDoc(Options{ObjectStreams: os}).Write(&whole); err != nil {
			t.Fatal(err)
		}
		for budget := 0; budget < whole.Len(); budget++ {
			err := streamDoc(Options{ObjectStreams: os}).Write(&failAfter{left: budget})
			if !errors.Is(err, errFull) {
				t.Fatalf("objstm=%v budget=%d of %d: err = %v, want the writer's error", os, budget, whole.Len(), err)
			}
		}
	}
	if err := streamDoc(Options{}).Write(io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestWriteErrorWhileManyObjectStreamsAreWritten(t *testing.T) {
	build := func() *Document {
		doc := New(Options{ObjectStreams: true})
		p := doc.AddPage(A4)
		for i := 0; i < objStmMaxObjects+100; i++ { // past one object stream
			p.AddLink(Rect{Width: 1, Height: 1}, "https://example.org/")
		}
		return doc
	}
	var whole bytes.Buffer
	if err := build().Write(&whole); err != nil {
		t.Fatal(err)
	}
	for budget := 0; budget < whole.Len(); budget += 509 {
		if err := build().Write(&failAfter{left: budget}); !errors.Is(err, errFull) {
			t.Fatalf("budget=%d of %d: err = %v, want the writer's error", budget, whole.Len(), err)
		}
	}
}

func TestImageStoredLen(t *testing.T) {
	doc := New(Options{})
	jpg, err := doc.AddJPEG(bigJPEG(1, 1000))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := jpg.StoredLen(), int64(len(jpeg0())+1000); got != want {
		t.Errorf("jpeg StoredLen = %d, want %d", got, want)
	}
	alpha := doc.AddImage(makeTestImage()) // carries a soft mask
	if got, want := alpha.StoredLen(), int64(len(alpha.x.data)+len(alpha.x.smask.data)); got != want || alpha.x.smask == nil {
		t.Errorf("alpha StoredLen = %d, want data + soft mask = %d", got, want)
	}
	opaque := doc.AddImage(makeOpaqueImage())
	if got := opaque.StoredLen(); got != int64(len(opaque.x.data)) {
		t.Errorf("opaque StoredLen = %d, want %d", got, len(opaque.x.data))
	}
	// The promise is the size actually written: the stream bytes in the file.
	var b bytes.Buffer
	doc.AddPage(A4).DrawImageHandle(jpg, Rect{Width: 1, Height: 1})
	if err := doc.Write(&b); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b.Bytes(), []byte("/Length "+strconv.Itoa(int(jpg.StoredLen())))) {
		t.Error("the file does not hold a stream of StoredLen bytes")
	}
}

func TestStreamObjectIsWrittenTheWayItIsEncoded(t *testing.T) {
	mk := func() *pdfStream {
		return &pdfStream{dict: newDict().set("Filter", pdfName("X")), data: []byte("DATA")}
	}
	var enc bytes.Buffer
	enc.WriteString("7 0 obj\n")
	mk().encodePDF(&enc)
	enc.WriteString("\nendobj\n")

	var out bytes.Buffer
	cw := &countWriter{w: &out}
	var scratch bytes.Buffer
	if off := writeIndirect(cw, &scratch, 7, mk()); off != 0 {
		t.Errorf("first object at offset %d, want 0", off)
	}
	if out.String() != enc.String() || cw.n != int64(out.Len()) {
		t.Errorf("streamed %q (%d counted), encoded %q", out.String(), cw.n, enc.String())
	}
}

func jpeg0() []byte { return jpegBytes() }
