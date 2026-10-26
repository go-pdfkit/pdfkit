// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

// Package widget prints a go-widgets/toolkit widget tree onto a pdfkit page,
// so any UI composed from toolkit widgets can be "printed" to a PDF. Two
// levels are offered:
//
//   - Add rasterises the tree through a painter.PixelPainter and places the
//     result as an image XObject. It renders every widget faithfully (the
//     same pixels a window would show) but the output is a bitmap.
//   - AddVector runs the tree through a painter.Painter that emits PDF vector
//     operators (fills, strokes, rounded rects and text-show operators)
//     instead of pixels, so text stays selectable and lines stay crisp. It
//     uses a caller-supplied embedded Font for text; widgets drawing through
//     the toolkit's built-in bitmap font reach the painter's Text primitive,
//     which becomes real PDF text, and a TrueType/OpenType toolkit font (a
//     painter.Face) is embedded itself.
//
// The bridge lives in its own package so that a program which only writes
// PDFs (import "github.com/go-pdfkit/pdfkit") does not link go-widgets/toolkit
// and everything it pulls in. Until v0.20.0 it was Page.AddWidget and
// Page.AddWidgetVector in the root package.
//
// The toolkit lays a scene out in integer painter units (pixels) with absolute
// child bounds, then every widget's Draw targets a single painter over the
// whole canvas — so one painter, positioned once over the target rectangle,
// renders the entire tree.
package widget

