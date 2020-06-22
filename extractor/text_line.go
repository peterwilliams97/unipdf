/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/unidoc/unipdf/v3/model"
)

// textLine repesents words on the same line within a textPara.
type textLine struct {
	serial             int         // Sequence number for debugging.
	model.PdfRectangle             // Bounding box (union of `marks` bounding boxes).
	depth              float64     // Distance from bottom of line to top of page.
	words              []*textWord // Words in this line.
	fontsize           float64     // Largest word font size.
	hyphenated         bool        // Does line have at least minHyphenation runes and end in a hyphen.
}

const minHyphenation = 4

// newTextLine creates a line with font and bbox size of `w`, removes `w` from
// p.bins[bestWordDepthIdx] and adds it to the line
func newTextLine(p *wordBag, depthIdx int) *textLine {
	words := p.stratum(depthIdx)
	word := words[0]
	line := textLine{
		serial:       serial.line,
		PdfRectangle: word.PdfRectangle,
		fontsize:     word.fontsize,
		depth:        word.depth,
	}
	serial.line++
	line.moveWord(p, depthIdx, word)
	return &line
}

// String returns a description of `l`.
func (l *textLine) String() string {
	return fmt.Sprintf("serial=%d %.2f %6.2f fontsize=%.2f \"%s\"",
		l.serial, l.depth, l.PdfRectangle, l.fontsize, l.text())
}

// bbox makes textLine implementethe `bounded` interface.
func (l *textLine) bbox() model.PdfRectangle {
	return l.PdfRectangle
}

// text returns the extracted text contained in line.
func (l *textLine) text() string {
	var words []string
	for _, w := range l.words {
		if w.newWord {
			words = append(words, " ")
		}
		words = append(words, w.text())
	}
	return strings.Join(words, "")
}

// toTextMarks returns the TextMarks contained in `l`.text().
// `offset` is used to give the TextMarks the correct Offset values.
func (l *textLine) toTextMarks(offset *int) []TextMark {
	var marks []TextMark
	for _, w := range l.words {
		if w.newWord {
			marks = appendSpaceMark(marks, offset, " ")
		}
		wordMarks := w.toTextMarks(offset)
		marks = append(marks, wordMarks...)
	}
	return marks
}

// moveWord removes `word` from bag and appends it to `l`.
func (l *textLine) moveWord(bag *wordBag, depthIdx int, word *textWord) {
	l.appendWord(word)
	bag.removeWord(depthIdx, word)
}

// appendWord appends `word` to `l`.
// `l.PdfRectangle` is increased to bound the new word
// `l.fontsize` is the largest of the fontsizes of the words in line
func (l *textLine) appendWord(word *textWord) {
	l.words = append(l.words, word)
	l.PdfRectangle = rectUnion(l.PdfRectangle, word.PdfRectangle)
	if word.fontsize > l.fontsize {
		l.fontsize = word.fontsize
	}
	if word.depth > l.depth {
		l.depth = word.depth
	}
}

// markWordBoundaries marks the word fragments that are new words.
func (l *textLine) markWordBoundaries() {
	maxGap := maxIntraLineGapR * l.fontsize
	for i, w := range l.words[1:] {
		if gapReading(w, l.words[i]) >= maxGap {
			w.newWord = true
		}
	}

	// check for hyphen at end of line
	l.hyphenated = isHyphenated(l.text())
}

// isHyphenated returns true if `text` is a hyphenated word.
func isHyphenated(text string) bool {
	runes := []rune(text)
	return len(runes) >= minHyphenation &&
		unicode.Is(unicode.Hyphen, runes[len(runes)-1]) &&
		!unicode.IsSpace(runes[len(runes)-2])
}
