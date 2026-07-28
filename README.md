# pdfkit

[![CI](https://github.com/go-pdfkit/pdfkit/actions/workflows/ci.yml/badge.svg)](https://github.com/go-pdfkit/pdfkit/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-pdfkit/pdfkit.svg)](https://pkg.go.dev/github.com/go-pdfkit/pdfkit)
[![Go Report Card](https://goreportcard.com/badge/github.com/go-pdfkit/pdfkit)](https://goreportcard.com/report/github.com/go-pdfkit/pdfkit)
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
  copy/paste. TrueType `glyf` → **FontFile2 / CIDFontType2**; CFF/OpenType →
  **FontFile3 / CIDFontType0**. Char/word spacing, leading, render modes, a
  simple wrapping helper, and an optional **shaped-text** API (GSUB/GPOS via
  go-opentype) for Arabic/Indic/CJK.
- **Images** — JPEG embedded directly (DCTDecode); PNG and any `image.Image`
  rasterised as XObjects (FlateDecode) with an `/SMask` for alpha.
- **Pages** — standard sizes (A3/A4/A5/Letter/Legal/Tabloid), portrait/landscape,
  custom sizes; `Pt`/`Mm`/`In` unit helpers.
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

## Testing

`GOWORK=off CGO_ENABLED=0 go test ./...` runs the suite at **exact 100%
statement coverage**. Correctness is checked against an independent parser:
generated documents are re-opened with [`rsc.io/pdf`](https://pkg.go.dev/rsc.io/pdf)
and their structure verified, and the embedded TrueType subset is re-parsed with
go-opentype to confirm it still contains the glyphs that were drawn. Tests are
deterministic and network-free: they use a synthesised TrueType font and a
bundled OFL OpenType/CFF font.

## Scope and limitations

- CFF/OpenType fonts embed their **whole `CFF ` table** (charstring subsetting is
  not yet implemented); TrueType fonts are fully subsetted.
- `go-opentype` exposes no raw table bytes, units-per-em, glyf/loca arrays or a
  subsetting export, so `pdfkit` reparses the sfnt container it is handed and
  implements TrueType subsetting itself.
- Encryption, tagged/PDF-A, forms and annotations are out of scope for v0.1.

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright (c) 2026 the go-pdfkit/pdfkit
authors.
