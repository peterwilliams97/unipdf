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

// wordBag is a list of word bins arranged by their depth on a page.
// The words in each bin are sorted in reading order.
type wordBag struct {
	serial             int                 // Sequence number for debugging.
	model.PdfRectangle                     // Bounding box (union of words' in bins bounding boxes).
	bins               map[int][]*textWord // bins[n] = w: n*depthBinPoints <= w.depth < (n+1)*depthBinPoints
	pageHeight         float64
	fontsize           float64
}

// makeWordBag builds a wordBag from `words` by putting the words into the appropriate
// depth bins.
func makeWordBag(words []*textWord, pageHeight float64) *wordBag {
	b := newWordBag(pageHeight)
	for _, w := range words {
		depthIdx := depthIndex(w.depth)
		b.bins[depthIdx] = append(b.bins[depthIdx], w)
	}
	b.sort()
	return b
}

// newWordBag returns an empty wordBag with page height `pageHeight`.
func newWordBag(pageHeight float64) *wordBag {
	bag := wordBag{
		serial:       serial.strata,
		bins:         map[int][]*textWord{},
		PdfRectangle: model.PdfRectangle{Urx: -1.0, Ury: -1.0},
		pageHeight:   pageHeight,
	}
	serial.strata++
	return &bag
}

// String returns a description of `s`.
func (b *wordBag) String() string {
	var texts []string
	for _, depthIdx := range b.depthIndexes() {
		words, _ := b.bins[depthIdx]
		for _, w := range words {
			texts = append(texts, w.text())
		}
	}
	return fmt.Sprintf("serial=%d %.2f fontsize=%.2f %d %q",
		b.serial, b.PdfRectangle, b.fontsize, len(texts), texts)
}

// sort sorts the words in each bin in `s` in the reading direction.
func (b *wordBag) sort() {
	for _, bin := range b.bins {
		sort.Slice(bin, func(i, j int) bool { return diffReading(bin[i], bin[j]) < 0 })
	}
}

// minDepth returns the minimum depth that words in `s` touch.
func (b *wordBag) minDepth() float64 {
	return b.pageHeight - (b.Ury - b.fontsize)
}

// maxDepth returns the maximum depth that words in `s` touch.
func (b *wordBag) maxDepth() float64 {
	return b.pageHeight - b.Lly
}

// depthIndex returns a bin index for depth `depth`.
// The returned depthIdx obeys the following rule.
// depthIdx * depthBinPoints <= depth <= (depthIdx+1) * depthBinPoint
func depthIndex(depth float64) int {
	var depthIdx int
	if depth >= 0 {
		depthIdx = int(depth / depthBinPoints)
	} else {
		depthIdx = int(depth/depthBinPoints) - 1
	}
	return depthIdx
}

