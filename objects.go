// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"strconv"
)

// pdfValue is any object that can be serialised into PDF syntax. The concrete
// implementations model the handful of COS object types pdfkit emits: names,
// numbers, strings, arrays, dictionaries, streams and indirect references.
type pdfValue interface {
	encodePDF(*bytes.Buffer)
}

// ftoa formats a float in PDF's plain decimal notation: no exponent (PDF has no
// scientific form) and no redundant trailing zeros. Whole values render without
// a fractional part, so 100.0 becomes "100".
func ftoa(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// pdfName is a PDF name object such as /Type. It is written with a leading
// slash and the characters that are illegal in a name escaped as #xx.
type pdfName string

func (n pdfName) encodePDF(b *bytes.Buffer) {
	b.WriteByte('/')
	for i := 0; i < len(n); i++ {
		c := n[i]
		if c < '!' || c > '~' || c == '#' || c == '/' || c == '%' ||
			c == '(' || c == ')' || c == '<' || c == '>' || c == '[' ||
			c == ']' || c == '{' || c == '}' {
			b.WriteByte('#')
			const hex = "0123456789ABCDEF"
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xf])
			continue
		}
		b.WriteByte(c)
	}
}

// pdfInt is a PDF integer object.
type pdfInt int64

func (n pdfInt) encodePDF(b *bytes.Buffer) {
	b.WriteString(strconv.FormatInt(int64(n), 10))
}

// pdfReal is a PDF real (floating-point) object.
type pdfReal float64

func (n pdfReal) encodePDF(b *bytes.Buffer) {
	b.WriteString(ftoa(float64(n)))
}

// pdfString is a PDF literal string, written in parentheses with the reserved
// bytes escaped. It carries arbitrary bytes (used for /Producer, dates, ...).
type pdfString []byte

func (s pdfString) encodePDF(b *bytes.Buffer) {
	b.WriteByte('(')
	for _, c := range s {
		switch c {
		case '(', ')', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte(')')
}

// pdfHexString is a PDF hexadecimal string, written between angle brackets. It
// is used for the trailer /ID and other binary payloads.
type pdfHexString []byte

func (s pdfHexString) encodePDF(b *bytes.Buffer) {
	const hex = "0123456789ABCDEF"
	b.WriteByte('<')
	for _, c := range s {
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0xf])
	}
	b.WriteByte('>')
}

// pdfArray is a PDF array object.
type pdfArray []pdfValue

func (a pdfArray) encodePDF(b *bytes.Buffer) {
	b.WriteByte('[')
	for i, v := range a {
		if i > 0 {
			b.WriteByte(' ')
		}
		v.encodePDF(b)
	}
	b.WriteByte(']')
}

// pdfDict is a PDF dictionary. Keys are held in insertion order so the encoded
// bytes are deterministic (map iteration order is not).
type pdfDict struct {
	keys []string
	vals []pdfValue
}

// newDict returns an empty dictionary ready for set.
func newDict() *pdfDict { return &pdfDict{} }

// set adds or replaces the entry for key. Replacing keeps the original slot so
// ordering stays stable across edits.
func (d *pdfDict) set(key string, v pdfValue) *pdfDict {
	for i, k := range d.keys {
		if k == key {
			d.vals[i] = v
			return d
		}
	}
	d.keys = append(d.keys, key)
	d.vals = append(d.vals, v)
	return d
}

func (d *pdfDict) encodePDF(b *bytes.Buffer) {
	b.WriteString("<<")
	for i, k := range d.keys {
		if i > 0 {
			b.WriteByte(' ')
		}
		pdfName(k).encodePDF(b)
		b.WriteByte(' ')
		d.vals[i].encodePDF(b)
	}
	b.WriteString(">>")
}

// pdfStream is an indirect stream object: a dictionary followed by raw bytes.
// The /Length entry is filled in from the data when the object is written.
type pdfStream struct {
	dict *pdfDict
	data []byte
}

func (s *pdfStream) encodePDF(b *bytes.Buffer) {
	s.dict.set("Length", pdfInt(len(s.data)))
	s.dict.encodePDF(b)
	b.WriteString("\nstream\n")
	b.Write(s.data)
	b.WriteString("\nendstream")
}

// objRef is an indirect reference to object number n (generation 0), written as
// "n 0 R".
type objRef int

func (r objRef) encodePDF(b *bytes.Buffer) {
	b.WriteString(strconv.Itoa(int(r)))
	b.WriteString(" 0 R")
}
