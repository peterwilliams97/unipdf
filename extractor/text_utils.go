/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"fmt"
	"math"
	"sort"
	"unicode"

	"github.com/unidoc/unipdf/v3/common"
)

// TOL is the tolerance for coordinates to be consideted equal. It is big enough to cover all
// rounding errors and small enough that TOL point differences on a page aren't visible.
const TOL = 1.0e-6

// isZero returns true if x is with TOL of 0.0
func isZero(x float64) bool {
	return math.Abs(x) < TOL
}

// minInt return the lesser of `a` and `b`.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// maxInt return the greater of `a` and `b`.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// addNeighbours fills out the below and right fields of the paras in `paras`.
// For each para `a`:
//    a.below is the unique highest para completely below `a` that overlaps it in the x-direction
//    a.right is the unique leftmost para completely to the right of `a` that overlaps it in the y-direction
func (paras paraList) addNeighbours() {
	splitYNeighbours := func(neighbours []int, para *textPara) ([]*textPara, []*textPara) {
		leftElts := make([]*textPara, 0, len(neighbours)-1)
		rightElts := make([]*textPara, 0, len(neighbours)-1)
		for _, k := range neighbours {
			b := paras[k]
			if b.Urx <= para.Llx {
				leftElts = append(leftElts, b)
			} else if b.Llx >= para.Urx {
				rightElts = append(rightElts, b)
			}
		}
		return leftElts, rightElts
	}
	splitXNeighbours := func(neighbours []int, para *textPara) ([]*textPara, []*textPara) {
		aboveElts := make([]*textPara, 0, len(neighbours)-1)
		belowElts := make([]*textPara, 0, len(neighbours)-1)
		for _, k := range neighbours {
			b := paras[k]
			if b.Ury <= para.Lly {
				belowElts = append(belowElts, b)
			} else if b.Lly >= para.Ury {
				aboveElts = append(aboveElts, b)
			}
		}
		return aboveElts, belowElts
	}

	paraNeighbours := paras.yNeighbours(cellMarginY)
	for _, para := range paras {
		neighbours := paraNeighbours[para]
		if len(neighbours) == 0 {
			continue
		}
		leftElts, rightElts := splitYNeighbours(neighbours, para)
		if len(leftElts) == 0 && len(rightElts) == 0 {
			continue
		}
		if len(leftElts) > 0 {
			left := leftElts[0]
			for _, b := range leftElts[1:] {
				if b.Urx >= left.Urx {
					left = b
				}
			}
			for _, b := range leftElts {
				if b != left && b.Urx > left.Llx {
					left = nil
					break
				}
			}
			if left != nil && intersectsY(para.PdfRectangle, left.PdfRectangle) {
				para.left = left
			}
			// common.Log.Notice("para=%s\n\t left=%s\n\t+left=%s", para, left, para.left)
		}
		if len(rightElts) > 0 {
			right := rightElts[0]
			for _, b := range rightElts[1:] {
				if b.Llx <= right.Llx {
					right = b
				}
			}
			for _, b := range rightElts {
				if b != right && b.Llx < right.Urx {
					right = nil
					break
				}
			}
			if right != nil && intersectsY(para.PdfRectangle, right.PdfRectangle) {
				para.right = right
			}
			// common.Log.Notice("para=%s\n\t left=%s\n\t+left=%s", para, left, para.left)
		}
	}

	paraNeighbours = paras.xNeighbours(cellMarginX)
	for _, para := range paras {
		neighbours := paraNeighbours[para]
		if len(neighbours) == 0 {
			continue
		}
		aboveElts, belowElts := splitXNeighbours(neighbours, para)
		if len(aboveElts) == 0 && len(belowElts) == 0 {
			continue
		}

		if len(belowElts) > 0 {
			below := belowElts[0]
			for _, b := range belowElts[1:] {
				if b.Ury >= below.Ury {
					below = b
				}
			}
			for _, b := range belowElts {
				if b != below && b.Ury > below.Lly {
					below = nil
					break
				}
			}
			if below != nil && intersectsX(para.PdfRectangle, below.PdfRectangle) {
				para.below = below
			}
			// common.Log.Notice("para=%s\n\t left=%s\n\t+left=%s", para, left, para.left)
		}
		if len(aboveElts) > 0 {
			above := aboveElts[0]
			for _, b := range aboveElts[1:] {
				if b.Lly <= above.Lly {
					above = b
				}
			}
			for _, b := range aboveElts {
				if b != above && b.Lly < above.Ury {
					above = nil
					break
				}
			}
			if above != nil && intersectsX(para.PdfRectangle, above.PdfRectangle) {
				para.above = above
			}
			// common.Log.Notice("para=%s\n\t left=%s\n\t+left=%s", para, left, para.left)
		}
	}

	if true {
		for _, para := range paras {
			if para.left != nil && para.left.right != para {
				para.left = nil
			}
			if para.above != nil && para.above.below != para {
				para.above = nil
			}
			// }
			// for _, para := range paras {
			if para.right != nil && para.right.left != para {
				// common.Log.Notice("Remove right: %s", para)
				para.right = nil
			}
			if para.below != nil && para.below.above != para {
				// common.Log.Notice("Remove belwo: %s", para)
				para.below = nil
			}
		}
	}

	if false {
		show := func(p *textPara) string {
			if p == nil {
				return ""
			}
			text := truncate2(p.text(), 60)
			return fmt.Sprintf("%q", text)
		}

		common.Log.Notice("addNeighbours %d", len(paras))
		for i, para := range paras {
			fmt.Printf("%d: %s\n", i, show(para))
			fmt.Printf("      left: %s\n", show(para.left))
			fmt.Printf("     above: %s\n", show(para.above))
			fmt.Printf("     right: %s\n", show(para.right))
			fmt.Printf("     below: %s\n", show(para.below))
		}
		fmt.Println("=======================")
		panic("done")
	}
}

