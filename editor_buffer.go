package main

import (
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

const editorLimit = 256 << 10
const tabSize = 4

// Byte offsets retain the original line endings. Cursor and selection indices
// address grapheme boundaries so that movement and deletion use the same units.
type glyph struct{ offset, column int }
type selection struct{ anchor, caret int }
type change struct {
	offset            int
	removed, inserted string
	before, after     selection // byte offsets, valid in the corresponding revision
}
type document struct {
	text, saved, bom, newline, endings string
	glyphs                             []glyph // includes an EOF sentinel
	lines                              []int   // index of first glyph in each line
	selection
	preferred  int
	undo, redo []change
	version    uint64
}

func loadDocument(raw string) *document {
	d := &document{preferred: -1, newline: "\n"}
	if strings.HasPrefix(raw, "\uFEFF") {
		d.bom = "\uFEFF"
		raw = raw[len(d.bom):]
	}
	d.text, d.saved = raw, raw
	d.index()
	if i := strings.IndexAny(raw, "\r\n"); i >= 0 {
		d.newline = raw[i : i+1]
		if strings.HasPrefix(raw[i:], "\r\n") {
			d.newline = "\r\n"
		}
	}
	return d
}
func (d *document) index() {
	d.glyphs = d.glyphs[:0]
	d.lines = append(d.lines[:0], 0)
	column, state := 0, -1
	var lf, crlf, cr bool
	for i := 0; i < len(d.text); {
		g, _, width, next := uniseg.FirstGraphemeClusterInString(d.text[i:], state)
		state = next
		d.glyphs = append(d.glyphs, glyph{i, column})
		switch g {
		case "\r\n", "\n", "\r":
			switch g {
			case "\r\n":
				crlf = true
			case "\r":
				cr = true
			default:
				lf = true
			}
			column = 0
			d.lines = append(d.lines, len(d.glyphs))
		case "\t":
			column += tabSize - column%tabSize
		default:
			column += max(1, width)
		}
		i += len(g)
	}
	d.glyphs = append(d.glyphs, glyph{len(d.text), column})
	switch {
	case lf && crlf || cr && (lf || crlf):
		d.endings = "MIX"
	case crlf:
		d.endings = "CRLF"
	case cr:
		d.endings = "CR"
	default:
		d.endings = "LF"
	}
}
func (d *document) end() int { return len(d.glyphs) - 1 }
func (d *document) row(pos int) int {
	return sort.Search(len(d.lines), func(i int) bool { return d.lines[i] > pos }) - 1
}
func (d *document) lineEnd(row int) int {
	if row+1 < len(d.lines) {
		return d.lines[row+1] - 1
	}
	return d.end()
}
func (d *document) atByte(n int) int {
	return min(d.end(), sort.Search(len(d.glyphs), func(i int) bool { return d.glyphs[i].offset >= n }))
}
func (d *document) part(a, b int) string { return d.text[d.glyphs[a].offset:d.glyphs[b].offset] }
func (d *document) bounds() (int, int)   { return min(d.anchor, d.caret), max(d.anchor, d.caret) }
func (d *document) bytesSelection() selection {
	return selection{d.glyphs[d.anchor].offset, d.glyphs[d.caret].offset}
}
func (d *document) restore(s selection) {
	d.selection = selection{d.atByte(s.anchor), d.atByte(s.caret)}
	d.preferred = -1
}
func (d *document) move(pos int, extend bool) {
	d.caret = max(0, min(pos, d.end()))
	if !extend {
		d.anchor = d.caret
	}
}
func (d *document) vertical(delta int, extend bool) {
	if d.preferred < 0 {
		d.preferred = d.glyphs[d.caret].column
	}
	r := max(0, min(d.row(d.caret)+delta, len(d.lines)-1))
	start, end := d.lines[r], d.lineEnd(r)
	d.move(start+sort.Search(end-start, func(i int) bool { return d.glyphs[start+i].column >= d.preferred }), extend)
}
func (d *document) word(pos, dir int) int {
	isWord := func(i int) bool {
		r, _ := utf8.DecodeRuneInString(d.part(i, i+1))
		return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
	}
	if dir < 0 {
		for pos > 0 && !isWord(pos-1) {
			pos--
		}
		for pos > 0 && isWord(pos-1) {
			pos--
		}
		return pos
	}
	for pos < d.end() && isWord(pos) {
		pos++
	}
	for pos < d.end() && !isWord(pos) {
		pos++
	}
	return pos
}
func (d *document) splice(at, n int, s string) {
	d.text = d.text[:at] + s + d.text[at+n:]
	d.version++
	d.index()
}
func (d *document) replace(a, b int, s string) error {
	old := d.part(a, b)
	if len(d.bom)+len(d.text)-len(old)+len(s) > editorLimit {
		return errors.New(tr("лимит файла — 256 КиБ"))
	}
	if !utf8.ValidString(s) || strings.IndexByte(s, 0) >= 0 {
		return errors.New(tr("нужен текст UTF-8 без нулевых байтов"))
	}
	at := d.glyphs[a].offset
	before := d.bytesSelection()
	if old == s {
		d.move(b, false)
		return nil
	}
	d.splice(at, len(old), s)
	d.move(d.atByte(at+len(s)), false)
	d.preferred = -1
	d.undo = append(d.undo, change{at, strings.Clone(old), strings.Clone(s), before, d.bytesSelection()})
	// Bound both typing history and memory retained by large paste operations.
	used := 0
	for i := len(d.undo) - 1; i >= 0; i-- {
		used += len(d.undo[i].removed) + len(d.undo[i].inserted)
		if len(d.undo)-i > 500 || used > 4<<20 {
			d.undo = append([]change(nil), d.undo[i+1:]...)
			break
		}
	}
	d.redo = nil
	return nil
}
func (d *document) history(back bool) {
	from, to := &d.undo, &d.redo
	if !back {
		from, to = to, from
	}
	if len(*from) == 0 {
		return
	}
	n := len(*from) - 1
	c := (*from)[n]
	(*from)[n] = change{}
	*from = (*from)[:n]
	*to = append(*to, c)
	if back {
		d.splice(c.offset, len(c.inserted), c.removed)
		d.restore(c.before)
	} else {
		d.splice(c.offset, len(c.removed), c.inserted)
		d.restore(c.after)
	}
}
func (d *document) insert(s string) error { a, b := d.bounds(); return d.replace(a, b, s) }
func (d *document) lineBreak() string {
	r := d.row(d.caret)
	e := d.lineEnd(r)
	if e < d.end() {
		return d.part(e, e+1)
	}
	return d.newline
}
func (d *document) find(query string, back bool) bool {
	if query == "" {
		return false
	}
	a, b := d.bounds()
	pivot := d.glyphs[b].offset
	if back {
		pivot = d.glyphs[a].offset
	}
	findIn := func(lo, hi int) bool {
		for hi-lo >= len(query) {
			var n int
			if back {
				n = strings.LastIndex(d.text[lo:hi], query)
			} else {
				n = strings.Index(d.text[lo:hi], query)
			}
			if n < 0 {
				return false
			}
			start := lo + n
			end := start + len(query)
			first, last := d.atByte(start), d.atByte(end)
			if d.glyphs[first].offset == start && d.glyphs[last].offset == end {
				d.selection = selection{first, last}
				d.preferred = -1
				return true
			}
			if back {
				hi = end - 1
			} else {
				lo = start + 1
			}
		}
		return false
	}
	if back {
		return findIn(0, pivot) || findIn(0, len(d.text))
	}
	return findIn(pivot, len(d.text)) || findIn(0, len(d.text))
}
