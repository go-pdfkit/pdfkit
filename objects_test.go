// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"testing"
)

func enc(v pdfValue) string {
	var b bytes.Buffer
	v.encodePDF(&b)
	return b.String()
}

func TestEncodeNamePrimitives(t *testing.T) {
	cases := []struct {
		v    pdfValue
		want string
	}{
		{pdfName("Type"), "/Type"},
		{pdfName("A#B/C(D)"), "/A#23B#2FC#28D#29"}, // reserved chars escaped
		{pdfInt(-12), "-12"},
		{pdfReal(1.5), "1.5"},
		{pdfReal(100), "100"},
		{pdfString("a(b)\\\n\r\tz"), `(a\(b\)\\\n\r\tz)`},
		{pdfHexString([]byte{0x00, 0xff, 0x1a}), "<00FF1A>"},
		{pdfArray{pdfInt(1), pdfName("X")}, "[1 /X]"},
		{objRef(7), "7 0 R"},
	}
	for _, c := range cases {
		if got := enc(c.v); got != c.want {
			t.Errorf("encode %#v = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestDictSetReplaceAndOrder(t *testing.T) {
	d := newDict()
	d.set("A", pdfInt(1)).set("B", pdfInt(2)).set("A", pdfInt(3)) // replace A in place
	if got, want := enc(d), "<</A 3 /B 2>>"; got != want {
		t.Errorf("dict = %q, want %q", got, want)
	}
}

func TestStreamEncodeSetsLength(t *testing.T) {
	s := &pdfStream{dict: newDict(), data: []byte("abcd")}
	got := enc(s)
	if want := "<</Length 4>>\nstream\nabcd\nendstream"; got != want {
		t.Errorf("stream = %q, want %q", got, want)
	}
}

func TestFtoa(t *testing.T) {
	if ftoa(0.1) != "0.1" || ftoa(2) != "2" || ftoa(-3.25) != "-3.25" {
		t.Errorf("ftoa mismatch: %q %q %q", ftoa(0.1), ftoa(2), ftoa(-3.25))
	}
}
