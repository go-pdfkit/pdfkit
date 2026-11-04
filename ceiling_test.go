// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package pdfkit

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// claimingPNG is a real one-pixel PNG whose IHDR has been rewritten to claim w
// by h, with the chunk's CRC recomputed so every decoder accepts the header.
// The pixel data is still one pixel.
//
// ⛔ This is what the attack looks like: not large, not malformed, and nothing
// about it suspicious until something multiplies its two numbers together.
func claimingPNG(t testing.TB, w, h uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	// 8 signature, then IHDR: 4 length, 4 type, 13 data (width then height),
	// 4 CRC over type+data. W3C PNG 3rd ed. §11.2.1.
	put32(b[16:], w)
	put32(b[20:], h)
	put32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	return b
}

func put32(b []byte, v uint32) {
	b[0], b[1], b[2], b[3] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
}

// heldDuring is how much the heap grew across f, with a collection either
// side. Coarse, which is all this needs: the difference is between zero and
// thousands of megabytes.
func heldDuring(f func()) float64 {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	if after.HeapAlloc < before.HeapAlloc {
		return 0
	}
	return float64(after.HeapAlloc-before.HeapAlloc) / (1 << 20)
}

func TestAPNGIsRefusedFromItsHeaderBeforeItsPixelsAreAllocated(t *testing.T) {
	// ⛔ The file is not large, not malformed, and not slow. Measured on the
	// identical shape in go-pdfkit/convert: a 10000x10000 claim held 381.6
	// MiB; the four hundred megapixels claimed here are 1.5 GiB.
	bomb := claimingPNG(t, 20000, 20000)
	if len(bomb) > 200 {
		t.Fatalf("the fixture is %d bytes, which is not the point being made", len(bomb))
	}
	d := New(Options{})
	p := d.AddPage(A4)
	var err error
	held := heldDuring(func() { err = p.DrawPNG(bomb, Rect{Width: 100, Height: 100}) })
	if err == nil {
		t.Fatal("a PNG claiming four hundred megapixels was decoded")
	}
	if !strings.Contains(err.Error(), "pixels") {
		t.Errorf("it was refused as %q, which is not a refusal about its size", err)
	}
	if held > 32 {
		t.Errorf("refusing it held %.1f MiB: the ceiling is being applied after the "+
			"allocation it is meant to bound", held)
	}
}

func TestAnOrdinaryPictureIsStillEmbedded(t *testing.T) {
	// ⛔ The side a ceiling gets wrong. Tested only from above, a limit is
	// satisfied by refusing everything, and the test above would not notice.
	d := New(Options{})
	p := d.AddPage(A4)
	if err := p.DrawPNG(pngBytes(makeTestImage()), Rect{Width: 5, Height: 5}); err != nil {
		t.Fatalf("an ordinary picture was refused: %v", err)
	}
	if len(d.images) != 1 {
		t.Errorf("the document holds %d images: it was accepted and then not embedded", len(d.images))
	}
}

func TestTheCeilingCanBeRaisedAndLowered(t *testing.T) {
	img := claimingPNG(t, 1000, 1000) // a claim, so no megabytes are spent proving this
	draw := func(max int) error {
		d := New(Options{MaxPixels: max})
		return d.AddPage(A4).DrawPNG(img, Rect{Width: 10, Height: 10})
	}
	if err := draw(999_999); err == nil || !strings.Contains(err.Error(), "pixels") {
		t.Errorf("a million pixels passed a ceiling of 999999: %v", err)
	}
	// Exactly at the ceiling passes the ceiling; it then fails to decode,
	// because the pixel data is still one pixel. Two different refusals, and
	// the point is which one this is.
	if err := draw(1_000_000); err != nil && strings.Contains(err.Error(), "pixels") {
		t.Errorf("exactly at the ceiling was refused for its size: %v", err)
	}
	// ⛔ Zero means the default, not "no pixels allowed". Options{} is the
	// ordinary case — a caller who said nothing about size — and reading it as
	// a ceiling of zero would refuse every picture over a limit nobody set.
	if err := draw(0); err != nil && strings.Contains(err.Error(), "pixels") {
		t.Errorf("a picture was refused for its size when no ceiling was given: %v", err)
	}
}

func TestTheDecodedSizeIsTheSizeTheHeaderDeclared(t *testing.T) {
	// ⛔ This test exists to hold up an omission. go-pdfkit/convert#4 checks
	// the size again after decoding, because in general a header is a claim.
	// DrawPNG does not, on the grounds that png.DecodeConfig and png.Decode
	// both take their dimensions from the same IHDR — so the second check
	// could not differ, and an unreachable branch is a comfort, not a check.
	//
	// That is an assumption about image/png, not a theorem. If a Go release
	// ever makes the two disagree, this goes red and the omission is revisited
	// rather than discovered through a 1.5 GiB allocation.
	data := pngBytes(makeTestImage())
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != cfg.Width || b.Dy() != cfg.Height {
		t.Errorf("the header declared %dx%d and the decoder returned %dx%d: DrawPNG's "+
			"header check no longer bounds what DrawPNG decodes",
			cfg.Width, cfg.Height, b.Dx(), b.Dy())
	}
}

