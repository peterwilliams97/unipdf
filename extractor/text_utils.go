/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/unidoc/unipdf/v3/common"
)

// serial is used to add serial numbers to all text* instances.
var serial serialState

// serialState keeps serial number for text* structs.
type serialState struct {
	mark   int // textMark
	word   int // textWord
	strata int // textStrata
	line   int // textLine
	para   int // textPara
}

// reset resets `serial` to all zeros.
func (serial *serialState) reset() {
	var empty serialState
	*serial = empty
}

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

// fileLine printed out a file:line string for the caller `skip` levels up the call stack.
func fileLine(skip int, doSecond bool) string {
	_, file, line, ok := runtime.Caller(skip + 1)
	if !ok {
		file = "???"
		line = 0
	} else {
		file = filepath.Base(file)
	}
	depth := fmt.Sprintf("%s:%-4d", file, line)
	if !doSecond {
		return depth
	}
	_, _, line2, _ := runtime.Caller(skip + 2)
	return fmt.Sprintf("%s:%-4d", depth, line2)
}

func (paras paraList) addNeighbours() {
	s := func(p *textPara) string {
		if p == nil {
			return ""
		}
		return fmt.Sprintf("%s {%.1f}", truncate(p.text(), 20), p.PdfRectangle)
	}

	paraNeighbours := paras.yNeighbours()
	for _, para := range paras {
		// parts := make([]string, len(paraNeighbours[para]))
		// for i, k := range paraNeighbours[para] {
		// 	parts[i] = truncate(paras[k].text(), 10)
		// }
		var right *textPara
		dup := false
		for _, k := range paraNeighbours[para] {
			b := paras[k]
			if b.Llx >= para.Urx {
				if right == nil {
					right = b
				} else {
					if b.Llx < right.Llx {
						right = b
						dup = false
					} else if b.Llx == right.Llx {
						dup = true
					}
				}
			}
		}
		if verboseTable2 && right != nil {
			common.Log.Info("%30s -> %s %t", s(para), s(right), dup)
		}
		if !dup {
			para.right = right
		}
		if right != nil && right.Llx < para.Urx {
			panic("wrogn")
		}
	}
	// panic("DANE")

	paraNeighbours = paras.xNeighbours()
	for _, para := range paras {
		var below *textPara
		dup := false
		// parts := make([]string, len(paraNeighbours[para]))
		// for i, k := range paraNeighbours[para] {
		// 	parts[i] = truncate(paras[k].text(), 10)
		// }
		for _, i := range paraNeighbours[para] {
			b := paras[i]
			if b.Ury <= para.Lly {
				if below == nil {
					below = b
				} else {
					if b.Ury > below.Ury {
						below = b
						dup = false
					} else if b.Ury == below.Ury {
						dup = true
					}
				}
			}
		}
		if verboseTable2 && below != nil {
			common.Log.Info("%30s -> %s %t", s(para), s(below), dup)
		}

		if !dup {
			para.below = below
		}
	}
	// panic("DUNE")
}

// xNeighbours returns a map {para: indexes of paras that x-overlap para}.
func (paras paraList) xNeighbours() map[*textPara][]int {
	events := make([]event, 2*len(paras))
	for i, para := range paras {
		events[2*i] = event{para.Llx, true, i}
		events[2*i+1] = event{para.Urx, false, i}
	}
	return paras.eventNeighbours(events)
}

// yNeighbours returns a map {para: indexes of paras that y-overlap para}.
func (paras paraList) yNeighbours() map[*textPara][]int {
	events := make([]event, 2*len(paras))
	for i, para := range paras {
		events[2*i] = event{para.Lly, true, i}
		events[2*i+1] = event{para.Ury, false, i}
	}
	return paras.eventNeighbours(events)
}

type event struct {
	z     float64
	enter bool
	i     int
}

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

	overlaps := map[int]map[int]bool{}
	olap := map[int]bool{}
	for k, e := range events {
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
		if verboseTable2 {
			common.Log.Info("%4d: {%7.3f %5t %2d} %2d %2d",
				k, e.z, e.enter, e.i, len(olap), len(overlaps[e.i]))
		}
	}
	// panic("DINE")
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
