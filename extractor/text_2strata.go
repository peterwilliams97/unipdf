/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/unidoc/unipdf/v3/common"
	"github.com/unidoc/unipdf/v3/model"
)

// text2Strata is a list of word bins arranged by their depth on a page.
// The words in each bin are sorted in reading order.
type text2Strata struct {
	serial             int                 // Sequence number for debugging.
	model.PdfRectangle                     // Bounding box (union of words' in bins bounding boxes).
	bins               map[int][]*textWord // bins[n] = w: n*depthBinPoints <= w.depth < (n+1)*depthBinPoints
	pageHeight         float64
	fontsize           float64
}

// makeText2Strata builds a text2Strata from `words` by putting the words into the appropriate
// depth bins.
func makeText2Strata(words []*textWord, pageHeight float64) *text2Strata {
	s := newText2Strata(pageHeight)
	for _, w := range words {
		depthIdx := depth2Index(w.depth)
		s.bins[depthIdx] = append(s.bins[depthIdx], w)
	}
	s.sort()
	return s
}

// newText2Strata returns an empty text2Strata with page height `pageHeight`.
func newText2Strata(pageHeight float64) *text2Strata {
	strata := text2Strata{
		serial:       serial.strata,
		bins:         map[int][]*textWord{},
		PdfRectangle: model.PdfRectangle{Urx: -1.0, Ury: -1.0},
		pageHeight:   pageHeight,
	}
	serial.strata++
	return &strata
}

// String returns a description of `s`.
func (s *text2Strata) String() string {
	var texts []string
	for _, depthIdx := range s.depthIndexes() {
		words, _ := s.bins[depthIdx]
		for _, w := range words {
			texts = append(texts, w.text())
		}
	}
	// return fmt.Sprintf("serial=%d %d %q", s.serial, )
	return fmt.Sprintf("serial=%d %.2f fontsize=%.2f %d %q",
		s.serial, s.PdfRectangle, s.fontsize, len(texts), texts)
}

// sort sorts the words in each bin in `s` in the reading direction.
func (s *text2Strata) sort() {
	for _, bin := range s.bins {
		sort.Slice(bin, func(i, j int) bool { return diffReading(bin[i], bin[j]) < 0 })
	}
}

// minDepth returns the minimum depth that words in `s` touch.
func (s *text2Strata) minDepth() float64 {
	return s.pageHeight - (s.Ury - s.fontsize)
}