func TestTheDefaultCeilingIsTheNumberTheDocumentationQuotes(t *testing.T) {
	// ⛔ A change detector, deliberately. The number is quoted in Options's doc
	// comment, in DefaultMaxPixels's, and in the README as a figure in
	// megabytes — moving it silently makes three statements false at once, and
	// the only one anybody notices is the one that stops a real document.
	//
	// Written out rather than derived from the constant: deriving it would
	// make the judge move with the subject, which is how a ceiling test passes
	// a mutation that raised the ceiling.
	if DefaultMaxPixels != 41_943_040 {
		t.Errorf("DefaultMaxPixels is %d. If that was meant, update Options.MaxPixels's "+
			"doc comment, DefaultMaxPixels's, and the README.", DefaultMaxPixels)
	}
	if got := fmt.Sprintf("%d MiB", 41_943_040*4/(1<<20)); got != "160 MiB" {
		t.Errorf("the default at four bytes a pixel is %s", got)
	}
	// ⛔ And the sentence that sent me to check. go-gfx/gfx's svg package
	// documents 40_000_000 as "the same ceiling go-pdfkit/render uses"; it is
	// not, render.go:79 is 40 << 20. Written out so that aligning on a figure
	// borrowed from a neighbour's prose cannot happen here quietly.
	if DefaultMaxPixels == 40_000_000 {
		t.Error("the default has drifted onto go-gfx/gfx's number, which is the one that " +
			"claims a parity with go-pdfkit/render that it does not have")
	}
}

// --- the bomb, under a hard ceiling on the whole process ------------------

// bombEnv selects the child process's half of the bomb test: "guarded" calls
// DrawPNG, "bare" calls png.Decode with nothing in the way.
const bombEnv = "PDFKIT_BOMB_CHILD"

func TestABombIsRefusedByAProcessTooSmallToHoldIt(t *testing.T) {
	// ⛔ The heap test above reads two numbers and believes them. This one
	// cannot be argued with: the child may reserve 512 MiB more address space
	// than it already holds, a third of what the bomb needs, so an unbounded
	// decode does not merely allocate too much, it DIES. A refusal and a
	// corpse are easy to tell apart.
	//
	// It also keeps the bomb off this machine's memory — the limit is the
	// child's, not the host's, which matters on a box running two dozen
	// sessions at once.
	//
	// ⛔⛔ And it carries its own control, because a child that exits cleanly
	// proves nothing unless an unguarded one does not. The "bare" half decodes
	// the same bytes with no ceiling under the same limit and MUST die; if it
	// ever survives, the limit is not being applied and the "guarded" half's
	// clean exit means nothing.
	if mode := os.Getenv(bombEnv); mode != "" {
		drawTheBomb(t, mode)
		return
	}
	out, err := runBombChild(t, "guarded")
	if err != nil {
		t.Fatalf("the child did not survive the bomb with 512 MiB of headroom, "+
			"which is what an unbounded decode looks like: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte("refused:")) {
		t.Errorf("the child finished without refusing the bomb:\n%s", out)
	}
	bare, err := runBombChild(t, "bare")
	if err == nil {
		t.Errorf("decoding the same bomb with NO ceiling also survived 512 MiB of "+
			"headroom, so the limit is not being applied and the run above proved "+
			"nothing:\n%s", bare)
	}
}

// runBombChild re-runs this one test in a child process in the given mode. A
// child that could not take the limit skips the parent too, loudly: a bomb
// test that quietly runs without its limit is the thing the limit is for.
func runBombChild(t *testing.T, mode string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestABombIsRefusedByAProcessTooSmallToHoldIt", "-test.v")
	cmd.Env = append(os.Environ(), bombEnv+"="+mode, "GOMEMLIMIT=256MiB")
	out, err := cmd.CombinedOutput()
	if bytes.Contains(out, []byte(skipMark)) {
		// ⛔⛔ A skip and a pass are the same colour, and CI does not run with
		// -v, so a limit that silently stopped working would look exactly like
		// this test doing its job. On Linux there is no excuse — /proc and
		// RLIMIT_AS are both there — so a skip there is a FAILURE, and the
		// only platforms that may skip are the ones named in the message.
		if runtime.GOOS == "linux" {
			t.Fatalf("the child could not take an address-space limit on linux, where it "+
				"should always be able to; this test has stopped testing anything:\n%s", out)
		}
		t.Skipf("%s does not enforce an address-space limit, so the bomb is left to the "+
			"heap measurement in TestAPNGIsRefusedFromItsHeaderBeforeItsPixelsAreAllocated",
			runtime.GOOS)
	}
	return out, err
}

const skipMark = "no address-space limit here:"

// drawTheBomb is the child half. The limit is taken HERE rather than handed
// down from the parent: os/exec has no rlimit of its own, and the child is our
// own code, so it can refuse itself the room before it is asked for any.
func drawTheBomb(t *testing.T, mode string) {
	// The fixture is built BEFORE the limit comes down. It is one pixel, but
	// encoding it asks the runtime for room, and a test that dies making its
	// own fixture fails in the same words as the defect it is looking for.
	bomb := claimingPNG(t, 20000, 20000)
	// 512 MiB of headroom: ample for a 1x1 PNG and for whatever the runtime
	// wants next, and a third of what this bomb needs.
	if err := capAddressSpace(512 << 20); err != nil {
		t.Skipf("%s %v", skipMark, err)
	}
	if mode == "bare" {
		img, err := png.Decode(bytes.NewReader(bomb))
		t.Fatalf("the control survived: png.Decode returned %v, %v", img != nil, err)
	}
	err := New(Options{}).AddPage(A4).DrawPNG(bomb, Rect{Width: 100, Height: 100})
	if err == nil {
		t.Fatal("the bomb was decoded")
	}
	t.Logf("refused: %v", err)
}
