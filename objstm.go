// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"io"
	"strconv"
)

// Object streams (PDF 1.5, ISO 32000-1 §7.5.7) let a writer pack many small
// indirect objects into one stream that is compressed as a whole, and the
// cross-reference stream (§7.5.8) that goes with them records, for each packed
// object, which object stream holds it and at what index. A document made of
// thousands of small dictionaries — one link annotation per hyperlink, say —
// shrinks by a large factor, because flate finds the repetition across objects
// that the classic layout (each object bare in the file, each with a 20-byte
// cross-reference line) exposes to no filter at all.
//
// This file is the writer's alternate back end, selected by
// Options.ObjectStreams. The classic table writer, emit in document.go, is
// left as it is so that its bytes stay stable for consumers that do not opt in.

// An object stream is closed and a new one started when the next object would
// take it past either bound. The count bound keeps each stream's offsets table
// and every in-stream index small (the cross-reference row needs one byte
// fewer for an index below 256, two fewer below 65536); the byte bound keeps a
// reader from inflating a very large stream to reach one object in it.
const (
	objStmMaxObjects = 1000
	objStmMaxBytes   = 1 << 20
)

// objStmEligible reports whether object body v may live inside an object
// stream. A stream may not (the specification forbids it, and a stream inside
// a stream would defeat the point), and neither may an object with a non-zero
// generation — moot here, since every object this writer emits is generation
// 0 (see objRef).
func objStmEligible(v pdfValue) bool {
	_, isStream := v.(*pdfStream)
	return !isStream
}

// objStmChunk is one object stream under construction: the numbers of the
// objects packed into it and their encoded bodies, in the same order, plus
// the running total of body bytes for the byte bound.
type objStmChunk struct {
	nums   []int
	bodies [][]byte
	size   int
}

// packObjectStreams groups the eligible objects of objs (object number n at
// objs[n-1]) into object streams in number order, closing a stream when the
// next object would take it past objStmMaxObjects or objStmMaxBytes. A
// stream is never empty, so a single object larger than the byte bound gets a
// stream of its own.
func packObjectStreams(objs []pdfValue) []*objStmChunk {
	var chunks []*objStmChunk
	var cur *objStmChunk
	for i, o := range objs {
		if !objStmEligible(o) {
			continue
		}
		var b bytes.Buffer
		o.encodePDF(&b)
		if cur == nil || len(cur.nums) >= objStmMaxObjects || cur.size+b.Len() > objStmMaxBytes {
			cur = &objStmChunk{}
			chunks = append(chunks, cur)
		}
		cur.nums = append(cur.nums, i+1)
		cur.bodies = append(cur.bodies, b.Bytes())
		cur.size += b.Len()
	}
	return chunks
}

// objectStream builds the /ObjStm object for chunk c: a header of
// "objnum offset" pairs, one per packed object, followed at /First by the
// object bodies themselves, each on its own line, offsets counted from
// /First. The data is flate-compressed when Options.Compress is set.
func (d *Document) objectStream(c *objStmChunk) *pdfStream {
	var hdr, body bytes.Buffer
	for i, n := range c.nums {
		if i > 0 {
			hdr.WriteByte(' ')
		}
		hdr.WriteString(strconv.Itoa(n))
		hdr.WriteByte(' ')
		hdr.WriteString(strconv.Itoa(body.Len()))
		body.Write(c.bodies[i])
		body.WriteByte('\n')
	}
	hdr.WriteByte('\n')

	dict := newDict()
	dict.set("Type", pdfName("ObjStm"))
	dict.set("N", pdfInt(len(c.nums)))
	dict.set("First", pdfInt(hdr.Len()))
	hdr.Write(body.Bytes())
	return &pdfStream{dict: dict, data: d.maybeFlate(dict, hdr.Bytes())}
}

