/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

/*
  Mods:
	depth -> depth
	textStrata -> stratum
	textPara -> para
*/

package extractor

import (
	"github.com/unidoc/unipdf/v3/model"
)

var serial serialState

type serialState struct {
	mark int
	word int
	bins int
	line int
	para int
}

func (serial *serialState) reset() {
	var empty serialState
	*serial = empty
}

/*
 * Sorting functions.
 *
 * There are two directions:
 *  - reading. Left to right in English
 *  - depth (aka non-reading).  Top to botttom in English.
 *
 * Text is read in reading then depth order.
 *
 * TODO(peterwilliams97): Add support for other reading orders and page rotations
 */

// bounded is an object with a bounding box. A mark, word, line or para.
type bounded interface {
	bbox() model.PdfRectangle
}

// func center(a bounded) transform.Point {
// 	box := a.bbox()
// 	return transform.Point{
// 		X: 0.5 * (box.Llx + box.Urx),
// 		Y: 0.5 * (box.Lly + box.Ury)}
// }

// getDepth returns the depth of `a` on a page of size `pageSize`.
func getDepth(pageSize model.PdfRectangle, a bounded) float64 {
	return pageSize.Ury - a.bbox().Lly
}

// diffReading returns `a` - `b` in the reading direction.
func diffReading(a, b bounded) float64 {
	return a.bbox().Llx - b.bbox().Llx
}

// func boundedUnion(objs ...bounded) model.PdfRectangle {
// 	rect := objs[0].bbox()
// 	for _, r := range objs[1:] {
// 		rect = rectUnion(rect, r.bbox())
// 	}
// 	return rect
// }

// diffDepth returns `a` - `b` in the depth direction..
func diffDepth(a, b bounded) float64 {
	return bboxDepth(a) - bboxDepth(b)
}

// diffReadingDepth returns `a` - `b` in the reading then depth direction..
func diffReadingDepth(a, b bounded) float64 {
	diff := diffReading(a, b)
	if !isZero(diff) {
		return diff
	}
	return diffDepth(a, b)
}

// diffDepthReading returns `a` - `b` in the depth then reading directions
func diffDepthReading(a, b bounded) float64 {
	cmp := diffDepth(a, b)
	if !isZero(cmp) {
		return cmp
	}
	return diffReading(a, b)
}

// gapReading returns the reading direction gap between `a` and the following object `b` in the
// reading direction.
func gapReading(a, b bounded) float64 {
	return a.bbox().Llx - b.bbox().Urx
}

// bboxDepth returns the relative depth of `b`. Depth is only used for comparison so we don't care
// about its absolute value
func bboxDepth(b bounded) float64 {
	return -b.bbox().Lly
}

// readingOverlapLeft returns true is the left of `word` is in within `para` or delta to its right
func readingOverlapLeft(para *textStrata, word *textWord, delta float64) bool {
	return para.Urx <= word.Llx && word.Llx < para.Urx+delta
}

// readingOverlapPlusGap returns true if `word` overlaps [para.Llx-maxIntraReadingGap, para.Urx+maxIntraReadingGap]
// in the reading direction.
func readingOverlapPlusGap(para *textStrata, word *textWord, maxIntraReadingGap float64) bool {
	return word.Llx < para.Urx+maxIntraReadingGap && para.Llx-maxIntraReadingGap < word.Urx
}

// partial return 'overlap`(*textStrata, *textWord, `param`) bool.
func partial(overlap func(*textStrata, *textWord, float64) bool,
	param float64) func(*textStrata, *textWord) bool {
	return func(para *textStrata, word *textWord) bool {
		return overlap(para, word, param)
	}
}

// overlapped returns true if `a` and `b` overlap.
func overlapped(a, b bounded) bool {
	return overlappedX(a, b) && overlappedY(a, b)
}

// overlappedX returns true if `a` and `b` overlap in the x direction.
func overlappedX(a, b bounded) bool {
	return overlappedXRect(a.bbox(), b.bbox())
}

// overlappedY returns true if `a` and `b` overlap in the y direction.
func overlappedY(a, b bounded) bool {
	return overlappedYRect(a.bbox(), b.bbox())
}

// overlappedXRect returns true if `r0` and `r1` overlap in the x direction.
func overlappedXRect(r0, r1 model.PdfRectangle) bool {
	return (r0.Llx <= r1.Llx && r1.Llx <= r0.Urx) || (r0.Llx <= r1.Urx && r1.Urx <= r0.Urx)
}

// overlappedYRect returns true if `r0` and `r1` overlap in the y direction.
func overlappedYRect(r0, r1 model.PdfRectangle) bool {
	return (r0.Lly <= r1.Lly && r1.Lly <= r0.Ury) || (r0.Lly <= r1.Ury && r1.Ury <= r0.Ury)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