import (
	"errors"
	"image"
	"math"

	"github.com/go-pdfkit/pdfkit"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// DefaultScale is the number of layout pixels per PDF point used when
// Options.Scale is unset. A value of 2 lays the tree out at twice the point
// resolution, giving crisper raster output and finer layout rounding.
const DefaultScale = 2.0

// glyphPx is the layout height, in painter pixels, of the toolkit's built-in
// 5x7 bitmap font (the height widgets lay text out against). The vector
// painter sizes PDF text from it so a run roughly fills the same box.
const glyphPx = 7.0

// ErrEmptyRect is returned when the target rectangle scales to a
// zero-or-negative pixel canvas, so there is nothing to lay out or render.
var ErrEmptyRect = errors.New("pdfkit/widget: target rectangle is empty at the given scale")

// ErrNoFont is returned by AddVector when no Font is supplied: selectable
// vector text needs an embedded font.
var ErrNoFont = errors.New("pdfkit/widget: AddVector requires Options.Font for selectable text")

// Options configures how a widget tree is transferred onto a page. The nil
// *Options is valid and selects every default.
type Options struct {
	// Theme is the toolkit theme the widgets paint with. When nil,
	// toolkit.DefaultLight() is used.
	Theme *toolkit.Theme

	// Scale is the number of layout pixels per PDF point. Values <= 0 default
	// to DefaultScale. Larger values give a crisper raster (Add) and finer
	// layout rounding for both paths.
	Scale float64

	// Font is the embedded font used for selectable text on the vector path
	// (AddVector). It is ignored by the raster Add path.
	Font *pdfkit.Font
}

// resolve fills in the theme, scale and font defaults for a possibly nil
// *Options.
func resolve(opts *Options) (*toolkit.Theme, float64, *pdfkit.Font) {
	var theme *toolkit.Theme
	var scale float64
	var font *pdfkit.Font
	if opts != nil {
		theme = opts.Theme
		scale = opts.Scale
		font = opts.Font
	}
	if theme == nil {
		theme = toolkit.DefaultLight()
	}
	if scale <= 0 {
		scale = DefaultScale
	}
	return theme, scale, font
}

// pixelCanvas resolves the integer pixel dimensions the tree lays out into
// for rect at scale, returning ErrEmptyRect when either dimension is not
// positive.
func pixelCanvas(rect pdfkit.Rect, scale float64) (w, h int, err error) {
	w = int(math.Round(rect.Width * scale))
	h = int(math.Round(rect.Height * scale))
	if w <= 0 || h <= 0 {
		return 0, 0, ErrEmptyRect
	}
	return w, h, nil
}

// Add lays root out to fill rect (in points) on page p, renders the whole
// widget tree to an RGBA raster through a painter.PixelPainter, and places
// that raster as an image XObject at rect. It is the "print any UI to PDF"
// path: every widget renders exactly as it would on screen. opts may be nil.
func Add(p *pdfkit.Page, root toolkit.Widget, rect pdfkit.Rect, opts *Options) error {
	theme, scale, _ := resolve(opts)
	w, h, err := pixelCanvas(rect, scale)
	if err != nil {
		return err
	}
	root.SetBounds(toolkit.Rect{X: 0, Y: 0, W: w, H: h})
	buf := make([]byte, 4*w*h)
	pp := painter.NewPixelPainter(buf, w, h)
	root.Draw(pp, theme)
	img := &image.RGBA{
		Pix:    buf,
		Stride: 4 * w,
		Rect:   image.Rect(0, 0, w, h),
	}
	p.DrawImage(img, rect)
	return nil
}

// AddVector lays root out to fill rect (in points) on page p and renders the
// widget tree with PDF vector operators, so text stays selectable and edges
// stay crisp. Text is drawn with opts.Font, which must be non-nil: a nil or
// fontless opts returns ErrNoFont. The whole tree is clipped to rect.
func AddVector(p *pdfkit.Page, root toolkit.Widget, rect pdfkit.Rect, opts *Options) error {
	theme, scale, font := resolve(opts)
	if font == nil {
		return ErrNoFont
	}
	w, h, err := pixelCanvas(rect, scale)
	if err != nil {
		return err
	}
	root.SetBounds(toolkit.Rect{X: 0, Y: 0, W: w, H: h})
	vp := &vectorPainter{
		page: p,
		rx:   rect.X,
		ry:   rect.Y,
		rh:   rect.Height,
		sx:   rect.Width / float64(w),
		sy:   rect.Height / float64(h),
		w:    w,
		h:    h,
		font: font,
	}
	p.Save()
	p.Rectangle(rect)
	p.Clip()
	p.EndPath()
	root.Draw(vp, theme)
	p.Restore()
	return nil
}

// vectorPainter implements painter.Painter (and painter.Clipper) by emitting
// PDF content-stream operators. Painter coordinates are top-left-origin
// pixels; PDF user space is bottom-left-origin points, so every call maps x
// through fx and y through fy (which also flips the axis).
type vectorPainter struct {
	page *pdfkit.Page

	rx, ry float64      // target rect lower-left corner, in points
	rh     float64      // target rect height, in points
	sx, sy float64      // points per painter pixel (x and y)
	w, h   int          // canvas size, in painter pixels
	font   *pdfkit.Font // fallback font for the plain Text primitive (bitmap-font runs)

	// faceFonts memoises the *Font embedded for each painter.Face drawn
	// through TextFace, so a TrueType-font widget tree embeds each face once
	// and re-uses it. A face whose bytes fail to parse is cached as a nil
	// entry so the parse is not retried on every run.
	faceFonts map[painter.Face]*pdfkit.Font

	clipDepth int // balanced q/Q depth pushed by PushClip
}

// faceFont returns the embedded *Font for face, loading (and memoising) it
// from the face's own sfnt bytes on first use. It returns nil when those
// bytes do not parse, so the caller can fall back to the painter's plain-Text
// font.
func (v *vectorPainter) faceFont(face painter.Face) *pdfkit.Font {
	if f, ok := v.faceFonts[face]; ok {
		return f
	}
	if v.faceFonts == nil {
		v.faceFonts = map[painter.Face]*pdfkit.Font{}
	}
	f, err := pdfkit.LoadFont(face.FontData())
	if err != nil {
		v.faceFonts[face] = nil
		return nil
	}
	v.faceFonts[face] = f
	return f
}

// TextFace draws s as selectable PDF text in face — the painter.FacePainter
// seam a TrueType/OpenType toolkit font hands its run to. The face's own sfnt
// bytes are embedded (so glyph shapes AND advances match the on-screen
// layout) and the run is emitted at the face's pixel size.
//
// The painter positions text by its top-left corner and PDF by the baseline,
// so the origin drops by the face ascent. When the face bytes do not parse,
// it degrades to the plain Text primitive (the fallback font) so a broken
// face still yields some selectable text rather than nothing.
func (v *vectorPainter) TextFace(x, y int, s string, face painter.Face, ink painter.RGBA) {
	if ink.A == 0 || s == "" {
		return
	}
	f := v.faceFont(face)
	if f == nil {
		v.Text(x, y, s, ink)
		return
	}
	v.page.Save()
	v.applyAlpha(ink)
	v.page.SetFillColor(pdfkit.RGB8(ink.R, ink.G, ink.B))
	v.page.SetFont(f, float64(face.SizePx())*v.sy)
	baseline := v.fy(y + face.Ascent())
	// The font was just set, so Text cannot return the no-font error.
	_ = v.page.Text(v.fx(x), baseline, s)
	v.page.Restore()
}

// fx maps a painter x (pixels) to a PDF x (points).
func (v *vectorPainter) fx(x int) float64 { return v.rx + float64(x)*v.sx }

// fy maps a painter y (pixels, top-down) to a PDF y (points, bottom-up).
func (v *vectorPainter) fy(y int) float64 { return v.ry + v.rh - float64(y)*v.sy }

// prect maps a painter rectangle (top-left origin) to a PDF rectangle
// (lower-left origin).
func (v *vectorPainter) prect(r painter.Rect) pdfkit.Rect {
	return pdfkit.Rect{
		X:      v.fx(r.X),
		Y:      v.fy(r.Y + r.H),
		Width:  float64(r.W) * v.sx,
		Height: float64(r.H) * v.sy,
	}
}

// applyAlpha sets a constant transparency when c is not fully opaque. Opaque
// colours skip the ExtGState so the common case stays clean.
func (v *vectorPainter) applyAlpha(c painter.RGBA) {
	if c.A < 0xFF {
		a := float64(c.A) / 255
		v.page.SetAlpha(a, a)
	}
}

// FillRect fills r with c.
func (v *vectorPainter) FillRect(r painter.Rect, c painter.RGBA) {
	if c.A == 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	v.page.Save()
	v.applyAlpha(c)
	v.page.SetFillColor(pdfkit.RGB8(c.R, c.G, c.B))
	v.page.Rectangle(v.prect(r))
	v.page.Fill()
	v.page.Restore()
}

// lineWidthPoints converts a painter line-width hint (pixels, minimum 1) to
// points along the x scale.
func (v *vectorPainter) lineWidthPoints(lineW int) float64 {
	if lineW < 1 {
		lineW = 1
	}
	return float64(lineW) * v.sx
}

// StrokeRect strokes a rectangular border around r.
func (v *vectorPainter) StrokeRect(r painter.Rect, c painter.RGBA, lineW int) {
	if c.A == 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	v.page.Save()
	v.applyAlpha(c)
	v.page.SetStrokeColor(pdfkit.RGB8(c.R, c.G, c.B))
	v.page.SetLineWidth(v.lineWidthPoints(lineW))
	v.page.Rectangle(v.prect(r))
	v.page.Stroke()
	v.page.Restore()
}

// bezierCircle is the control-point offset factor that approximates a quarter
// circle with a cubic Bézier.
const bezierCircle = 0.5522847498307936

// roundRectPath emits a rounded-rectangle subpath for the PDF rectangle R
// with horizontal corner radius rx and vertical corner radius ry (both in
// points).
func (v *vectorPainter) roundRectPath(R pdfkit.Rect, rx, ry float64) {
	x0, y0 := R.X, R.Y
	x1, y1 := R.X+R.Width, R.Y+R.Height
	kx, ky := rx*bezierCircle, ry*bezierCircle
	v.page.MoveTo(x0+rx, y0)
	v.page.LineTo(x1-rx, y0)
	v.page.CurveTo(x1-rx+kx, y0, x1, y0+ry-ky, x1, y0+ry)
	v.page.LineTo(x1, y1-ry)
	v.page.CurveTo(x1, y1-ry+ky, x1-rx+kx, y1, x1-rx, y1)
	v.page.LineTo(x0+rx, y1)
	v.page.CurveTo(x0+rx-kx, y1, x0, y1-ry+ky, x0, y1-ry)
	v.page.LineTo(x0, y0+ry)
	v.page.CurveTo(x0, y0+ry-ky, x0+rx-kx, y0, x0+rx, y0)
	v.page.ClosePath()
}

// cornerRadii resolves the painter pixel radius (clamped to half the smaller
// side, mirroring the pixel painter) into PDF-point radii along each axis. A
// non-positive result means "no rounding".
func (v *vectorPainter) cornerRadii(r painter.Rect, radius int) (rx, ry float64) {
	m := r.W
	if r.H < m {
		m = r.H
	}
	if radius > m/2 {
		radius = m / 2
	}
	if radius <= 0 {
		return 0, 0
	}
	return float64(radius) * v.sx, float64(radius) * v.sy
}

// FillRoundRect fills r with rounded corners.
func (v *vectorPainter) FillRoundRect(r painter.Rect, radius int, c painter.RGBA) {
	if c.A == 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	rx, ry := v.cornerRadii(r, radius)
	v.page.Save()
	v.applyAlpha(c)
	v.page.SetFillColor(pdfkit.RGB8(c.R, c.G, c.B))
	if rx <= 0 {
		v.page.Rectangle(v.prect(r))
	} else {
		v.roundRectPath(v.prect(r), rx, ry)
	}
	v.page.Fill()
	v.page.Restore()
}

// StrokeRoundRect strokes a rounded-corner border around r.
func (v *vectorPainter) StrokeRoundRect(r painter.Rect, radius int, c painter.RGBA, lineW int) {
	if c.A == 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	rx, ry := v.cornerRadii(r, radius)
	v.page.Save()
	v.applyAlpha(c)
	v.page.SetStrokeColor(pdfkit.RGB8(c.R, c.G, c.B))
	v.page.SetLineWidth(v.lineWidthPoints(lineW))
	if rx <= 0 {
		v.page.Rectangle(v.prect(r))
	} else {
		v.roundRectPath(v.prect(r), rx, ry)
	}
	v.page.Stroke()
	v.page.Restore()
}

// PutPixel paints a single painter pixel as a 1x1 filled cell.
func (v *vectorPainter) PutPixel(x, y int, c painter.RGBA) {
	if c.A == 0 {
		return
	}
	v.page.Save()
	v.applyAlpha(c)
	v.page.SetFillColor(pdfkit.RGB8(c.R, c.G, c.B))
	v.page.Rectangle(pdfkit.Rect{X: v.fx(x), Y: v.fy(y + 1), Width: v.sx, Height: v.sy})
	v.page.Fill()
	v.page.Restore()
}

// Text draws s as selectable PDF text. The painter positions text by its
// top-left corner; PDF positions it by the baseline, so the origin drops by
// the glyph box height. The run is sized so it roughly fills that box.
func (v *vectorPainter) Text(x, y int, s string, ink painter.RGBA) {
	if ink.A == 0 || s == "" {
		return
	}
	v.page.Save()
	v.applyAlpha(ink)
	v.page.SetFillColor(pdfkit.RGB8(ink.R, ink.G, ink.B))
	v.page.SetFont(v.font, glyphPx*v.sy)
	baseline := v.fy(y + int(glyphPx))
	// The font was just set, so Text cannot return the no-font error.
	_ = v.page.Text(v.fx(x), baseline, s)
	v.page.Restore()
}

// Size returns the canvas dimensions in painter pixels.
func (v *vectorPainter) Size() (int, int) { return v.w, v.h }

// PushClip confines subsequent drawing to r (implements painter.Clipper). PDF
// graphics-state nesting carries the intersection with any enclosing clip.
func (v *vectorPainter) PushClip(r painter.Rect) {
	v.page.Save()
	v.page.Rectangle(v.prect(r))
	v.page.Clip()
	v.page.EndPath()
	v.clipDepth++
}

// PopClip removes the most recent PushClip. It is a no-op when the clip stack
// is empty so an unbalanced call cannot corrupt the graphics-state stack.
func (v *vectorPainter) PopClip() {
	if v.clipDepth == 0 {
		return
	}
	v.clipDepth--
	v.page.Restore()
}