// maxDepth returns the maximum depth that words in `s` touch.
func (s *text2Strata) maxDepth() float64 {
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

// depthIndexes returns the sorted keys of s.bins.
func (s *text2Strata) depthIndexes() []int {
	if len(s.bins) == 0 {
		return nil
	}
	indexes := make([]int, len(s.bins))
	i := 0
	for idx := range s.bins {
		indexes[i] = idx
		i++
	}
	sort.Ints(indexes)
	return indexes
}

// scanBand scans the bins for words w:
//     `minDepth` <= w.depth <= `maxDepth` &&  // in the depth diraction
//    `readingOverlap`(`para`, w) &&  // in the reading directon
//     math.Abs(w.fontsize-fontsize) > `fontTol`*fontsize // font size tolerance
// and applies `move2Word`(depthIdx, s,para w) to them.
// If `detectOnly` is true, don't appy move2Word.
// If `freezeDepth` is true, don't update minDepth and maxDepth in scan as words are added.
func (s *text2Strata) scanBand(title string, para *text2Strata,
	readingOverlap func(para *text2Strata, word *textWord) bool,
	minDepth, maxDepth, fontTol float64,
	detectOnly, freezeDepth bool) int {
	fontsize := para.fontsize
	lineDepth := lineDepthR * fontsize
	n := 0
	minDepth0, maxDepth0 := minDepth, maxDepth
	var newWords []*textWord
	for _, depthIdx := range s.depthBand(minDepth-lineDepth, maxDepth+lineDepth) {
		for _, word := range s.bins[depthIdx] {
			if !(minDepth-lineDepth <= word.depth && word.depth <= maxDepth+lineDepth) {
				continue
			}

			if !readingOverlap(para, word) {
				continue
			}
			fontRatio1 := math.Abs(word.fontsize-fontsize) / fontsize
			fontRatio2 := word.fontsize / fontsize
			fontRatio := math.Min(fontRatio1, fontRatio2)
			if fontTol > 0 {
				if fontRatio > fontTol {
					continue
				}
			}

			if !detectOnly {
				move2Word(depthIdx, s, para, word)
			}
			newWords = append(newWords, word)
			n++
			if !freezeDepth {
				if word.depth < minDepth {
					minDepth = word.depth
				}
				if word.depth > maxDepth {
					maxDepth = word.depth
				}
			}
			// Has no effect on results
			// fontsize = para.fontsize
			// lineDepth = lineDepthR * fontsize
			if detectOnly {
				break
			}
		}
	}
	if verbose {
		if len(title) > 0 {
			common.Log.Info("scanBand: %s [%.2f %.2f]->[%.2f %.2f] para=%.2f fontsize=%.2f %q",
				title,
				minDepth0, maxDepth0,
				minDepth, maxDepth,
				para.PdfRectangle, para.fontsize, truncate(para.text(), 20))
			for i, word := range newWords {
				// fmt.Printf("%4d: %s\n", i, word)
				fmt.Printf("  %q", word.text())
				if i >= 5 {
					break
				}
			}
			if len(newWords) > 0 {
				fmt.Println()
			}
		}
	}
	return n
}

func (para *text2Strata) text() string {
	words := para.allWords()
	texts := make([]string, len(words))
	for i, w := range words {
		texts[i] = w.text()
	}
	return strings.Join(texts, " ")
}

// stratumBand returns the words in s.bins[depthIdx] w: minDepth <= w.depth <= maxDepth.
func (s *text2Strata) stratumBand(depthIdx int, minDepth, maxDepth float64) []*textWord {
	if len(s.bins) == 0 {
		return nil
	}
	var words []*textWord
	for _, word := range s.bins[depthIdx] {
		if minDepth <= word.depth && word.depth <= maxDepth {
			words = append(words, word)
		}
	}
	return words
}

// depthBand returns the indexes of the bins with depth: `minDepth` <= depth <= `maxDepth`.
func (s *text2Strata) depthBand(minDepth, maxDepth float64) []int {
	if len(s.bins) == 0 {
		return nil
	}
	return s.depthRange(s.getDepthIdx(minDepth), s.getDepthIdx(maxDepth))
}

// depthRange returns the sorted keys of s.bins for depths indexes [`minDepth`,`maxDepth`).
func (s *text2Strata) depthRange(minDepthIdx, maxDepthIdx int) []int {
	indexes := s.depthIndexes()
	var rangeIndexes []int
	for _, depthIdx := range indexes {
		if minDepthIdx <= depthIdx && depthIdx <= maxDepthIdx {
			rangeIndexes = append(rangeIndexes, depthIdx)
		}
	}
	return rangeIndexes
}

// getDepthIdx returns the index into `s.bins` for depth axis value `depth`.
func (s *text2Strata) getDepthIdx(depth float64) int {
	if len(s.bins) == 0 {
		panic("NOT ALLOWED")
	}
	indexes := s.depthIndexes()
	depthIdx := depth2Index(depth)
	if depthIdx < indexes[0] {
		return indexes[0]
	}
	if depthIdx > indexes[len(indexes)-1] {
		return indexes[len(indexes)-1]
	}
	return depthIdx
}

// firstReadingIndex returns the index of the depth bin that starts with that word with the smallest
// reading direction value in the depth region `minDepthIndex` < depth <= minDepthIndex+ 4*fontsize
// This avoids choosing a bin that starts with a superscript word.
func (s *text2Strata) firstReadingIndex(minDepthIdx int) int {
	firstReadingIdx := minDepthIdx
	firstReadingWords := s.getStratum(firstReadingIdx)
	fontsize := firstReadingWords[0].fontsize
	minDepth := float64(minDepthIdx+1) * depthBinPoints
	for _, depthIdx := range s.depthBand(minDepth, minDepth+4*fontsize) {
		words := s.getStratum(depthIdx)
		if diffReading(words[0], firstReadingWords[0]) < 0 {
			firstReadingIdx = depthIdx
			firstReadingWords = s.getStratum(firstReadingIdx)
		}
	}
	return firstReadingIdx
}

// empty returns true if the depth bin with index `depthIdx` is empty.
// NOTE: We delete bins as soon as they become empty so we just have to check for the bin's existence.
func (s *text2Strata) empty(depthIdx int) bool {
	_, ok := s.bins[depthIdx]
	return !ok
}

// getStratum returns a copy of `p`.bins[`depthIdx`].
// getStratum is guaranteed to return a non-nil value. It must be called with a valid depth index.
// NOTE: We need to return a copy because remove() and other functions manipulate the array
// underlying the slice.
func (s *text2Strata) getStratum(depthIdx int) []*textWord {
	words := s.bins[depthIdx]
	if words == nil {
		panic("NOT ALLOWED")
	}
	dup := make([]*textWord, len(words))
	copy(dup, words)
	return dup
}

// move2Word moves `word` from 'page'[`depthIdx`] to 'para'[`depthIdx`].
func move2Word(depthIdx int, page, para *text2Strata, word *textWord) {
	if para.Llx > para.Urx {
		para.PdfRectangle = word.PdfRectangle
	} else {
		para.PdfRectangle = rectUnion(para.PdfRectangle, word.PdfRectangle)
	}
	if word.fontsize > para.fontsize {
		para.fontsize = word.fontsize
	}
	para.bins[depthIdx] = append(para.bins[depthIdx], word)
	page.removeWord(depthIdx, word)
}

func (s *text2Strata) allWords() []*textWord {
	var wordList []*textWord
	for _, words := range s.bins {
		wordList = append(wordList, words...)
	}
	return wordList
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

// removeWord removes `word`from `s`.bins[`depthIdx`].
// NOTE: We delete bins as soon as they become empty to save code that calls other text2Strata
// functions from having to check for empty bins.
// !@#$ Find a more efficient way of doing this.
func (s *text2Strata) removeWord(depthIdx int, word *textWord) {
	words := removeWord(s.getStratum(depthIdx), word)
	if len(words) == 0 {
		delete(s.bins, depthIdx)
	} else {
		s.bins[depthIdx] = words
	}
}

// merge2Stratas merges paras less than a character width to the left of a stata;
func merge2Stratas(paras []*text2Strata) []*text2Strata {
	if len(paras) <= 1 {
		return paras
	}
	if verbose {
		common.Log.Info("merge2Stratas:")
	}
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
	merged := []*text2Strata{paras[0]}
	absorbed := map[int]bool{0: true}
	numAbsorbed := 0
	for i0 := 0; i0 < len(paras); i0++ {
		if _, ok := absorbed[i0]; ok {
			continue
		}
		para0 := paras[i0]
		for i1 := i0 + 1; i1 < len(paras); i1++ {
			if _, ok := absorbed[i0]; ok {
				continue
			}
			para1 := paras[i1]
			r := para0.PdfRectangle
			r.Llx -= para0.fontsize * 0.99
			if rectContainsRect(r, para1.PdfRectangle) {
				para0.absorb(para1)
				absorbed[i1] = true
				numAbsorbed++
			}
		}
		merged = append(merged, para0)
		absorbed[i0] = true
	}

	if len(paras) != len(merged)+numAbsorbed {
		common.Log.Info("merge2Stratas: %d->%d absorbed=%d", len(paras), len(merged), numAbsorbed)
		panic("wrong")
	}
	return merged
}

// absorb combines `word` into `w`.
func (s *text2Strata) absorb(strata *text2Strata) {
	var absorbed []string
	for depthIdx, words := range strata.bins {
		for _, word := range words {
			move2Word(depthIdx, strata, s, word)
			absorbed = append(absorbed, word.text())
		}
	}
	if verbose {
		common.Log.Info("absorb: %d %q", len(absorbed), absorbed)
	}
}
