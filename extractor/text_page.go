/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"fmt"
	"io"
	"math"
	"unicode"

	"github.com/unidoc/unipdf/v3/common"
	"github.com/unidoc/unipdf/v3/model"
)

// paraList is a sequence of textPara. We use it so often that it is convenient to have its own
// type so we can have methods on it.
type paraList []*textPara

// makeTextPage builds a paraList from `marks`, the textMarks on a page.
func makeTextPage(marks []*textMark, pageSize model.PdfRectangle, rot int) paraList {
	common.Log.Trace("makeTextPage: %d elements pageSize=%.2f", len(marks), pageSize)

	// Break the marks into words
	words := makeTextWords(marks, pageSize)

	// Divide the words into depth bins with each the contents of each bin sorted by reading direction
	page := makeTextStrata(words, pageSize.Ury)
	// Divide the page into rectangular regions for each paragraph and creata a textStrata for each one.
	paraStratas := dividePage(page, pageSize.Ury)
	// Arrange the contents of each para into lines
	paras := make(paraList, len(paraStratas))
	for i, para := range paraStratas {
		paras[i] = composePara(para)
	}
	if verbose || true {
		common.Log.Info("unsorted: %d paras =========----------=====", len(paras))
		for i, para := range paras {
			fmt.Printf("%4d: %6.2f %q\n", i, para.PdfRectangle, truncate(paras[i].text(), 60))
		}
	}

	paras.computeEBBoxes()
	paras = paras.extractTables()

	// Sort the paras into reading order.
	paras.sortReadingOrder()
	if verbose || true {
		common.Log.Info("para sorted in reading order -----------=========")
		for i, para := range paras {
			tab := ""
			if para.table != nil {
				tab = fmt.Sprintf("[%dx%d]", para.table.w, para.table.h)
			}
			fmt.Printf("%4d: %6.2f %s %q\n", i, para.PdfRectangle, tab, truncate(para.text(), 50))
		}
	}
	return paras
}

// dividePage divides page builds a list of paragraph textStrata from `page`, the page textStrata.
func dividePage(page *textStrata, pageHeight float64) []*textStrata {
	var paraStratas []*textStrata

	// We move words from `page` to paras until there no words left in page.
	// We do this by iterating through `page` in depth bin order and, for each surving bin (see
	// below),  creating a paragraph with seed word, `words[0]` in the code below.
	// We then move words from around the `para` region from `page` to `para` .
	// This may empty some page bins before we iterate to them
	// Some bins are emptied before they iterated to (seee "surving bin" above).
	// If a `page` survives until it is iterated to then at least one `para` will be built around it.

	if verbose {
		common.Log.Info("dividePage")
	}
	cnt := 0
	for _, depthIdx := range page.depthIndexes() {
		changed := false
		for ; !page.empty(depthIdx); cnt++ {
			// Start a new paragraph region `para`.
			// Build `para` out from the left-most (lowest in reading direction) word `words`[0],
			// in the bins in and below `depthIdx`.
			para := newTextStrata(pageHeight)

			// words[0] is the leftmost word from the bins in and a few lines below `depthIdx`. We
			// seed 'para` with this word.
			firstReadingIdx := page.firstReadingIndex(depthIdx)
			words := page.getStratum(firstReadingIdx)
			moveWord(firstReadingIdx, page, para, words[0])
			if verbose {
				common.Log.Info("words[0]=%s", words[0].String())
			}

			// The following 3 numbers define whether words should be added to `para`.
			minInterReadingGap := minInterReadingGapR * para.fontsize
			maxIntraReadingGap := maxIntraReadingGapR * para.fontsize
			maxIntraDepthGap := maxIntraDepthGapR * para.fontsize

			// Add words to `para` until we pass through the following loop without a new word
			// being added to a `para`.
			for running := true; running; running = changed {
				changed = false

				// Add words that are within maxIntraDepthGap of `para` in the depth direction.
				// i.e. Stretch para in the depth direction, vertically for English text.
				if verbose {
					common.Log.Info("para depth %.2f - %.2f maxIntraDepthGap=%.2f ",
						para.minDepth(), para.maxDepth(), maxIntraDepthGap)
				}
				if page.scanBand("veritcal", para, partial(readingOverlapPlusGap, 0),
					para.minDepth()-maxIntraDepthGap, para.maxDepth()+maxIntraDepthGap,
					maxIntraDepthFontTolR, false, false) > 0 {
					changed = true
				}
				// Add words that are within maxIntraReadingGap of `para` in the reading direction.
				// i.e. Stretch para in the reading direction, horizontall for English text.
				if page.scanBand("horizontal", para, partial(readingOverlapPlusGap, maxIntraReadingGap),
					para.minDepth(), para.maxDepth(),
					maxIntraReadingFontTol, false, false) > 0 {
					changed = true
				}
				// The above stretching has got as far as it go. Repeating it won't pull in more words.

				// Only try to combine other words if we can't grow para in the simple way above.
				if changed {
					continue
				}

				// In the following cases, we don't expand `para` while scanning. We look for words
				// around para. If we find them, we add them then expand `para` when we are done.
				// This pulls the numbers to the left of para into para
				// e.g. From
				// 		Regulatory compliance
				// 		Archiving
				// 		Document search
				// to
				// 		1. Regulatory compliance
				// 		2. Archiving
				// 		3. Document search

				// If there are words to the left of `para`, add them.
				// We need to limit the number of word
				n := page.scanBand("", para, partial(readingOverlapLeft, minInterReadingGap),
					para.minDepth(), para.maxDepth(),
					minInterReadingFontTol, true, false)
				if n > 0 {
					r := (para.maxDepth() - para.minDepth()) / para.fontsize
					if (n > 1 && float64(n) > 0.3*r) || n <= 5 {
						if page.scanBand("other", para, partial(readingOverlapLeft, minInterReadingGap),
							para.minDepth(), para.maxDepth(),
							minInterReadingFontTol, false, true) > 0 {
							changed = true
						}
					}
				}
			}

			// Sort the words in `para`'s bins in the reading direction.
			para.sort()
			paraStratas = append(paraStratas, para)
		}
	}

	return paraStratas
}

