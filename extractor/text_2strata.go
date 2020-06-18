/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/RoaringBitmap/roaring"
	"github.com/unidoc/unipdf/v3/common"
	"github.com/unidoc/unipdf/v3/model"
)

type universe struct {
	idx        *rectIndex // bins[n] = w: n*depthBinPoints <= w.depth < (n+1)*depthBinPoints
	words      []*textWord
	elements   *roaring.Bitmap
	pageHeight float64
}

// text2Strata is a list of word bins arranged by their depth on a page.
// The words in each bin are sorted in reading order.
type text2Strata struct {
	serial             int // Sequence number for debugging.
	model.PdfRectangle     // Bounding box (union of words' in bins bounding boxes).
	fontsize           float64
	elements           *roaring.Bitmap
	*universe
}

const readingNone = -1.0e10

type readingRange struct {
	llxLo, llxHi float64
	urxLo, urxHi float64
}

// // lo <= w.Llx <= hi
// func (s *text2Strata) readingSpanLlx(lo, hi float64) set {
// 	return s.idx.filterLEGE(kLlx, lo, hi, s.elements)
// }

// // lo <= w.Llx  && w.Urx <= hi
// func (s *text2Strata) readingSpan(lo, hi float64) set {
// 	elements := s.idx.filterLE(kUrx, hi, s.elements)
// 	elements = s.idx.filterGE(kLlx, lo, s.elements)
// 	return elements
// }

// makeText2Strata builds a text2Strata from `words` by putting the words into the appropriate
// depth bins.
func makeUniverse(words []*textWord, pageHeight float64) *universe {
	rects := make([]textRect, len(words))
	for i, w := range words {
		rects[i] = textRect{PdfRectangle: w.PdfRectangle, depth: w.depth, fontsize: w.fontsize}
	}
	elements := roaring.New()
	elements.AddRange(0, uint64(len(words)))
	return &universe{
		words:      words,
		idx:        makeRectIndex(rects),
		elements:   elements,
		pageHeight: pageHeight,
	}

}

// newText2Strata returns an empty text2Strata with page height `pageHeight`.
func (u *universe) newText2Strata(page *text2Strata, e uint32) *text2Strata {
	s := &text2Strata{
		serial:   serial.strata,
		universe: u,
		elements: roaring.BitmapOf(e),
	}
	serial.strata++
	word := s.words[e]
	s.PdfRectangle = word.PdfRectangle
	s.fontsize = word.fontsize
	page.elements.Remove(e)
	return s
}

func (u *universe) makeText2Strata() *text2Strata {
	s := &text2Strata{
		serial:   serial.strata,
		universe: u,
		elements: u.elements.Clone(),
	}
	serial.strata++
	return s
}

// scanBand scans the bins for words w:
//     `minDepth` <= w.depth <= `maxDepth` &&  // in the depth diraction
//    `readingOverlap`(`para`, w) &&  // in the reading directon
//     math.Abs(w.fontsize-fontsize) > `fontTol`*fontsize // font size tolerance
// and applies `move2Word`(depthIdx, s,para w) to them.
// If `detectOnly` is true, don't appy move2Word.
// If `freezeDepth` is true, don't update minDepth and maxDepth in scan as words are added.
func (s *text2Strata) scanBand(fontTol, fontsize float64, readingFilter []rectQuery) *roaring.Bitmap {
	elements := s.elements.Clone()
	s.idx.filter(readingFilter, elements)
	// elements = s.filterFont(elements, fontTol, fontsize)
	// elements.And(filter)
	return elements
}

// func (s *text2Strata) filterFont(elements set, fontTol, fontsize float64) set {
// 	if fontTol < 0 {
// 		return elements
// 	}
// 	filtered := set{}
// 	for e := range elements {
// 		word := s.words[e]
// 		fontRatio1 := math.Abs(word.fontsize-fontsize) / fontsize
// 		fontRatio2 := word.fontsize / fontsize
// 		fontRatio := math.Min(fontRatio1, fontRatio2)
// 		if fontRatio > fontTol {
// 			continue
// 		}
// 		filtered.add(e)
// 	}
// 	return filtered
// }

