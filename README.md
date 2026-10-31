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
  A layout engine that places its own glyphs draws them with
  **`Page.ShowGlyphs`** — glyph ids at the positions it computed, each with the
  text it stands for — and hands over the fonts it already parsed with
  **`NewFont(*opentype.Font)`**.
- **Gradients** — axial (type 2) and **radial (type 3)** shadings with any
  number of stops, painted through the clip (`Page.Shade`).
- **Images** — JPEG embedded directly (DCTDecode); PNG and any `image.Image`
  rasterised as XObjects (FlateDecode) with an `/SMask` for alpha, the colour
  samples written straight (not premultiplied), as a soft mask without
  `/Matte` requires.
  **Pixel-identical bitmaps are shared**: each image is content-addressed by its
  *uncompressed* samples, so a repeat is embedded — and compressed — once per
  document and every placement, on any page, references the one XObject.
- **Form XObjects** (ISO 32000-1 §8.10) — `Document.NewForm(bbox)` returns a
  `Form`, drawn with every `Page` method (paths, colours, alpha, shadings,
  images, text, other forms) in its own form space. `Page.DrawForm(f, rect)`
  maps its box onto a rectangle, and `Page.PaintForm(f, a, b, c, d, e, f)`
  places it under any matrix. A form painted any number of times, on any page,
  is one stream: a logo, an emoji or a formula used thirty times costs thirty
  `Do` operators. Forms may paint forms made before or after them. A cycle is
  refused (`ErrFormCycle`). A form's content belongs to the tagged sequence it
  is painted in (§14.7.4.2), so `BeginTag` and tagged links inside a form write
  nothing.
- **Pages** — standard sizes (A3/A4/A5/Letter/Legal/Tabloid), portrait/landscape,
  custom sizes; `Pt`/`Mm`/`In` unit helpers.
- **Widget bridge**, in the subpackage `pdfkit/widget`: `widget.Add` and
  `widget.AddVector` "print" a
  [go-widgets/toolkit](https://github.com/go-widgets/toolkit) widget tree onto a
  page.
  - `Add` rasterises the tree and places it as an image XObject,
    pixel-identical to the screen.
  - `AddVector` instead emits PDF vector operators, so fills and strokes stay
    crisp and text stays selectable. A TrueType-font widget label is embedded
    in its own face as real Type0 text.
  - Importing `pdfkit` alone does not link the toolkit: 196 packages instead of
    403, and a test keeps it that way.
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
runs the go-opentype shaper so Arabic, Indic and CJK position correctly.
The run is written as one `TJ` array per baseline segment — a numeric
correction only where shaping departs from the font's own advances (kerning,
marks), none for plain text — and unchanged font/colour state is not
rewritten, so a page of prose is a few tens of KB compressed, not hundreds. The
default `Text` path stays a simple left-to-right cmap mapping.

Text a layout engine has already shaped and placed goes through
`p.ShowGlyphs(x, y, glyphs)`: each `pdfkit.Glyph` is a glyph id, its origin's
offset from the run origin and the text it stands for (the `/ToUnicode`
entry). The same `TJ` encoding applies — a correction only where a position
departs from the font's own advance by a thousandth of an em or more, computed
from absolute positions so that the pen never drifts. go-webengine's
`pdfcanvas` draws a laid-out page this way.

### Printing a widget tree

```go
import (
	"github.com/go-pdfkit/pdfkit/widget"
	"github.com/go-widgets/toolkit"
)

root := toolkit.NewContainer(toolkit.NewBoxLayout())
btn := toolkit.NewButton("Submit", nil)
btn.Style = toolkit.ButtonProminent
root.AddWidget(btn)
root.AddWidget(toolkit.NewLabel("Status: ready"))

rect := pdfkit.Rect{X: pdfkit.Mm(20), Y: pdfkit.Mm(200), Width: pdfkit.Mm(80), Height: pdfkit.Mm(30)}

// Raster: pixel-identical to the screen, not selectable.
_ = widget.Add(p, root, rect, nil)

// Vector: crisp fills/strokes, selectable text (needs an embedded Font).
rect.Y -= pdfkit.Mm(40)
_ = widget.AddVector(p, root, rect, &widget.Options{Font: font})
```

