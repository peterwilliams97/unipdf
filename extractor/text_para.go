/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"bytes"
	"fmt"
	"io"
	"sort"

	"github.com/unidoc/unipdf/v3/common"
	"github.com/unidoc/unipdf/v3/model"
)

// paraList is a sequence of textPara. We use it so often that it is convenient to have its own
// type so we can have methods on it.
type paraList []*textPara

// textPara is a group of words in a rectangular region of a page that get read together.
// An peragraph in a document might span multiple pages. This is the paragraph framgent on one page.
// We start by finding paragraph regions on a page, then we break the words into the textPara into
// textLines.
type textPara struct {
	serial             int                // Sequence number for debugging.
	model.PdfRectangle                    // Bounding box.
	eBBox              model.PdfRectangle // Extended bounding box needed to compute reading order.
	lines              []*textLine        // Paragraph text gets broken into lines.
	table              *textTable         // A table in which the cells which textParas.
	isCell             bool               // Is this para a cell in a textTable>
	// The unique highest para completely below this that overlaps it in the y-direction, if one exists.
	right *textPara
	// The unique highest para completely below `this that overlaps it in the x-direction, if one exists.
	below *textPara
}

// newTextPara returns a textPara with bounding rectangle `bbox`.
func newTextPara(bbox model.PdfRectangle) *textPara {
	para := textPara{
		serial:       serial.para,
		PdfRectangle: bbox,
	}
	serial.para++
	return &para
}

// String returns a description of `p`.
func (p *textPara) String() string {
	table := ""
	if p.table != nil {
		table = fmt.Sprintf("[%dx%d] ", p.table.w, p.table.h)
	}
	return fmt.Sprintf("serial=%d %6.2f %s%d lines %q",
		p.serial, p.PdfRectangle, table, len(p.lines), truncate(p.text(), 50))
}

// text returns the text  of the lines in `p`.
func (p *textPara) text() string {
	w := new(bytes.Buffer)
	p.writeText(w)
	return w.String()
}

func (p *textPara) depth() float64 {
	if len(p.lines) > 0 {
		return p.lines[0].depth
	}
	return p.table.get(0, 0).depth()
}

// writeText writes the text of `p` including tables to `w`.
func (p *textPara) writeText(w io.Writer) {
	if p.table == nil {
		p.writeCellText(w)
		return
	}
	for y := 0; y < p.table.h; y++ {
		for x := 0; x < p.table.w; x++ {
			cell := p.table.get(x, y)
			if cell == nil {
				w.Write([]byte("\t"))
			} else {
				cell.writeCellText(w)
			}
			w.Write([]byte(" "))
		}
		if y < p.table.h-1 {
			w.Write([]byte("\n"))
		}
	}
}

// toTextMarks creates the TextMarkArray corresponding to the extracted text created by
// paras `p`.writeText().
func (p *textPara) toTextMarks(offset *int) []TextMark {
	if p.table == nil {
		return p.toCellTextMarks(offset)
	}
	var marks []TextMark
	for y := 0; y < p.table.h; y++ {
		for x := 0; x < p.table.w; x++ {
			cell := p.table.get(x, y)
			if cell == nil {
				marks = appendSpaceMark(marks, offset, "\t")
			} else {
				cellMarks := cell.toCellTextMarks(offset)
				marks = append(marks, cellMarks...)
			}
			marks = appendSpaceMark(marks, offset, " ")
		}
		if y < p.table.h-1 {
			marks = appendSpaceMark(marks, offset, "\n")
		}
	}
	return marks
}

// writeCellText writes the text of `p` not including tables to `w`.
func (p *textPara) writeCellText(w io.Writer) {
	for il, line := range p.lines {
		lineText := line.text()
		reduced := doHyphens && line.hyphenated && il != len(p.lines)-1
		if reduced { // Line ending with hyphen. Remove it.
			lineText = removeLastRune(lineText)
		}
		w.Write([]byte(lineText))
		if !(reduced || il == len(p.lines)-1) {
			w.Write([]byte(getSpace(line.depth, p.lines[il+1].depth)))
		}
	}
}

// toCellTextMarks creates the TextMarkArray corresponding to the extracted text created by
// paras `paras`.writeCellText().
func (p *textPara) toCellTextMarks(offset *int) []TextMark {
	var marks []TextMark
	for il, line := range p.lines {
		lineMarks := line.toTextMarks(offset)
		reduced := doHyphens && line.hyphenated && il != len(p.lines)-1
		if reduced { // Line ending with hyphen. Remove it.
			lineMarks = removeLastTextMarkRune(lineMarks, offset)
		}
		marks = append(marks, lineMarks...)
		if !(reduced || il == len(p.lines)-1) {
			marks = appendSpaceMark(marks, offset, getSpace(line.depth, p.lines[il+1].depth))
		}
	}
	return marks
}

