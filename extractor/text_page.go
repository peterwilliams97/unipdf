/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/unidoc/unipdf/v3/common"
	"github.com/unidoc/unipdf/v3/model"
)

// makeTextPage builds a paraList from `marks`, the textMarks on a page.
func makeTextPage(marks []*textMark, pageSize model.PdfRectangle, rot int) paraList {
	common.Log.Trace("makeTextPage: %d elements pageSize=%.2f", len(marks), pageSize)

	// Break the marks into words
	words := makeTextWords(marks, pageSize)
	a := makeAether(words, pageSize.Ury)

	// Divide the words into depth bins with each the contents of each bin sorted by reading direction
	page := a.makeTextStrata(words)
	// Divide the page into rectangular regions for each paragraph and creata a textStrata for each one.
	paraStratas := a.dividePage(page, pageSize.Ury)
	paraStratas = mergeStratas(paraStratas)
	// Arrange the contents of each para into lines
	paras := make(paraList, len(paraStratas))
	for i, para := range paraStratas {
		paras[i] = para.composePara()
	}

	paras.log("unsorted")
	// paras.computeEBBoxes()

	if useTables {
		paras = paras.extractTables()
	}
	// paras.log("tables extracted")
	paras.computeEBBoxes()
	paras.log("EBBoxes 2")

	// Sort the paras into reading order.
	paras.sortReadingOrder()
	paras.log("sorted in reading order")

	return paras
}