// // depthBand returns the indexes of the words with depth: `minDepth` <= depth <= `maxDepth`.
// func (s *text2Strata) depthBandSet(minDepth, maxDepth float64) *roaring.Bitmap {
// 	yhi := s.pageHeight - minDepth
// 	ylo := s.pageHeight - maxDepth
// 	return s.idx.ge(kLly, ylo).and(s.idx.le(kLly, yhi)) // !@#$
// }

// func (s *text2Strata) depthBand(minDepth, maxDepth float64) []*textWord {
// 	indexes := s.depthBandSet(minDepth, maxDepth)
// 	order := s.idx.orders[kLlx]
// 	words := make([]*textWord, len(indexes))
// 	for _, i := range order {
// 		if indexes.has(i) {
// 			words = append(s.words[i])
// 		}
// 	}
// 	return words
// }

// firstReadingIndex returns the index of the depth bin that starts with that word with the smallest
// reading direction value in the depth region `minDepthIndex` < depth <= minDepthIndex+ 4*fontsize
// This avoids choosing a bin that starts with a superscript word.
func (s *text2Strata) firstReadingWord() uint32 {
	if s == nil {
		panic("s")
	}
	if s.idx == nil {
		panic("s.idx")
	}

	word := s.minDepthWord()
	minDepth := word.depth
	fontsize := word.fontsize
	if fontsize < 0.001 {
		panic(fontsize)
	}
	// common.Log.Info("word0=%s", word)
	elements := s.elements.Clone()
	s.idx.filterLEGE(kDepth, minDepth, minDepth+4*fontsize, elements)
	if elements.IsEmpty() {
		panic("no elements")
	}
	for _, e := range s.idx.orders[kLlx] {
		if elements.Contains(e) {
			return e
		}
	}
	panic("can't happen")
	return s.idx.orders[kLlx][0]
}

func (s *text2Strata) firstReadingWordRange(minDepth, maxDepth float64) (uint32, bool) {
	elements := s.elements.Clone()
	s.idx.filterLEGE(kDepth, minDepth, maxDepth, elements)
	for _, e := range s.idx.orders[kLlx] {
		if elements.Contains(e) {
			return e, true
		}
	}
	return 0, false
}

// empty returns true if `s` has no elements.
func (s *text2Strata) empty() bool {
	return s.elements.IsEmpty()
}

func (s *text2Strata) pullSet(page *text2Strata, elements *roaring.Bitmap) {
	if elements.GetCardinality() == 0 {
		panic(s)
	}
	page.elements.AndNot(elements)
	s.elements.Or(elements)
	for _, e := range elements.ToArray() {
		s.addWord(e)
	}
}

// move2Word moves `word` from 'page'[`depthIdx`] to 'para'[`depthIdx`].
// !@#$ Use same idx
func (s *text2Strata) pullWord(page *text2Strata, e uint32) {
	page.elements.Add(e)
	s.elements.Remove(e)
	s.addWord(e)
}

func (s *text2Strata) addWord(e uint32) {
	word := s.words[e]
	s.PdfRectangle = rectUnion(s.PdfRectangle, word.PdfRectangle)
	s.fontsize = math.Max(s.fontsize, word.fontsize)
}

func (s *text2Strata) allWords() []*textWord {
	words := make([]*textWord, s.elements.GetCardinality())
	for i, e := range s.elements.ToArray() {
		words[i] = s.words[e]
	}
	return words
}

func (s *text2Strata) isHomogenous(w *textWord) bool {
	words := s.allWords()
	words = append(words, w)
	if len(words) == 0 {
		return true
	}
	minFont := words[0].fontsize
	maxFont := minFont
	for _, w := range words {
		if w.fontsize < minFont {
			minFont = w.fontsize
		} else if w.fontsize > maxFont {
			maxFont = w.fontsize
		}
	}
	if maxFont/minFont > 1.3 {
		common.Log.Error("font size range: %.2f - %.2f = %.1fx", minFont, maxFont, maxFont/minFont)
		return false
	}
	return true
}