// xNeighbours returns a map {para: indexes of paras that x-overlap para}.
func (paras paraList) xNeighbours(margin float64) map[*textPara][]int {
	events := make([]event, 2*len(paras))
	if margin == 0 {
		for i, para := range paras {
			events[2*i] = event{para.Llx, true, i}
			events[2*i+1] = event{para.Urx, false, i}
		}
	} else {
		for i, para := range paras {
			events[2*i] = event{para.Llx - margin*para.fontsize(), true, i}
			events[2*i+1] = event{para.Urx + margin*para.fontsize(), false, i}
		}
	}
	return paras.eventNeighbours(events)
}

// yNeighbours returns a map {para: indexes of paras that y-overlap para}.
func (paras paraList) yNeighbours(margin float64) map[*textPara][]int {
	events := make([]event, 2*len(paras))
	if margin == 0 {
		for i, para := range paras {
			events[2*i] = event{para.Lly, true, i}
			events[2*i+1] = event{para.Ury, false, i}
		}
	} else {
		for i, para := range paras {
			events[2*i] = event{para.Lly - margin*para.fontsize(), true, i}
			events[2*i+1] = event{para.Ury + margin*para.fontsize(), false, i}
		}
	}
	return paras.eventNeighbours(events)
}

// event is an entry or exit from an interval while scanning.
type event struct {
	z     float64 // Coordinate in the scanning direction.
	enter bool    // True if entering the interval, false it leaving.
	i     int     // Index of the interval
}

