// Copyright (c) the go-pdfkit/pdfkit authors.
// SPDX-License-Identifier: BSD-3-Clause

package pdfkit

import (
	"bytes"
	"testing"
)

// Subject and Keywords reach the information dictionary as text strings.
func TestInfoSubjectAndKeywords(t *testing.T) {
	doc := New(Options{Subject: "A study of widgets", Keywords: "widgets, gadgets, gizmos"})
	doc.AddPage(A4)
	var b bytes.Buffer
	if err := doc.Write(&b); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/Subject (A study of widgets)", "/Keywords (widgets, gadgets, gizmos)"} {
		if !bytes.Contains(b.Bytes(), []byte(want)) {
			t.Errorf("output missing %q\n%s", want, b.String())
		}
	}
}

// A non-ASCII Subject/Keywords value is encoded UTF-16BE, like Title/Author.
func TestInfoSubjectKeywordsAccentedUTF16(t *testing.T) {
	doc := New(Options{Subject: "Résumé", Keywords: "clé, référence"})
	doc.AddPage(A4)
	var b bytes.Buffer
	if err := doc.Write(&b); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b.Bytes(), []byte("<FEFF")) {
		t.Errorf("accented Subject/Keywords were not written UTF-16BE\n%s", b.String())
	}
	if bytes.Contains(b.Bytes(), []byte("Résumé")) {
		t.Errorf("accented Subject leaked into the PDF as raw UTF-8")
	}
}