const doHyphens = true
const useTables = true

// writeText writes the text in `paras` to `w`.
func (paras paraList) writeText(w io.Writer) {
	for ip, para := range paras {
		if useTables {
			para.writeText(w)
			w.Write([]byte("\n"))
		} else {
			for il, line := range para.lines {
				s := line.text()
				reduced := false
				if doHyphens {
					if line.hyphenated && (il != len(para.lines)-1 || ip != len(paras)-1) {
						// Line ending with hyphen. Remove it.
						runes := []rune(s)
						s = string(runes[:len(runes)-1])
						reduced = true
					}
				}
				w.Write([]byte(s))
				if reduced {
					// We removed the hyphen from the end of the line so we don't need a line ending.
					continue
				}
				if il < len(para.lines)-1 && isZero(line.depth-para.lines[il+1].depth) {
					// Next line is the same depth so it's the same line as this one in the extracted text
					w.Write([]byte(" "))
					continue
				}
				w.Write([]byte("\n"))
			}
			w.Write([]byte("\n"))
		}
	}
}

func (paras paraList) toTables() []TextTable {
	var tables []TextTable
	for _, para := range paras {
		if para.table != nil {
			tables = append(tables, para.table.toTextTable())
		}
	}
	return tables
}

// toTextMarks creates the TextMarkArray corresponding to the extracted text created by
// paras `paras`.writeText().
func (paras paraList) toTextMarks() []TextMark {
	offset := 0
	var marks []TextMark
	addMark := func(mark TextMark) {
		mark.Offset = offset
		marks = append(marks, mark)
		offset += len(mark.Text)
	}
	addSpaceMark := func(spaceChar string) {
		mark := spaceMark
		mark.Text = spaceChar
		addMark(mark)
	}
	for ip, para := range paras {
		if useTables {
			paraMarks := para.toTextMarks(&offset)
			marks = append(marks, paraMarks...)
		} else {
			for il, line := range para.lines {
				lineMarks := line.toTextMarks(&offset)
				marks = append(marks, lineMarks...)
				reduced := false
				if doHyphens {
					if line.hyphenated && (il != len(para.lines)-1 || ip != len(paras)-1) {
						tm := marks[len(marks)-1]
						r := []rune(tm.Text)
						if unicode.IsSpace(r[len(r)-1]) {
							panic(tm)
						}
						if len(r) == 1 {
							marks = marks[:len(marks)-1]
							offset = marks[len(marks)-1].Offset + len(marks[len(marks)-1].Text)
						} else {
							s := string(r[:len(r)-1])
							offset += len(s) - len(tm.Text)
							tm.Text = s
						}
						reduced = true
					}
				}
				if reduced {
					continue
				}
				if il != len(para.lines)-1 && isZero(line.depth-para.lines[il+1].depth) {
					// Next line is the same depth so it's the same line as this one in the extracted text
					addSpaceMark(" ")
					continue
				}
				addSpaceMark("\n")
			}
			if ip != len(paras)-1 {
				addSpaceMark("\n")
			}
		}
	}
	return marks
}

// sortReadingOrder sorts `paras` in reading order.
func (paras paraList) sortReadingOrder() {
	common.Log.Debug("sortReadingOrder: paras=%d ===========x=============", len(paras))
	if len(paras) <= 1 {
		return
	}
	adj := paras.adjMatrix()
	order := topoOrder(adj)
	paras.reorder(order)
}

