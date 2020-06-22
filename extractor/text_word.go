/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/unidoc/unipdf/v3/common"
	"github.com/unidoc/unipdf/v3/model"
)

// textWord represents a word. It's a sequence of textMarks that are close enough toghether in the
// reading direction and doesn't have any space textMarks.
// In some cases a textWord is a fragment of a word separated by a hyphen from another fragments
type textWord struct {
	serial             int         // Sequence number for debugging.
	model.PdfRectangle             // Bounding box (union of `marks` bounding boxes).
	depth              float64     // Distance from bottom of word to top of page.
	marks              []*textMark // Marks in this word.
	fontsize           float64     // Largest fontsize in `marks`
	spaceAfter         bool        // Is this word followed by a space?
}

// makeTextPage combines `marks`, the textMarks on a page, into word fragments.
// `pageSize` is used to calculate the words` depths depth on the page.
// Algorithm:
//  1. `marks` are in the order they were rendered in the PDF.
//  2. Successive marks are combined into a word unless
//      One mark is a space character.
//      They are separated by more than maxWordAdvanceR*fontsize in the reading direction
//      They are not within the location allowed by horizontal and vertical variations allowed by
//       reasonable kerning and leading.
// TODO(peterwilliams97): Check for overlapping textWords for cases such as diacritics, bolding by
//                       repeating and others.
func makeTextWords(marks []*textMark, pageSize model.PdfRectangle) []*textWord {
	var words []*textWord // The words.
	var newWord *textWord // The word being built.

	// addNewWord adds `newWord` to `words` and resets `newWord` to nil.
	addNewWord := func() {
		if newWord != nil {
			if !isTextSpace(newWord.text()) {
				words = append(words, newWord)
			}
			newWord = nil
		}
	}

	for _, tm := range marks {
		isSpace := isTextSpace(tm.text)
		if newWord == nil && !isSpace {
			newWord = newTextWord([]*textMark{tm}, pageSize)
			continue
		}
		if isSpace {
			addNewWord()
			continue
		}

		fontsize := newWord.fontsize
		depthGap := math.Abs(getDepth(pageSize, tm)-newWord.depth) / fontsize
		readingGap := gapReading(tm, newWord) / fontsize

		// These are the conditions for `tm` to be from a new word.
		// - Gap between words in reading position is larger than a space.
		// - Change in reading position is too negative to be just a kerning adjustment.
		// - Change in depth is too large to be just a leading adjustment.
		if readingGap >= maxWordAdvanceR || !(-maxKerningR <= readingGap && depthGap <= maxLeadingR) {
			addNewWord()
			newWord = newTextWord([]*textMark{tm}, pageSize)
			continue
		}
		newWord.addMark(tm, pageSize)
	}
	addNewWord()
	return words
}

// newTextWord creates a textWords containing `marks`.
// `pageSize` is used to calculate the word's depth on the page.
func newTextWord(marks []*textMark, pageSize model.PdfRectangle) *textWord {
	r := marks[0].PdfRectangle
	fontsize := marks[0].fontsize
	for _, tm := range marks[1:] {
		r = rectUnion(r, tm.PdfRectangle)
		if tm.fontsize > fontsize {
			fontsize = tm.fontsize
		}
	}

	word := textWord{
		serial:       serial.word,
		PdfRectangle: r,
		marks:        marks,
		depth:        pageSize.Ury - r.Lly,
		fontsize:     fontsize,
	}
	serial.word++
	return &word
}

// String returns a description of `w.
func (w *textWord) String() string {
	return fmt.Sprintf("serial=%d %.2f %6.2f fontsize=%.2f \"%s\"",
		w.serial, w.depth, w.PdfRectangle, w.fontsize, w.text())
}

// bbox makes textWord implement the `bounded` interface.
func (w *textWord) bbox() model.PdfRectangle {
	return w.PdfRectangle
}

// addMark adds textMark `tm` to word `w`.
// `pageSize` is used to calculate the word's depth on the page.
func (w *textWord) addMark(tm *textMark, pageSize model.PdfRectangle) {
	w.marks = append(w.marks, tm)
	w.PdfRectangle = rectUnion(w.PdfRectangle, tm.PdfRectangle)
	if tm.fontsize > w.fontsize {
		w.fontsize = tm.fontsize
	}
	w.depth = pageSize.Ury - w.PdfRectangle.Lly
}

// len returns the number of runes in `w`.
func (w *textWord) len() int {
	return utf8.RuneCountInString(w.text())
}

// absorb combines `word` into `w`.
func (w *textWord) absorb(word *textWord) {
	w.PdfRectangle = rectUnion(w.PdfRectangle, word.PdfRectangle)
	w.marks = append(w.marks, word.marks...)
}

// text returns the text in `w`.
func (w *textWord) text() string {
	texts := make([]string, len(w.marks))
	for i, tm := range w.marks {
		texts[i] = tm.text
	}
	return strings.Join(texts, "")
}

// toTextMarks returns the TextMarks contained in `w`.text().
// `offset` is used to give the TextMarks the correct Offset values.
func (w *textWord) toTextMarks(offset *int) []TextMark {
	var marks []TextMark
	for _, tm := range w.marks {
		marks = appendTextMark(marks, offset, tm.ToTextMark())
	}
	return marks
}

// removeWord returns `words` with `word` removed.
// Caller must check that `words` contains `word`,
// TODO(peterwilliams97): Optimize
func removeWord(words []*textWord, word *textWord) []*textWord {
	for i, w := range words {
		if w == word {
			return removeWordAt(words, i)
		}
	}
	common.Log.Error("removeWord: words doesn't contain word=%s", word)
	return nil
}

// removeWord returns `word` with `word[idx]` removed.
func removeWordAt(words []*textWord, idx int) []*textWord {
	n := len(words)
	copy(words[idx:], words[idx+1:])
	return words[:n-1]
}
