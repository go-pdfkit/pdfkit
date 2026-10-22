package pdfkit

import "bytes"

// BeginActualText starts a marked-content span (/Span ... BDC) whose text,
// for extraction, copying and searching, is replacement instead of the
// glyphs drawn inside it. An empty replacement makes what is drawn
// invisible to text extraction while it still shows: numbers in a margin, a
// numbered disc after a line of code. Readers that honour ActualText —
// Poppler's pdftotext does — then leave it out of the text; a reader that
// does not still draws and extracts it as before. End the span with
// EndMarkedContent; spans nest. A span may enclose whole text objects
// (BT ... ET), which is how it is meant to be used.
func (p *Page) BeginActualText(replacement string) {
	var b bytes.Buffer
	b.WriteString("/Span <</ActualText ")
	pdfTextString(replacement).encodePDF(&b)
	b.WriteString(">>")
	p.op(b.String(), "BDC")
	p.marked++
}

// EndMarkedContent ends the innermost span BeginActualText started. Without
// one open it writes nothing, so a stray call cannot unbalance the stream.
func (p *Page) EndMarkedContent() {
	if p.marked == 0 {
		return
	}
	p.marked--
	p.op("", "EMC")
}