// adjMatrix creates an adjacency matrix for the DAG of connections over `paras`.
// Node i is connected to node j if i comes before j by Breuel's rules.
func (paras paraList) adjMatrix() [][]bool {
	n := len(paras)
	adj := make([][]bool, n)
	reasons := make([][]string, n)
	for i := range paras {
		adj[i] = make([]bool, n)
		reasons[i] = make([]string, n)
		for j := range paras {
			if i == j {
				continue
			}
			adj[i][j], reasons[i][j] = paras.before(i, j)
		}
	}
	if verbose && false {
		common.Log.Info("adjMatrix =======")
		for i := 0; i < n; i++ {
			a := paras[i]
			fmt.Printf("%4d: %q %.2f\n", i, truncate(a.text(), 50), a.PdfRectangle)
			for j := 0; j < n; j++ {
				if i == j {
					continue
				}
				if !adj[i][j] {
					continue
				}
				b := paras[j]
				fmt.Printf("%8d: %10s %q %.2f\n", j,
					reasons[i][j], truncate(b.text(), 40), b.PdfRectangle)

			}
		}
	}
	return adj
}

// before defines an ordering over `paras`.
// before returns true if `a` comes before `b`.
// 1. Line segment `a` comes before line segment `b` if their ranges of x-coordinates overlap and if
//    line segment `a` is above line segment `b` on the page.
// 2. Line segment `a` comes before line segment `b` if `a` is entirely to the left of `b` and if
//    there does not exist a line segment `c` whose y-coordinates are between `a` and `b` and whose
//    range of x coordinates overlaps both `a` and `b`.
// From Thomas M. Breuel "High Performance Document Layout Analysis"
func (paras paraList) before(i, j int) (bool, string) {
	a, b := paras[i], paras[j]
	// Breuel's rule 1
	if overlappedXPara(a, b) && a.Lly > b.Lly {
		return true, "above"
	}

	// Breuel's rule 2
	if !(a.eBBox.Urx < b.eBBox.Llx) {
		return false, "NOT left"
	}
	for k, c := range paras {
		if k == i || k == j {
			continue
		}
		lo := a.Lly
		hi := b.Lly
		if lo > hi {
			hi, lo = lo, hi
		}
		if !(lo < c.Lly && c.Lly < hi) {
			continue
		}
		if overlappedXPara(a, c) && overlappedXPara(c, b) {
			return false, "Y intervening"
		}
	}
	return true, "TO LEFT"
}

// overlappedX returns true if `r0` and `r1` overlap on the x-axis. !@#$ There is another version
// of this!
func overlappedXPara(r0, r1 *textPara) bool {
	return overlappedXRect(r0.eBBox, r1.eBBox)
}

// computeEBBoxes computes the eBBox fields in the elements of `paras`.
func (paras paraList) computeEBBoxes() {
	common.Log.Trace("computeEBBoxes:")

	for i, a := range paras {
		// [llx, urx] is the reading direction interval for which no paras overlap `a`
		llx := -1.0e9
		urx := +1.0e9
		for j, b := range paras {
			if i == j || !(a.Lly <= b.Ury && b.Lly <= a.Ury) {
				continue
			}
			// y overlap

			// `b` to left of `a`. no x overlap.
			if b.Urx < a.Llx {
				llx = math.Max(llx, b.Urx)
			}
			// `b` to right of `a`. no x overlap.
			if a.Urx < b.Llx {
				urx = math.Min(urx, b.Llx)
			}

		}
		// llx extends left from `a` and overlaps no other paras.
		// urx extends right from `a` and overlaps no other paras.

		// Go through all paras below `a` within interval [llx, urx] in the reading direction and
		// expand `a` as far as possible to left and right without overlapping any of them.
		a.eBBox = a.PdfRectangle
		for j, b := range paras {
			if i == j || b.Ury > a.Lly {
				continue
			}

			// If `b` is completely to right of `llx`, extend `a` left to `b`.
			if llx <= b.Llx {
				a.eBBox.Llx = math.Min(a.eBBox.Llx, b.Llx)
			}

			// If `b` is completely to left of `urx`, extend `a` right to `b`.
			if b.Urx <= urx {
				a.eBBox.Urx = math.Max(a.eBBox.Urx, b.Urx)
			}
		}
	}
}

// topoOrder returns the ordering of the topological sort of the nodes with adjacency matrix `adj`.
func topoOrder(adj [][]bool) []int {
	n := len(adj)
	visited := make([]bool, n)
	var order []int

	// sortNode recursively sorts below node `idx` in the adjacency matrix.
	var sortNode func(idx int)
	sortNode = func(idx int) {
		visited[idx] = true
		for i := 0; i < n; i++ {
			if adj[idx][i] && !visited[i] {
				sortNode(i)
			}
		}
		order = append(order, idx) // Should prepend but it's cheaper to append and reverse later.
	}

	for idx := 0; idx < n; idx++ {
		if !visited[idx] {
			sortNode(idx)
		}
	}
	// Order is currently reversed so change it to forward order.
	for i := 0; i < n/2; i++ {
		order[i], order[n-1-i] = order[n-1-i], order[i]
	}
	return order
}

// reorder reorders `para` to the order in `order`.
func (paras paraList) reorder(order []int) {
	sorted := make(paraList, len(paras))
	for i, k := range order {
		sorted[i] = paras[k]
	}
	copy(paras, sorted)
}
