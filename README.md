# pdfkit

[![CI](https://github.com/go-pdfkit/pdfkit/actions/workflows/ci.yml/badge.svg)](https://github.com/go-pdfkit/pdfkit/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-pdfkit/pdfkit.svg)](https://pkg.go.dev/github.com/go-pdfkit/pdfkit)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)
[![Coverage](https://img.shields.io/badge/coverage-100%25-brightgreen.svg)](#testing)

A pure-Go, **zero-C** PDF 1.7 **writer** with a Go-idiomatic API. It embeds
TrueType and OpenType/CFF fonts as subsetted composite fonts, draws vector
graphics and text, and places JPEG and raster images. Fonts are parsed and
shaped with [go-opentype](https://github.com/go-opentype/opentype); nothing
outside the Go standard library and our own pure-Go libraries is required.

`pdfkit` is the Go-native counterpart to our Ruby Prawn port
([go-ruby-prawn](https://github.com/go-ruby-prawn)); here the API is
Go-idiomatic rather than a gem port.

## Features

- **Documents** — catalog, page tree, cross-reference table and trailer; write
  to any `io.Writer`; optional Flate stream compression.
- **Graphics** — paths (move/line/cubic/rect/close), fill & stroke (nonzero and
  even-odd), DeviceGray/RGB/CMYK colour, line width/cap/join/miter/dash, CTM
  transforms (translate/scale/rotate/skew), clipping, `q`/`Q` state, and
  constant-alpha transparency via ExtGState.
- **Text** — embeds fonts as **Type0 (Identity-H)** composite fonts with glyph
  **subsetting**, a per-glyph `/W` width array and a `/ToUnicode` CMap for
  copy/paste. TrueType `glyf` → **FontFile2 / CIDFontType2** (compact subset with
  a `/CIDToGIDMap` stream); CFF/OpenType → **FontFile3 / CIDFontType0** with
  **CFF charstring subsetting** (only the used glyphs' charstrings are embedded).
  Char/word spacing, leading, render modes, a simple wrapping helper, and an
  optional **shaped-text** API (GSUB/GPOS via go-opentype) for Arabic/Indic/CJK.
- **Images** — JPEG embedded directly (DCTDecode); PNG and any `image.Image`
  rasterised as XObjects (FlateDecode) with an `/SMask` for alpha.
- **Pages** — standard sizes (A3/A4/A5/Letter/Legal/Tabloid), portrait/landscape,
  custom sizes; `Pt`/`Mm`/`In` unit helpers.
- **Widget bridge** — `Page.AddWidget` and `Page.AddWidgetVector` "print" a
  [go-widgets/toolkit](https://github.com/go-widgets/toolkit) widget tree onto a
  page. `AddWidget` rasterises the tree and places it as an image XObject —
  pixel-identical to the screen. `AddWidgetVector` instead emits PDF vector
  operators, so fills/strokes stay crisp and text stays selectable, including a
  TrueType-font widget label's own face embedded as real Type0 text.
- **Deterministic** — with the zero `Options`, output has no timestamps and a
  content-derived `/ID`, so identical inputs produce byte-identical PDFs.

## Install

```sh
go get github.com/go-pdfkit/pdfkit
```

## Usage

```go
package main

import (
	"os"

	"github.com/go-pdfkit/pdfkit"
)

func main() {
	ttf, _ := os.ReadFile("font.ttf")
	font, _ := pdfkit.LoadFont(ttf)

	doc := pdfkit.New(pdfkit.Options{Title: "Hello"})
	p := doc.AddPage(pdfkit.A4)

	p.SetFont(font, 24)
	p.Text(pdfkit.Mm(20), p.Height()-pdfkit.Mm(20), "Hello, pdfkit")

	p.SetStrokeColor(pdfkit.RGB8(0x0d, 0x94, 0x88))
	p.SetLineWidth(2)
	p.MoveTo(pdfkit.Mm(20), pdfkit.Mm(20))
	p.LineTo(pdfkit.Mm(190), pdfkit.Mm(20))
	p.Stroke()

	f, _ := os.Create("out.pdf")
	defer f.Close()
	doc.Write(f)
}
```

Shaped (complex-script) text uses `p.TextShaped(x, y, s, features...)`, which
runs the go-opentype shaper so Arabic, Indic and CJK position correctly. The
default `Text` path stays a simple left-to-right cmap mapping.

### Printing a widget tree

```go
import "github.com/go-widgets/toolkit"

root := toolkit.NewContainer(toolkit.NewBoxLayout())
btn := toolkit.NewButton("Submit", nil)
btn.Style = toolkit.ButtonProminent
root.AddWidget(btn)
root.AddWidget(toolkit.NewLabel("Status: ready"))

rect := pdfkit.Rect{X: pdfkit.Mm(20), Y: pdfkit.Mm(200), Width: pdfkit.Mm(80), Height: pdfkit.Mm(30)}

// Raster: pixel-identical to the screen, not selectable.
_ = p.AddWidget(root, rect, nil)

// Vector: crisp fills/strokes, selectable text (needs an embedded Font).
rect.Y -= pdfkit.Mm(40)
_ = p.AddWidgetVector(root, rect, &pdfkit.WidgetOptions{Font: font})
```

`WidgetOptions.Scale` sets the layout-pixels-per-point ratio (default
`DefaultWidgetScale`, 2); `WidgetOptions.Theme` selects the toolkit theme
(default `toolkit.DefaultLight()`).

## Testing

`GOWORK=off CGO_ENABLED=0 go test ./...` runs the suite at **exact 100%
statement coverage**. Correctness is checked against an independent parser:
generated documents are re-opened with [`rsc.io/pdf`](https://pkg.go.dev/rsc.io/pdf)
and their structure verified. The embedded TrueType subset is re-parsed with
go-opentype and each drawn glyph, resolved through the `/CIDToGIDMap`, is
confirmed contour-identical to the original; the embedded CFF subset is asserted
smaller than the whole `CFF` table and re-parsed so each kept glyph still renders
intact. Tests are deterministic and network-free: they use a synthesised
TrueType font, a synthesised CFF2 font and a bundled OFL OpenType/CFF font.

## Scope and limitations

- Both outline flavours are **subsetted**: TrueType `glyf` fonts via
  `go-opentype`'s `SubsetTrueType` (compact renumbering + a `/CIDToGIDMap`
  stream) and CFF/OpenType fonts via `SubsetCFF` (charstring subsetting,
  glyph numbering preserved). All subsetting and the font-descriptor metrics come
  straight from `go-opentype`; `pdfkit` keeps no private sfnt re-parse.
- A **CID-keyed CFF** or a **CFF2 (variable)** font cannot be charstring-subsetted
  by the preserve-numbering path, so it gracefully falls back to embedding the
  whole `CFF`/`CFF2` table.
- Encryption, tagged/PDF-A, forms and annotations are not yet implemented.

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright (c) 2026 the go-pdfkit/pdfkit
authors.
