// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestActualTextSpan(t *testing.T) {
	_, p, f := loadSynth(t)
	p.SetFont(f, 12)
	p.BeginActualText("")
	if err := p.Text(10, 10, "Hi"); err != nil {
		t.Fatal(err)
	}
	p.EndMarkedContent()
	p.EndMarkedContent() // stray: writes nothing
	p.BeginActualText("é")
	p.BeginActualText("x")
	c := content(p) // closes the two left open
	for _, want := range []string{"/Span <</ActualText ()>> BDC\n", "/Span <</ActualText <FEFF00E9>>> BDC\n", "/Span <</ActualText (x)>> BDC\n"} {
		if !strings.Contains(c, want) {
			t.Errorf("content lacks %q\n%s", want, c)
		}
	}
	if b, e := strings.Count(c, "BDC"), strings.Count(c, "EMC"); b != 3 || e != 3 {
		t.Errorf("%d BDC, %d EMC: unbalanced\n%s", b, e, c)
	}
	if i, j := strings.Index(c, "BDC"), strings.Index(c, "BT"); i > j {
		t.Error("the span must enclose the text object")
	}
}

// What an empty ActualText encloses is drawn but not extracted: checked
// with Poppler's pdftotext when it is installed.
func TestActualTextHiddenFromExtraction(t *testing.T) {
	bin, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("pdftotext not installed")
	}
	doc, p, f := loadSynth(t)
	p.SetFont(f, 12)
	if err := p.Text(10, 700, "Hi"); err != nil {
		t.Fatal(err)
	}
	p.BeginActualText("")
	if err := p.Text(10, 600, "HiHi"); err != nil {
		t.Fatal(err)
	}
	p.EndMarkedContent()
	var b bytes.Buffer
	if err := doc.Write(&b); err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(in, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, in, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	if text := strings.TrimSpace(string(out)); text != "Hi" {
		t.Errorf("pdftotext extracted %q, want only the text outside the span", text)
	}
}
