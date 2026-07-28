// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit_test

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/go-pdfkit/pdfkit"
)

// Example builds a one-page document with embedded-font text and vector
// graphics, then writes it to a buffer. The output is deterministic: with the
// zero Options there are no timestamps and the /ID is content-derived.
func Example() {
	font, err := pdfkit.LoadFont(mustRead("testdata/SourceSerif4-Regular.otf"))
	if err != nil {
		panic(err)
	}

	doc := pdfkit.New(pdfkit.Options{Title: "Hello"})
	p := doc.AddPage(pdfkit.A4)

	p.SetFont(font, 24)
	_ = p.Text(pdfkit.Mm(20), p.Height()-pdfkit.Mm(20), "Hello, pdfkit")

	p.SetStrokeColor(pdfkit.RGB8(0x0d, 0x94, 0x88))
	p.SetLineWidth(2)
	p.MoveTo(pdfkit.Mm(20), pdfkit.Mm(20))
	p.LineTo(pdfkit.Mm(190), pdfkit.Mm(20))
	p.Stroke()

	var buf bytes.Buffer
	if err := doc.Write(&buf); err != nil {
		panic(err)
	}
	fmt.Println(strings.SplitN(buf.String(), "\n", 2)[0])
	// Output: %PDF-1.7
}

// ExampleDocument_multiPage lays out several pages of different sizes.
func ExampleDocument_multiPage() {
	doc := pdfkit.New(pdfkit.Options{})
	doc.AddPage(pdfkit.A4)
	doc.AddPage(pdfkit.Letter.Landscape())
	doc.AddPage(pdfkit.NewPageSize(pdfkit.In(4), pdfkit.In(6)))

	var buf bytes.Buffer
	_ = doc.Write(&buf)
	fmt.Println(bytes.Contains(buf.Bytes(), []byte("/Count 3")))
	// Output: true
}

func mustRead(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return b
}