// merge2Stratas merges paras less than a character width to the left of a strata;
func merge2Stratas(paras []*text2Strata) []*text2Strata {
	for _, para := range paras {
		if para.empty() {
			panic(para)
		}
	}
	if len(paras) <= 1 {
		return paras
	}
	if verbose {
		common.Log.Info("merge2Stratas:")
	}
	// stratas with larger area first, if equal area then taller first
	sort.Slice(paras, func(i, j int) bool {
		pi, pj := paras[i], paras[j]
		ai := pi.Width() * pi.Height()
		aj := pj.Width() * pj.Height()
		if ai != aj {
			return ai > aj
		}
		if pi.Height() != pj.Height() {
			return pi.Height() > pj.Height()
		}
		return i < j
	})

	var merged []*text2Strata
	absorbed := roaring.New()
	for i0 := 0; i0 < len(paras); i0++ {
		if absorbed.ContainsInt(i0) {
			continue
		}
		para0 := paras[i0]
		for i1 := i0 + 1; i1 < len(paras); i1++ {
			if absorbed.ContainsInt(i1) {
				continue
			}
			para1 := paras[i1]
			r := para0.PdfRectangle
			r.Llx -= para0.fontsize * 0.99
			if rectContainsRect(r, para1.PdfRectangle) {
				para0.absorb(para1)
				absorbed.AddInt(i1)
			}
		}
		merged = append(merged, para0)
	}

	if len(paras) != len(merged)+int(absorbed.GetCardinality()) {
		common.Log.Info("merge2Stratas: %d->%d absorbed=%d",
			len(paras), len(merged), absorbed.GetCardinality())
		panic("wrong")
	}
	return merged
}

// absorb absords `strata` into `s`.
func (s *text2Strata) absorb(strata *text2Strata) {
	if strata.empty() {
		panic(strata)
	}
	s.pullSet(strata, strata.elements)
}

// String returns a description of `s`.
func (s *text2Strata) String() string {
	var texts []string
	// for _, depthIdx := range s.depthIndexes() {
	// 	words, _ := s.bins[depthIdx]
	// 	for _, w := range words {
	// 		texts = append(texts, w.text())
	// 	}
	// }
	// return fmt.Sprintf("serial=%d %d %q", s.serial, )
	return fmt.Sprintf("serial=%d %.2f fontsize=%.2f %d %q",
		s.serial, s.PdfRectangle, s.fontsize, len(texts), texts)
}

// minDepth returns the minimum depth that words in `s` touch.
func (s *text2Strata) minDepth() float64 {
	word := s.minDepthWord()
	if word == nil {
		return -1
	}
	return word.depth
	return s.pageHeight - (s.Ury - s.fontsize)
}

func (s *text2Strata) minDepthWord() *textWord {
	order := s.idx.orders[kDepth]
	for i := 0; i < len(order); i++ {
		if s.elements.Contains(order[i]) {
			return s.words[order[i]]
		}
	}
	return nil

}

// maxDepth returns the maximum depth that words in `s` touch.
func (s *text2Strata) maxDepth() float64 {
	order := s.idx.orders[kDepth]
	for i := len(order) - 1; i >= 0; i-- {
		if s.elements.Contains(order[i]) {
			return s.words[order[i]].depth
		}
	}
	return -1
	return s.pageHeight - s.Lly
}

// depth2Index returns a bin index for depth `depth`.
// The returned depthIdx obeys the following rule.
// depthIdx * depthBinPoints <= depth <= (depthIdx+1) * depthBinPoint
func depth2Index(depth float64) int {
	var depthIdx int
	if depth >= 0 {
		depthIdx = int(depth / depthBinPoints)
	} else {
		depthIdx = int(depth/depthBinPoints) - 1
	}
	return depthIdx
}

func (s *text2Strata) text() string {
	words := s.allWords()
	texts := make([]string, len(words))
	for i, w := range words {
		texts[i] = w.text()
	}
	return strings.Join(texts, " ")
}

func (s *text2Strata) depthToLly(depth float64) float64 {
	return s.pageHeight - depth
}
func (s *text2Strata) llyToDepth(lly float64) float64 {
	return s.pageHeight - lly
}
func (s *text2Strata) depthRange() (float64, float64) {
	order := s.idx.orders[kLly]
	n := len(order)
	lo := s.idx.rects[order[0]].Lly
	hi := s.idx.rects[order[n-1]].Lly
	return s.llyToDepth(hi), s.llyToDepth(lo)
}

