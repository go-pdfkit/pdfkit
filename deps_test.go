// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"os/exec"
	"strings"
	"testing"
)

// forbiddenDeps are the packages a program that only writes PDFs must not
// link. v0.19.0's widget bridge pulled go-widgets/toolkit, and with it
// collab, richdoc, JPEG 2000 and JBIG2, into every importer of this package:
// the go-llmflow gateway grew from 33.8 to 38.0 MB. The bridge now lives in
// pdfkit/widget.
var forbiddenDeps = []string{
	"github.com/go-widgets/toolkit",
	"github.com/go-crdt/",
	"github.com/go-richdoc/",
	"github.com/go-images/jpeg2000",
	"github.com/tannevaled/gobig2",
}

// rootDeps lists the packages the root package links, tests excluded, as
// the go command resolves them.
func rootDeps(t *testing.T, pkg string) []string {
	t.Helper()
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go command on PATH to list the import graph")
	}
	out, err := exec.Command(gobin, "list", "-deps", pkg).CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
	}
	return strings.Fields(string(out))
}

func forbidden(deps []string) []string {
	var bad []string
	for _, d := range deps {
		for _, f := range forbiddenDeps {
			if d == f || strings.HasPrefix(d, f) {
				bad = append(bad, d)
			}
		}
	}
	return bad
}

// TestRootDoesNotLinkTheToolkit fails if importing pdfkit links
// go-widgets/toolkit (or what it pulls) again. Its control: the widget
// subpackage does link the toolkit, so the measurement can see it.
func TestRootDoesNotLinkTheToolkit(t *testing.T) {
	if bad := forbidden(rootDeps(t, ".")); len(bad) > 0 {
		t.Errorf("importing github.com/go-pdfkit/pdfkit links %v: keep the toolkit bridge in pdfkit/widget", bad)
	}
	if len(forbidden(rootDeps(t, "./widget"))) == 0 {
		t.Error("control: pdfkit/widget does not link the toolkit either, so this test cannot see it")
	}
}
