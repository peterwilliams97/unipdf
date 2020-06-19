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
	llyOrder := paras.makeOrder()

	// bfr := map[uint64]int{}

	// sortNode recursively sorts below node `idx` in the adjacency matrix.
	var sortNode func(idx int)
	sortNode = func(idx int) {
		visited[idx] = true
		for i := 0; i < n; i++ {
			if !visited[i] {
				// k := uint64(idx)*0x1000000 + uint64(i)
				// bfr[k]++
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

	// var counts []uint64
	// for k := range bfr {
	// 	counts = append(counts, k)
	// }
	// common.Log.Notice("====================")
	// common.Log.Notice("n=%d bfr=%d counts=%d", n, len(bfr), len(counts))
	// sort.Slice(counts, func(i, j int) bool {
	// 	ci, cj := counts[i], counts[j]
	// 	ni, nj := bfr[ci], bfr[cj]
	// 	if ni != nj {
	// 		return ni > nj
	// 	}
	// 	return ci < cj
	// })
	// total := 0
	// for _, cnt := range bfr {
	// 	total += cnt
	// 	if cnt > 1 {
	// 		panic(cnt)
	// 	}
	// 	if total > n {
	// 		panic(cnt)
	// 	}
	// }

	// common.Log.Notice("====================")
	// for i, k := range counts {
	// 	k0 := k / 0x1000000
	// 	k1 := k % 0x1000000
	// 	cnt := bfr[k]
	// 	if i < 10 {
	// 		fmt.Printf("%4d: %4d %4d : %4d\n", i, k0, k1, cnt)
	// 	}
	// 	if cnt > 1 {
	// 		panic(cnt)
	// 	}
	// }

	return reversed(order)
}

// before defines an ordering over `paras`.
// before returns true if `a` comes before `b`.
// 1. Line segment `a` comes before line segment `b` if their ranges of x-coordinates overlap and if
//    line segment `a` is above line segment `b` on the page.
// 2. Line segment `a` comes before line segment `b` if `a` is entirely to the left of `b` and if
//    there does not exist a line segment `c` whose y-coordinates are between `a` and `b` and whose
//    range of x coordinates overlaps both `a` and `b`.
// From Thomas M. Breuel "High Performance Document Layout Analysis"
func (paras paraList) before(order []int, i, j int) bool {
	a, b := paras[i], paras[j]
	// Breuel's rule 1
	if overlappedXPara(a, b) && a.Lly > b.Lly {
		return true
	}

	// Breuel's rule 2
	if !(a.eBBox.Urx < b.eBBox.Llx) {
		return false
	}
	lo := a.Lly
	hi := b.Lly
	if lo > hi {
		hi, lo = lo, hi
	}
	llyOrder := paras.indexRange(order, lo, hi)
	for _, k := range llyOrder {
		c := paras[k]
		if k == i || k == j {
			continue
		}
		if !(lo < c.Lly && c.Lly < hi) {
			continue
		}
		if overlappedXPara(a, c) && overlappedXPara(c, b) {
			return false
		}
	}
	return true
}

func (paras paraList) makeOrder() []int {
	order := make([]int, len(paras))
	for i := range paras {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		oi, oj := order[i], order[j]
		return paras[oi].Lly < paras[oj].Lly
	})

	return order
}

func (paras paraList) indexRange(order []int, lo, hi float64) []int {
	depth := func(i int) float64 { return paras[order[i]].Lly }
	n := len(paras)
	if hi < depth(0) {
		return nil
	}
	if lo > depth(n-1) {
		return nil
	}

	// i0 is the lowest i: val(i) > z so i-1 is the greatest i: val(i) <= z
	i0 := sort.Search(n, func(i int) bool { return depth(i) >= lo })
	// fmt.Printf("##le %s %.1f >= %.1f => i=%d\n", k, val(i), z, i)
	if !(0 <= i0) {
		panic(paras)
	}

	// i1 is the lowest i: val(i) > z so i-1 is the greatest i: val(i) <= z
	i1 := sort.Search(n, func(i int) bool { return depth(i) > hi })
	// fmt.Printf("##le %s %.1f >= %.1f => i=%d\n", k, val(i), z, i)
	if !(0 <= i1) {
		panic(paras)
	}
	return order[i0:i1]
}

// overlappedX returns true if `r0` and `r1` overlap on the x-axis. !@#$ There is another version
// of this!
func overlappedXPara(r0, r1 *textPara) bool {
	return intersectsX(r0.eBBox, r1.eBBox)
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
	paraClearing := paras.findClearings()

	for i, aa := range paras {
		a := aa.eBBox
		// [llx, urx] is the reading direction interval for which no paras overlap `a`.

		llx := -1.0e9
		urx := +1.0e9
		if false {
			for j, bb := range paras {
				b := bb.eBBox
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
		} else {
			clearing := paraClearing[aa]
			llx, urx = clearing.llx, clearing.urx
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

			// If `b` is completely to right of `llx`, extend `a` left to `b`.
			if llx <= b.Llx && b.Llx < a.Llx {
				a.Llx = b.Llx
			}

			// If `b` is completely to left of `urx`, extend `a` right to `b`.
			if b.Urx <= urx && a.Urx < b.Urx {
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

type event struct {
	y     float64
	enter bool
	i     int
}

type clearing struct {
	llx float64
	urx float64
}

func (paras paraList) findClearings() map[*textPara]clearing {
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
	inScan := map[int]bool{}
	for _, e := range events {
		if e.enter {
			overlaps[e.i] = map[int]bool{}
			for i := range inScan {
				overlaps[e.i][i] = true
			}
			inScan[e.i] = true
		} else {
			delete(inScan, e.i)
		}
	}
	paraNeighbors := map[*textPara]clearing{}
	for i, olap := range overlaps {
		aa := paras[i]
		a := aa.eBBox
		llx, urx := -1.0e9, +1.0e9
		for j := range olap {
			bb := paras[j]
			b := bb.eBBox
			if b.Urx < a.Llx && b.Urx > llx {
				// `b` to left of `a`. no x overlap.
				llx = b.Urx
			} else if a.Urx < b.Llx && b.Llx < urx {
				// `b` to right of `a`. no x overlap.
				urx = b.Llx
			}
		}
		paraNeighbors[aa] = clearing{llx: llx, urx: urx}
	}

	return paraNeighbors
}