// dividePage divides page builds a list of paragraph textStrata from `page`, the page textStrata.
func (a *aether) dividePage(page *textStrata, pageHeight float64) []*textStrata {
	var paraStratas []*textStrata

	// We move words from `page` to paras until there no words left in page.
	// We do this by iterating through `page` in depth bin order and, for each surving bin (see
	// below),  creating a paragraph with seed word, `words[0]` in the code below.
	// We then move words from around the `para` region from `page` to `para` .
	// This may empty some page bins before we iterate to them
	// Some bins are emptied before they iterated to (seee "surving bin" above).
	// If a `page` survives until it is iterated to then at least one `para` will be built around it.

	if verbosePage {
		common.Log.Info("dividePage")
	}
	cnt := 0
	for _, depthIdx := range page.depthIndexes() {
		changed := false
		for ; !page.empty(depthIdx); cnt++ {
			// Start a new paragraph region `para`.
			// Build `para` out from the left-most (lowest in reading direction) word `words`[0],
			// in the bins in and below `depthIdx`.
			para := a.newTextStrata()

			// words[0] is the leftmost word from the bins in and a few lines below `depthIdx`. We
			// seed 'para` with this word.
			firstReadingIdx := page.firstReadingIndex(depthIdx)
			words := page.getStratum(firstReadingIdx)
			moveWord(firstReadingIdx, page, para, words[0])
			if verbosePage {
				common.Log.Info("words[0]=%s", words[0].String())
			}

			// The following 3 numbers define whether words should be added to `para`.
			minInterReadingGap := minInterReadingGapR * para.fontsize
			maxIntraReadingGap := maxIntraReadingGapR * para.fontsize
			maxIntraDepthGap := maxIntraDepthGapR * para.fontsize

			// Add words to `para` until we pass through the following loop without a new word
			// being added.
			for running := true; running; running = changed {
				changed = false

				// Add words that are within maxIntraDepthGap of `para` in the depth direction.
				// i.e. Stretch para in the depth direction, vertically for English text.
				if verbosePage {
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
				// We need to limit the number of words.
				otherTol := minInterReadingFontTol
				// otherTol = 0.7
				n := page.scanBand("", para, partial(readingOverlapLeft, minInterReadingGap),
					para.minDepth(), para.maxDepth(),
					otherTol, true, false)
				if n > 0 {
					r := (para.maxDepth() - para.minDepth()) / para.fontsize
					if (n > 1 && float64(n) > 0.3*r) || n <= 10 {
						if page.scanBand("other", para, partial(readingOverlapLeft, minInterReadingGap),
							para.minDepth(), para.maxDepth(),
							otherTol, false, true) > 0 {
							changed = true
						}
					}
				}
			}

			if verbosePage {
				para.sort()
				common.Log.Info("para=%s", para.String())
			}
			paraStratas = append(paraStratas, para)
		}
	}

	return paraStratas
}

// writeText writes the text in `paras` to `w`.
func (paras paraList) writeText(w io.Writer) {
	for ip, para := range paras {
		para.writeText(w)
		if ip != len(paras)-1 {
			if sameLine(para, paras[ip+1]) {
				w.Write([]byte(" "))
			} else {
				w.Write([]byte("\n"))
				w.Write([]byte("\n"))
			}
		}
	}
	w.Write([]byte("\n"))
	w.Write([]byte("\n"))
}

// toTextMarks creates the TextMarkArray corresponding to the extracted text created by
// paras `paras`.writeText().
func (paras paraList) toTextMarks() []TextMark {
	offset := 0
	var marks []TextMark
	for ip, para := range paras {
		paraMarks := para.toTextMarks(&offset)
		marks = append(marks, paraMarks...)
		if ip != len(paras)-1 {
			if sameLine(para, paras[ip+1]) {
				marks = appendSpaceMark(marks, &offset, " ")
			} else {
				marks = appendSpaceMark(marks, &offset, "\n")
				marks = appendSpaceMark(marks, &offset, "\n")
			}
		}
	}
	marks = appendSpaceMark(marks, &offset, "\n")
	marks = appendSpaceMark(marks, &offset, "\n")
	return marks
}

// sameLine returms true if `para1` and `para2` are on the same line.
func sameLine(para1, para2 *textPara) bool {
	return isZero(para1.depth() - para2.depth())
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

// sortReadingOrder sorts `paras` in reading order.
func (paras paraList) sortReadingOrder() {
	common.Log.Debug("sortReadingOrder: paras=%d ===========x=============", len(paras))
	if len(paras) <= 1 {
		return
	}
	sort.Slice(paras, func(i, j int) bool { return diffDepthReading(paras[i], paras[j]) <= 0 })
	paras.log("diffReadingDepth")
	order := paras.topoOrder()

	paras.reorder(order)
}

// topoOrder returns the ordering of the topological sort of the nodes with adjacency matrix `adj`.
func (paras paraList) topoOrder() []int {
	if verbosePage {
		common.Log.Info("topoOrder:")
	}
	n := len(paras)
	visited := make([]bool, n)
	order := make([]int, 0, n)
	llyOrder := paras.llyOrdering()

	// sortNode recursively sorts below node `idx` in the adjacency matrix.
	var sortNode func(idx int)
	sortNode = func(idx int) {
		visited[idx] = true
		for i := 0; i < n; i++ {
			if !visited[i] {
				if paras.before(llyOrder, idx, i) {
					sortNode(i)
				}
			}
		}
		order = append(order, idx) // Should prepend but it's cheaper to append and reverse later.
	}

	for idx := 0; idx < n; idx++ {
		if !visited[idx] {
			sortNode(idx)
		}
	}

	return reversed(order)
}

// before returns true if paras[`i`] comes before paras[`j`].
// before defines an ordering over `paras`.
// a = paras[i],  b= paras[j]
// 1. Line segment `a` comes before line segment `b` if their ranges of x-coordinates overlap and if
//    line segment `a` is above line segment `b` on the page.
// 2. Line segment `a` comes before line segment `b` if `a` is entirely to the left of `b` and if
//    there does not exist a line segment `c` whose y-coordinates are between `a` and `b` and whose
//    range of x coordinates overlaps both `a` and `b`.
// From Thomas M. Breuel "High Performance Document Layout Analysis"
func (paras paraList) before(ordering []int, i, j int) bool {
	a, b := paras[i], paras[j]
	// Breuel's rule 1
	if overlappedXPara(a, b) && a.Lly > b.Lly {
		return true
	}

	// Breuel's rule 2
	if !(a.eBBox.Urx < b.eBBox.Llx) {
		return false
	}

	lo, hi := a.Lly, b.Lly
	if lo > hi {
		hi, lo = lo, hi
	}
	llx := math.Max(a.eBBox.Llx, b.eBBox.Llx)
	urx := math.Min(a.eBBox.Urx, b.eBBox.Urx)

	llyOrder := paras.llyRange(ordering, lo, hi)
	for _, k := range llyOrder {
		if k == i || k == j {
			continue
		}
		c := paras[k]
		if c.eBBox.Llx <= urx && llx <= c.eBBox.Urx {
			return false
		}
	}
	return true
}

// overlappedX returns true if `r0` and `r1` overlap on the x-axis.
func overlappedXPara(r0, r1 *textPara) bool {
	return intersectsX(r0.eBBox, r1.eBBox)
}

// llyOrdering and ordering over the indexes of `paras` sorted by Llx is increasing order.
func (paras paraList) llyOrdering() []int {
	ordering := make([]int, len(paras))
	for i := range paras {
		ordering[i] = i
	}
	sort.SliceStable(ordering, func(i, j int) bool {
		oi, oj := ordering[i], ordering[j]
		return paras[oi].Lly < paras[oj].Lly
	})
	return ordering
}

// llyRange returns the indexes in `paras` of paras p: lo <= p.Llx < hi
func (paras paraList) llyRange(ordering []int, lo, hi float64) []int {
	n := len(paras)
	if hi < paras[ordering[0]].Lly || lo > paras[ordering[n-1]].Lly {
		return nil
	}

	// i0 is the lowest i: lly(i) >= lo
	// i1 is the lowest i: lly(i) > hi
	i0 := sort.Search(n, func(i int) bool { return paras[ordering[i]].Lly >= lo })
	i1 := sort.Search(n, func(i int) bool { return paras[ordering[i]].Lly > hi })

	return ordering[i0:i1]
}

// computeEBBoxes computes the eBBox fields in the elements of `paras`.
func (paras paraList) computeEBBoxes() {
	// fmt.Fprintf(os.Stderr, "\ncomputeEBBoxes: %d\n", len(paras))
	if verbose {
		common.Log.Info("computeEBBoxes:")
	}

	for _, para := range paras {
		para.eBBox = para.PdfRectangle
	}
	paraYNeighbours := paras.yNeighbours()

	for i, aa := range paras {
		a := aa.eBBox
		// [llx, urx] is the reading direction interval for which no paras overlap `a`.
		llx, urx := -1.0e9, +1.0e9

		for _, j := range paraYNeighbours[aa] {
			b := paras[j].eBBox
			if b.Urx < a.Llx { // `b` to left of `a`. no x overlap.
				llx = math.Max(llx, b.Urx)
			} else if a.Urx < b.Llx { // `b` to right of `a`. no x overlap.
				urx = math.Min(urx, b.Llx)
			}
		}

		// llx extends left from `a` and overlaps no other paras.
		// urx extends right from `a` and overlaps no other paras.

		// Go through all paras below `a` within interval [llx, urx] in the reading direction and
		// expand `a` as far as possible to left and right without overlapping any of them.
		for j, bb := range paras {
			b := bb.eBBox
			if i == j || b.Ury > a.Lly {
				continue
			}

			if llx <= b.Llx && b.Llx < a.Llx {
				// If `b` is completely to right of `llx`, extend `a` left to `b`.
				a.Llx = b.Llx
			} else if b.Urx <= urx && a.Urx < b.Urx {
				// If `b` is completely to left of `urx`, extend `a` right to `b`.
				a.Urx = b.Urx
			}
		}
		if verbose {
			fmt.Printf("%4d: %6.2f->%6.2f %q\n", i, aa.eBBox, a, truncate(aa.text(), 50))
		}
		aa.eBBox = a
	}
	if useEBBox {
		for _, para := range paras {
			para.PdfRectangle = para.eBBox
		}
	}
}

type event struct {
	y     float64
	enter bool
	i     int
}

// yNeighbours returns a map {para: indexes of paras that y overap para}
func (paras paraList) yNeighbours() map[*textPara][]int {
	events := make([]event, 2*len(paras))
	for i, para := range paras {
		events[2*i] = event{para.Ury, true, i}
		events[2*i+1] = event{para.Lly, false, i}
	}
	sort.Slice(events, func(i, j int) bool {
		ei, ej := events[i], events[j]
		yi, yj := ei.y, ej.y
		if yi != yj {
			return yi > yj
		}
		if ei.enter != ej.enter {
			return ei.enter
		}
		return i < j
	})

	overlaps := map[int]map[int]bool{}
	olap := map[int]bool{}
	for _, e := range events {
		if e.enter {
			overlaps[e.i] = map[int]bool{}
			for i := range olap {
				if i != e.i {
					overlaps[e.i][i] = true
					overlaps[i][e.i] = true
				}
			}
			olap[e.i] = true
		} else {
			delete(olap, e.i)
		}
	}
	paraNeighbors := map[*textPara][]int{}
	for i, olap := range overlaps {
		para := paras[i]
		neighbours := make([]int, len(olap))
		k := 0
		for j := range olap {
			neighbours[k] = j
			k++
		}
		paraNeighbors[para] = neighbours
	}
	return paraNeighbors
}

// reversed return `order` reversed.
func reversed(order []int) []int {
	rev := make([]int, len(order))
	for i, v := range order {
		rev[len(order)-1-i] = v
	}
	return rev
}

// reorder reorders `para` to the order in `order`.
func (paras paraList) reorder(order []int) {
	sorted := make(paraList, len(paras))
	for i, k := range order {
		sorted[i] = paras[k]
	}
	copy(paras, sorted)
}