// func (s *text2Strata) depthIndexes() []int {
// 	order := s.idx.orders[kLly]
// 	n := len(order)
// 	lo := s.idx.rects[order[0]].Lly
// 	hi := s.idx.rects[order[n-1]].Lly

// 	i0 := 0
// 	indexes := []int{i0}
// 	for y := hi; ; y -= depthBinPoints {
// 		i := s.idx.ile(kLly, y)
// 		if i > i0 {
// 			indexes = append(indexes, i)
// 			i0 = i
// 		}
// 		if y < lo {
// 			break
// 		}
// 	}
// 	return indexes
// }

// composePara builds a textPara from the words in `strata`.
// It does this by arranging the words in `strata` into lines.
func (strata *text2Strata) composePara() *textPara {
	para := newTextPara(strata.PdfRectangle)

	if verbosePage {
		common.Log.Info("composePara: para=%s", para)
	}
	if para.PdfRectangle.Width() == 0 {
		panic(strata)
	}

	// build the lines
	for !strata.empty() {
		// seed is the leftmost word from bins near `depthIdx`.
		seed := strata.firstReadingWord()
		// create a new line
		line := strata.newTextLine(seed)

		// Compute the search range.
		// This is based on word0, the first word in the `firstReadingIdx` bin.
		depth := line.words[0].depth
		fontsize := line.words[0].fontsize
		minDepth := depth - lineDepthR*fontsize
		maxDepth := depth + lineDepthR*fontsize
		maxIntraWordGap := maxIntraWordGapR * fontsize
		if verbosePage {
			common.Log.Info(" strata=%d line=%s", strata.elements.GetCardinality(), line)
		}

		// Find the rest of the words in the line.
		for !strata.empty() {
			n0 := strata.elements.GetCardinality()
			// `leftWord` is the left-most word w: minDepth <= w.depth <= maxDepth.
			e, found := strata.firstReadingWordRange(minDepth, maxDepth)
			if !found {
				break
			}
			if !strata.elements.Contains(e) {
				panic(strata)
			}
			leftWord := strata.words[e]
			lastWord := line.words[len(line.words)-1]
			gap := gapReading(leftWord, lastWord)
			if verbosePage {
				common.Log.Info(" strata=%d leftWord=%s", strata.elements.GetCardinality(), leftWord)
			}
			if gap < -maxIntraLineOverlapR*fontsize {
				// New line
				break
			}
			// No `leftWord` or `word` to the left of `leftWord`.
			if gap > maxIntraWordGap {
				break
			}
			// remove `leftWord` from `strata` append it to `line`.
			line.appendWord(leftWord)
			strata.elements.Remove(e)
			if n0 == strata.elements.GetCardinality() {
				panic("no change")
			}
		}

		line.mergeWordFragments()
		// add the line
		para.lines = append(para.lines, line)
	}

	sort.Slice(para.lines, func(i, j int) bool {
		return diffDepthReading(para.lines[i], para.lines[j]) < 0
	})
	if len(para.lines) == 0 {
		panic(para)
	}
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

// newTextLine creates a line seeded with word `s`.words[`seed`] and removes `seed` from `s`.
func (s *text2Strata) newTextLine(seed uint32) *textLine {
	word := s.words[seed]
	line := textLine{
		serial:       serial.line,
		PdfRectangle: word.PdfRectangle,
		fontsize:     word.fontsize,
		depth:        word.depth,
	}
	serial.line++
	line.appendWord(word)
	s.elements.Remove(seed)
	return &line
}

func (s text2Strata) vaidate() {
	show := func() {
		fmt.Fprintln(os.Stderr, "")
		for _, e := range s.elements.ToArray() {
			fmt.Fprintf(os.Stderr, "%4d: %s\n", e, s.words[e])
		}
	}
	err := fmt.Errorf("s=%s words=%s", s.String(), s.elements.String())
	if s.Width() == 0 {
		show()
		panic(err)
	}
	if s.Height() == 0 {
		show()
		panic(err)
	}
	if s.elements.IsEmpty() {
		show()
		panic(err)
	}
}
