// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"compress/zlib"
)

// flateCompress returns data wrapped in a zlib (RFC 1950) stream, the byte
// format a PDF /FlateDecode filter expects. Writing to a bytes.Buffer never
// fails, so the errors from the zlib writer are structurally unreachable and
// discarded.
func flateCompress(data []byte) []byte {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return buf.Bytes()
}