// removeLastTextMarkRune removes the last run from `marks`.
func removeLastTextMarkRune(marks []TextMark, offset *int) []TextMark {
	tm := marks[len(marks)-1]
	runes := []rune(tm.Text)
	if len(runes) == 1 {
		marks = marks[:len(marks)-1]
		tm1 := marks[len(marks)-1]
		*offset = tm1.Offset + len(tm1.Text)
	} else {
		text := removeLastRune(tm.Text)
		*offset += len(text) - len(tm.Text)
		tm.Text = text
	}
	return marks
}

// removeLastRune removes the last run from `text`.
func removeLastRune(text string) string {
	runes := []rune(text)
	return string(runes[:len(runes)-1])
}

// getSpace returns the space to insert between lines of depth `depth1` and `depth2`.
// Next line is the same depth so it's the same line as this one in the extracted text
func getSpace(depth1, depth2 float64) string {
	eol := !isZero(depth1 - depth2)
	if eol {
		return "\n"
	}
	return " "
}

// bbox makes textPara implement the `bounded` interface.
func (p *textPara) bbox() model.PdfRectangle {
	return p.PdfRectangle
}

// fontsize return the para's fontsize which we take to be the first line's fontsize.
// Caller must check that `p` has at least one line.
func (p *textPara) fontsize() float64 {
	return p.lines[0].fontsize
}

// composePara builds a textPara from the words in `b`.
// It does this by arranging the words in `b` into lines.
func (b *wordBag) composePara() *textPara {
	// Sort the words in `para`'s bins in the reading direction.
	b.sort()
	para := newTextPara(b.PdfRectangle)

	// build the lines
	for _, depthIdx := range b.depthIndexes() {
		for !b.empty(depthIdx) {

			// words[0] is the leftmost word from bins near `depthIdx`.
			firstReadingIdx := b.firstReadingIndex(depthIdx)
			// create a new line
			words := b.getStratum(firstReadingIdx)
			word0 := words[0]
			line := newTextLine(b, firstReadingIdx)
			lastWord := words[0]

			// Compute the search range.
			// This is based on word0, the first word in the `firstReadingIdx` bin.
			fontSize := b.fontsize
			minDepth := word0.depth - lineDepthR*fontSize
			maxDepth := word0.depth + lineDepthR*fontSize
			maxIntraWordGap := maxIntraWordGapR * fontSize

		remainingWords: // Find the rest of the words in this line.
			for {
				// Search for `leftWord`, the left-most word w: minDepth <= w.depth <= maxDepth.
				var leftWord *textWord
				leftDepthIdx := 0
				for _, depthIdx := range b.depthBand(minDepth, maxDepth) {
					words := b.stratumBand(depthIdx, minDepth, maxDepth)
					if len(words) == 0 {
						continue
					}
					word := words[0]
					gap := gapReading(word, lastWord)
					if gap < -maxIntraLineOverlapR*fontSize {
						break remainingWords
					}
					// No `leftWord` or `word` to the left of `leftWord`.
					if gap < maxIntraWordGap {
						if leftWord == nil || diffReading(word, leftWord) < 0 {
							leftDepthIdx = depthIdx
							leftWord = word
						}
					}
				}
				if leftWord == nil {
					break
				}

				// remove `leftWord` from `b`[`leftDepthIdx`], and append it to `line`.
				line.moveWord(b, leftDepthIdx, leftWord)
				lastWord = leftWord
			}

			line.mergeWordFragments()
			// add the line
			para.lines = append(para.lines, line)
		}
	}

	sort.Slice(para.lines, func(i, j int) bool {
		return diffDepthReading(para.lines[i], para.lines[j]) < 0
	})

	if verbosePara {
		common.Log.Info("!!! para=%s", para.String())
		if verboseParaLine {
			for i, line := range para.lines {
				fmt.Printf("%4d: %s\n", i, line.String())
				if verboseParaWord {
					for j, word := range line.words {
						fmt.Printf("%8d: %s\n", j, word.String())
						for k, mark := range word.marks {
							fmt.Printf("%12d: %s\n", k, mark.String())
						}
					}
				}
			}
		}
	}
	return para
}

// log logs the contents of `paras`.
func (paras paraList) log(title string) {
	if !verbosePage {
		return
	}
	common.Log.Info("%8s: %d paras =======-------=======", title, len(paras))
	for i, para := range paras {
		if para == nil {
			continue
		}
		text := para.text()
		tabl := "  "
		if para.table != nil {
			tabl = fmt.Sprintf("[%dx%d]", para.table.w, para.table.h)
		}
		fmt.Printf("%4d: %6.2f %s %q\n", i, para.PdfRectangle, tabl, truncate(text, 50))
	}
}
