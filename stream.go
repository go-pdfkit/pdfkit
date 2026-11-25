// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"crypto/sha256"
	"hash"
	"io"
	"strconv"
)

// countWriter is the sink [Document.Write] puts the file through. It counts
// the bytes written, because a cross-reference entry is the byte offset of its
// object and that offset is known only by having written everything before it;
// it feeds them to a hash when the trailer /ID is to be derived from the body;
// and it keeps the first error, after which it writes nothing.
//
// Writing as it goes is what bounds memory: the file is never assembled in one
// buffer, so what is held at once is one object's dictionary, and a stream's
// data is handed to the writer straight from where the document already keeps
// it.
type countWriter struct {
	w   io.Writer
	n   int64
	err error
	h   hash.Hash // nil when the document carries its own /ID
}

// newCountWriter wraps w, hashing the body only if the /ID is to be derived
// from it.
func (d *Document) newCountWriter(w io.Writer) *countWriter {
	cw := &countWriter{w: w}
	if !d.idSupplied() {
		cw.h = sha256.New()
	}
	return cw
}

func (c *countWriter) Write(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	n, err := c.w.Write(p)
	c.n += int64(n)
	if c.h != nil {
		c.h.Write(p[:n])
	}
	c.err = err
	return n, err
}

// writeIndirect writes object number n, generation 0, with body v as
// "n 0 obj ... endobj" and returns the offset it starts at, which is what the
// cross-reference table or stream records for it. scratch is reused for the
// object's text; a stream's data is not copied into it.
func writeIndirect(cw *countWriter, scratch *bytes.Buffer, n int, v pdfValue) int {
	off := int(cw.n)
	scratch.Reset()
	scratch.WriteString(strconv.Itoa(n))
	scratch.WriteString(" 0 obj\n")
	if s, ok := v.(*pdfStream); ok {
		s.encodePDFHead(scratch)
		cw.Write(scratch.Bytes())
		cw.Write(s.data)
		scratch.Reset()
		scratch.WriteString("\nendstream")
	} else {
		v.encodePDF(scratch)
	}
	scratch.WriteString("\nendobj\n")
	cw.Write(scratch.Bytes())
	return off
}