`Options.Scale` sets the layout-pixels-per-point ratio (default
`widget.DefaultScale`, 2). `Options.Theme` selects the toolkit theme (default
`toolkit.DefaultLight()`).

### Upgrading from v0.19 to v0.20

The widget bridge moved out of the root package. Make these replacements:

| v0.19 (`pdfkit`) | v0.20 (`pdfkit/widget`) |
| --- | --- |
| `p.AddWidget(root, rect, opts)` | `widget.Add(p, root, rect, opts)` |
| `p.AddWidgetVector(root, rect, opts)` | `widget.AddVector(p, root, rect, opts)` |
| `pdfkit.WidgetOptions` | `widget.Options` |
| `pdfkit.DefaultWidgetScale` | `widget.DefaultScale` |

Nothing else changed. A program that never used the bridge only gets smaller.

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
- **Link annotations are written here**: `AddLink`, `AddNamedDest` and
  `AddNamedLink` put a clickable `/Link` with a URI or a named destination over
  a rectangle. **Text (sticky note) annotations** too, since v0.16.0:
  `Page.AddTextAnnotation` puts a note with its contents, author, subject,
  icon, colour and opacity on a page — go-slides carries a slide's speaker
  notes in them. No other annotation type is written, and nothing here draws
  an appearance stream — `go-pdfkit/render` does that when reading.
- **Bookmarks** (`AddOutlineItem`) nest by level; since v0.16.0
  `AddOutlineItemWith` styles a title bold or italic and points it at a place on
  the page (`[page /XYZ x y null]`) instead of fitting the whole page.
- **Replacement text** (after v0.16.0): `Page.BeginActualText(s)` …
  `EndMarkedContent()` wrap what is drawn in a `/Span` with `/ActualText`, the
  text extraction, copying and search use instead of the glyphs. An empty
  string hides decorations from the text (numbers in a margin, a numbered disc
  after a line of code): Poppler's `pdftotext` then leaves them out. Spans left
  open are closed when the page is written.
- **Encryption is not written here.** It is not missing from the organisation:
  `go-pdfkit/reader` reads RC4-40 through AES-256 and writes AES-128 and
  AES-256 with permissions and both passwords.
- **Forms are not written here** — `go-pdfkit/forms` reads, fills and flattens
  an AcroForm, and `go-pdfkit/xfa` reads the other kind.
- **Tagged PDF and PDF/UA** (after v0.17.0): `Document.Structure()` returns
  the root of the logical structure (ISO 32000-1, 14.7), and `Add` builds the
  tree in reading order with the standard types (`StructSect`, `StructH1`,
  `StructP`, `StructL`/`StructLI`/`StructLbl`/`StructLBody`, `StructTable`,
  `StructFigure` and `StructFormula` with `Alt`, `StructCode`, `StructLink`…).
  `Page.BeginTag(e)` … `EndMarkedContent()` ties what is drawn to an element
  (a marked-content sequence with an MCID, mapped back through the parent
  tree). `Page.BeginArtifact` marks backgrounds, page numbers and ornaments,
  and `Page.AddTaggedLink` puts a link annotation under its Link element with
  its `/Contents`. Notes get an `Annot` element of their own, and elements
  with no content are left out. After v0.18.0, `StructElem.Attributes` carries
  the standard structure attributes (§14.8.5): a `TH`'s `Scope`, which
  PDF/UA asks for, a cell's `RowSpan`… `Options.Lang`, `DisplayDocTitle` and `PDFUA`
  (the XMP metadata with `dc:title` and `pdfuaid:part`) complete what PDF/UA-1
  asks of the file; a small tagged document passes veraPDF's PDF/UA-1
  profile. Tagging every drawing is the caller's part. An untagged document
  is written as before.
- **PDF/A is not written here**, and is not implemented anywhere in the
  organisation. `go-pdfkit/ops` preserves a structure tree it is given around
  the pages that survive.

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright (c) 2026 the go-pdfkit/pdfkit
authors.