// xrefEntry is one row of the cross-reference stream. Type 1 locates an object
// at byte offset field2 in the file (field3 is then its generation, always 0
// here); type 2 locates it at index field3 inside object stream number
// field2. The zero value is the type-0 free entry that object number 0 is.
type xrefEntry struct {
	typ            byte
	field2, field3 int
}

// byteWidth is the number of bytes needed to hold v in a cross-reference
// stream field: at least one, so that every row carries every field.
func byteWidth(v int) int {
	w := 1
	for v >>= 8; v > 0; v >>= 8 {
		w++
	}
	return w
}

// appendBE appends v to b as a width-byte big-endian integer.
func appendBE(b []byte, v, width int) []byte {
	for i := width - 1; i >= 0; i-- {
		b = append(b, byte(v>>(8*uint(i))))
	}
	return b
}

// encodeXref serialises entries (indexed by object number) as the fixed-width
// big-endian rows of a cross-reference stream and returns them with the /W
// array that describes the row layout: one byte for the type, then the
// narrowest widths that hold the largest offset and the largest in-stream
// index.
func encodeXref(entries []xrefEntry) ([]byte, pdfArray) {
	var max2, max3 int
	for _, e := range entries {
		max2 = max(max2, e.field2)
		max3 = max(max3, e.field3)
	}
	w2, w3 := byteWidth(max2), byteWidth(max3)
	rows := make([]byte, 0, len(entries)*(1+w2+w3))
	for _, e := range entries {
		rows = append(rows, e.typ)
		rows = appendBE(rows, e.field2, w2)
		rows = appendBE(rows, e.field3, w3)
	}
	return rows, pdfArray{pdfInt(1), pdfInt(w2), pdfInt(w3)}
}

// emitObjectStreams is the Options.ObjectStreams counterpart of emit. It
// writes the stream objects bare, in number order; packs every other object
// into object streams; and closes the file with a cross-reference stream
// whose dictionary doubles as the trailer, so no classic xref table or
// trailer keyword is written. Object numbers 1..len(bd.objs) are the
// document's own; the object streams take the numbers after them and the
// cross-reference stream the last one, which is what /Size counts.
func (d *Document) emitObjectStreams(w io.Writer, bd *builder, catalog, info objRef, hasInfo bool) error {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n")
	// A comment with high bytes marks the file as binary for transfer tools.
	buf.WriteString("%\xe2\xe3\xcf\xd3\n")

	chunks := packObjectStreams(bd.objs)
	xrefNum := len(bd.objs) + len(chunks) + 1
	entries := make([]xrefEntry, xrefNum+1) // by object number; 0 stays free

	for i, o := range bd.objs {
		if !objStmEligible(o) {
			entries[i+1] = xrefEntry{typ: 1, field2: writeIndirect(&buf, i+1, o)}
		}
	}
	for ci, c := range chunks {
		num := len(bd.objs) + ci + 1
		for idx, n := range c.nums {
			entries[n] = xrefEntry{typ: 2, field2: num, field3: idx}
		}
		entries[num] = xrefEntry{typ: 1, field2: writeIndirect(&buf, num, d.objectStream(c))}
	}

	// The cross-reference stream lists itself, at the offset startxref names.
	xrefOff := buf.Len()
	entries[xrefNum] = xrefEntry{typ: 1, field2: xrefOff}
	rows, widths := encodeXref(entries)

	xref := newDict()
	xref.set("Type", pdfName("XRef"))
	xref.set("Size", pdfInt(xrefNum+1))
	xref.set("W", widths)
	xref.set("Root", catalog)
	if hasInfo {
		xref.set("Info", info)
	}
	id := d.documentID(buf.Bytes())
	xref.set("ID", pdfArray{pdfHexString(id[0]), pdfHexString(id[1])})
	writeIndirect(&buf, xrefNum, &pdfStream{dict: xref, data: d.maybeFlate(xref, rows)})

	buf.WriteString("startxref\n")
	buf.WriteString(strconv.Itoa(xrefOff))
	buf.WriteString("\n%%EOF\n")

	_, err := w.Write(buf.Bytes())
	return err
}