// depthIndexes returns the sorted keys of b.bins.
func (b *wordBag) depthIndexes() []int {
	if len(b.bins) == 0 {
		return nil
	}
	indexes := make([]int, len(b.bins))
	i := 0
	for idx := range b.bins {
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
// and applies `moveWord`(depthIdx, s,para w) to them.
// If `detectOnly` is true, moveWord is not applied.
// If `freezeDepth` is true, minDepth and maxDepth are not updated in scan as words are added.
func (b *wordBag) scanBand(title string, para *wordBag,
	readingOverlap func(para *wordBag, word *textWord) bool,
	minDepth, maxDepth, fontTol float64,
	detectOnly, freezeDepth bool) int {
	fontsize := para.fontsize
	lineDepth := lineDepthR * fontsize
	n := 0
	minDepth0, maxDepth0 := minDepth, maxDepth
	var newWords []*textWord
	for _, depthIdx := range b.depthBand(minDepth-lineDepth, maxDepth+lineDepth) {
		for _, word := range b.bins[depthIdx] {
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
				moveWord(depthIdx, b, para, word)
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

func (b *wordBag) text() string {
	words := b.allWords()
	texts := make([]string, len(words))
	for i, w := range words {
		texts[i] = w.text()
	}
	return strings.Join(texts, " ")
}

// stratumBand returns the words in b.bins[depthIdx] w: minDepth <= w.depth <= maxDepth.
func (b *wordBag) stratumBand(depthIdx int, minDepth, maxDepth float64) []*textWord {
	if len(b.bins) == 0 {
		return nil
	}
	var words []*textWord
	for _, word := range b.bins[depthIdx] {
		if minDepth <= word.depth && word.depth <= maxDepth {
			words = append(words, word)
		}
	}
	return words
}

// depthBand returns the indexes of the bins with depth: `minDepth` <= depth <= `maxDepth`.
func (b *wordBag) depthBand(minDepth, maxDepth float64) []int {
	if len(b.bins) == 0 {
		return nil
	}

	return b.depthRange(b.getDepthIdx(minDepth), b.getDepthIdx(maxDepth))
}

// depthRange returns the sorted keys of b.bins for depths indexes [`minDepth`,`maxDepth`).
func (b *wordBag) depthRange(minDepthIdx, maxDepthIdx int) []int {
	indexes := b.depthIndexes()
	var rangeIndexes []int
	for _, depthIdx := range indexes {
		if minDepthIdx <= depthIdx && depthIdx <= maxDepthIdx {
			rangeIndexes = append(rangeIndexes, depthIdx)
		}
	}
	return rangeIndexes
}

// firstReadingIndex returns the index of the depth bin that starts with that word with the smallest
// reading direction value in the depth region `minDepthIndex` < depth <= minDepthIndex+ 4*fontsize
// This avoids choosing a bin that starts with a superscript word.
func (b *wordBag) firstReadingIndex(minDepthIdx int) int {
	firstReadingIdx := minDepthIdx
	firstReadingWords := b.getStratum(firstReadingIdx)
	fontsize := firstReadingWords[0].fontsize
	minDepth := float64(minDepthIdx+1) * depthBinPoints
	for _, depthIdx := range b.depthBand(minDepth, minDepth+4*fontsize) {
		words := b.getStratum(depthIdx)
		if diffReading(words[0], firstReadingWords[0]) < 0 {
			firstReadingIdx = depthIdx
			firstReadingWords = b.getStratum(firstReadingIdx)
		}
	}
	return firstReadingIdx
}

// getDepthIdx returns the index into `b.bins` for depth axis value `depth`.
// Caller must check that len(b.bins) > 0.
func (b *wordBag) getDepthIdx(depth float64) int {
	indexes := b.depthIndexes()
	depthIdx := depthIndex(depth)
	if depthIdx < indexes[0] {
		return indexes[0]
	}
	if depthIdx > indexes[len(indexes)-1] {
		return indexes[len(indexes)-1]
	}
	return depthIdx
}

// empty returns true if the depth bin with index `depthIdx` is empty.
// NOTE: We delete bins as soon as they become empty so we just have to check for the bin's existence.
func (b *wordBag) empty(depthIdx int) bool {
	_, ok := b.bins[depthIdx]
	return !ok
}

// getStratum returns a copy of `p`.bins[`depthIdx`].
// getStratum is guaranteed to return a non-nil value. It must be called with a valid depth index.
// NOTE: We need to return a copy because remove() and other functions manipulate the array
// underlying the slice.
func (b *wordBag) getStratum(depthIdx int) []*textWord {
	words := b.bins[depthIdx]
	dup := make([]*textWord, len(words))
	copy(dup, words)
	return dup
}

// moveWord moves `word` from 'page'[`depthIdx`] to 'para'[`depthIdx`].
func moveWord(depthIdx int, page, para *wordBag, word *textWord) {
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

func (b *wordBag) allWords() []*textWord {
	var wordList []*textWord
	for _, words := range b.bins {
		wordList = append(wordList, words...)
	}
	return wordList
}

// removeWord removes `word`from `s`.bins[`depthIdx`].
// NOTE: We delete bins as soon as they become empty to save code that calls other wordBag
// functions from having to check for empty bins.
// !@#$ Find a more efficient way of doing this.
func (b *wordBag) removeWord(depthIdx int, word *textWord) {
	words := removeWord(b.getStratum(depthIdx), word)
	if len(words) == 0 {
		delete(b.bins, depthIdx)
	} else {
		b.bins[depthIdx] = words
	}
}

// mergeStratas merges paras less than a character width to the left of a stata;
func mergeStratas(paras []*wordBag) []*wordBag {
	if len(paras) <= 1 {
		return paras
	}
	if verbose {
		common.Log.Info("mergeStratas:")
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
	var merged []*wordBag
	absorbed := map[int]struct{}{}
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
				absorbed[i1] = struct{}{}
			}
		}
		merged = append(merged, para0)
	}

	if len(paras) != len(merged)+len(absorbed) {
		common.Log.Error("mergeStratas: %d->%d absorbed=%d",
			len(paras), len(merged), len(absorbed))
	}
	return merged
}

// absorb combines the words from `bag` into `b`.
func (b *wordBag) absorb(bag *wordBag) {
	var absorbed []string
	for depthIdx, words := range bag.bins {
		for _, word := range words {
			moveWord(depthIdx, bag, b, word)
			absorbed = append(absorbed, word.text())
		}
	}
	if verbose {
		common.Log.Info("absorb: %d %q", len(absorbed), absorbed)
	}
}