// eventNeighbours returns a map {para: indexes of paras that overlap para in `events`}.
func (paras paraList) eventNeighbours(events []event) map[*textPara][]int {
	sort.Slice(events, func(i, j int) bool {
		ei, ej := events[i], events[j]
		zi, zj := ei.z, ej.z
		if zi != zj {
			return zi < zj
		}
		if ei.enter != ej.enter {
			return ei.enter
		}
		return i < j
	})

	overlaps := map[int]map[int]struct{}{}
	olap := map[int]struct{}{}
	for _, e := range events {
		if e.enter {
			overlaps[e.i] = map[int]struct{}{}
			for i := range olap {
				if i != e.i {
					overlaps[e.i][i] = struct{}{}
					overlaps[i][e.i] = struct{}{}
				}
			}
			olap[e.i] = struct{}{}
		} else {
			delete(olap, e.i)
		}
	}

	paraNeighbors := map[*textPara][]int{}
	for i, olap := range overlaps {
		para := paras[i]
		if len(olap) == 0 {
			paraNeighbors[para] = nil
			continue
		}
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

const GRAIN = 6.0

func granularize(z float64) float64 {
	return GRAIN * math.Round(z/GRAIN)
}

// isTextSpace returns true if `text` contains nothing but space code points.
func isTextSpace(text string) bool {
	for _, r := range text {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// combiningDiacritic returns the combining version of `text` if text contains a single uncombined
// diacritic rune.
func combiningDiacritic(text string) (string, bool) {
	runes := []rune(text)
	if len(runes) != 1 {
		return "", false
	}
	combining, isDiacritic := diacriticsToCombining[runes[0]]
	return combining, isDiacritic
}

var (
	// diacriticsToCombining is a map of diacritic runes to their combining diacritic equivalents.
	// These values were  copied from  (https://svn.apache.org/repos/asf/pdfbox/trunk/pdfbox/src/main/java/org/apache/pdfbox/text/TextPosition.java)
	diacriticsToCombining = map[rune]string{
		0x0060: "\u0300", //   ` -> ò
		0x02CB: "\u0300", //   ˋ -> ò
		0x0027: "\u0301", //   ' -> ó
		0x00B4: "\u0301", //   ´ -> ó
		0x02B9: "\u0301", //   ʹ -> ó
		0x02CA: "\u0301", //   ˊ -> ó
		0x005E: "\u0302", //   ^ -> ô
		0x02C6: "\u0302", //   ˆ -> ô
		0x007E: "\u0303", //   ~ -> õ
		0x02DC: "\u0303", //   ˜ -> õ
		0x00AF: "\u0304", //   ¯ -> ō
		0x02C9: "\u0304", //   ˉ -> ō
		0x02D8: "\u0306", //   ˘ -> ŏ
		0x02D9: "\u0307", //   ˙ -> ȯ
		0x00A8: "\u0308", //   ¨ -> ö
		0x00B0: "\u030A", //   ° -> o̊
		0x02DA: "\u030A", //   ˚ -> o̊
		0x02BA: "\u030B", //   ʺ -> ő
		0x02DD: "\u030B", //   ˝ -> ő
		0x02C7: "\u030C", //   ˇ -> ǒ
		0x02C8: "\u030D", //   ˈ -> o̍
		0x0022: "\u030E", //   " -> o̎
		0x02BB: "\u0312", //   ʻ -> o̒
		0x02BC: "\u0313", //   ʼ -> o̓
		0x0486: "\u0313", //   ҆ -> o̓
		0x055A: "\u0313", //   ՚ -> o̓
		0x02BD: "\u0314", //   ʽ -> o̔
		0x0485: "\u0314", //   ҅ -> o̔
		0x0559: "\u0314", //   ՙ -> o̔
		0x02D4: "\u031D", //   ˔ -> o̝
		0x02D5: "\u031E", //   ˕ -> o̞
		0x02D6: "\u031F", //   ˖ -> o̟
		0x02D7: "\u0320", //   ˗ -> o̠
		0x02B2: "\u0321", //   ʲ -> o̡
		0x00B8: "\u0327", //   ¸ -> o̧
		0x02CC: "\u0329", //   ˌ -> o̩
		0x02B7: "\u032B", //   ʷ -> o̫
		0x02CD: "\u0331", //   ˍ -> o̱
		0x005F: "\u0332", //   _ -> o̲
		0x204E: "\u0359", //   ⁎ -> o͙
	}
)
