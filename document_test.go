// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

// failWriter fails after allowing n bytes through, to exercise write errors.
type failWriter struct{ remaining int }

func (w *failWriter) Write(p []byte) (int, error) {
	if len(p) <= w.remaining {
		w.remaining -= len(p)
		return len(p), nil
	}
	return 0, errors.New("boom")
}

func TestWriteNoPages(t *testing.T) {
	if err := New(Options{}).Write(&bytes.Buffer{}); err == nil {
		t.Fatal("expected error for empty document")
	}
}

func TestWriteError(t *testing.T) {
	doc := New(Options{})
	doc.AddPage(A4)
	if err := doc.Write(&failWriter{remaining: 0}); err == nil {
		t.Fatal("expected write error")
	}
}

func TestDeterministicOutput(t *testing.T) {
	build := func() []byte {
		doc := New(Options{})
		p := doc.AddPage(A4)
		p.SetFillColor(Gray{0.5})
		p.Rectangle(Rect{X: 1, Y: 2, Width: 3, Height: 4})
		p.Fill()
		var b bytes.Buffer
		if err := doc.Write(&b); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	if !bytes.Equal(build(), build()) {
		t.Fatal("output is not reproducible")
	}
}

func TestCustomID(t *testing.T) {
	doc := New(Options{ID: [2][]byte{{1, 2}, {3, 4}}})
	doc.AddPage(A4)
	var b bytes.Buffer
	if err := doc.Write(&b); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b.Bytes(), []byte("<0102>")) || !bytes.Contains(b.Bytes(), []byte("<0304>")) {
		t.Error("custom /ID not used")
	}
}

func TestInfoAndDates(t *testing.T) {
	now := time.Date(2026, 7, 28, 23, 15, 0, 0, time.FixedZone("CEST", 2*3600))
	doc := New(Options{Title: "Doc", Author: "Me", Producer: "custom", Now: func() time.Time { return now }})
	doc.AddPage(A4)
	var b bytes.Buffer
	if err := doc.Write(&b); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	for _, want := range []string{"(Doc)", "(Me)", "(custom)", "D:20260728231500+02'00'"} {
		if !bytes.Contains(b.Bytes(), []byte(want)) {
			t.Errorf("output missing %q\n%s", want, s)
		}
	}
}

func TestFormatPDFDateNegativeOffset(t *testing.T) {
	tm := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("X", -5*3600-30*60))
	if got := formatPDFDate(tm); got != "D:20260102030405-05'30'" {
		t.Errorf("date = %q", got)
	}
}

func TestDefaultProducer(t *testing.T) {
	doc := New(Options{})
	if doc.producer() != DefaultProducer {
		t.Errorf("producer = %q", doc.producer())
	}
	doc.AddPage(A4)
	var b bytes.Buffer
	_ = doc.Write(&b)
	if !bytes.Contains(b.Bytes(), []byte(DefaultProducer)) {
		t.Error("default producer not written")
	}
}

func TestRegisterFontDedup(t *testing.T) {
	doc := New(Options{})
	f, err := LoadFont(synthTTF(defaultSynth()))
	if err != nil {
		t.Fatal(err)
	}
	n1 := doc.registerFont(f)
	n2 := doc.registerFont(f)
	if n1 != "F0" || n2 != "F0" || len(doc.fonts) != 1 {
		t.Errorf("font not deduplicated: %q %q %d", n1, n2, len(doc.fonts))
	}
}
