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

	"github.com/unidoc/unipdf/v3/model"
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

var myRects = []model.PdfRectangle{
	{Llx: 0, Urx: 10, Lly: 1, Ury: 6},
	{Llx: 4, Urx: 15, Lly: 11, Ury: 16},
	{Llx: 5, Urx: 15, Lly: 2, Ury: 7},
	{Llx: 6, Urx: 15, Lly: 10, Ury: 15},
	{Llx: 10, Urx: 20, Lly: 0, Ury: 5},
}

func testRectIndex() {
	fmt.Println("testRectIndex -------------")
	idx := makeRectIndex(myRects)
	leLlx := idx.le(kLlx, 5)
	geLlx := idx.ge(kLlx, 5)

	fmt.Printf("leLlx=%d %.1f\n", len(leLlx), idx.asRects(leLlx))
	fmt.Printf("geLlx=%d %.1f\n", len(geLlx), idx.asRects(geLlx))

	leUry := idx.le(kUry, 5)
	geLly := idx.ge(kLly, 5)

	fmt.Printf("leUry=%d %+v\n", len(leUry), idx.asRects(leUry))
	fmt.Printf("geLly=%d %+v\n", len(geLly), idx.asRects(geLly))

	r := model.PdfRectangle{Llx: 1, Urx: 11, Lly: 4, Ury: 6}
	olap := idx.overlappingRect(r)
	fmt.Printf("olap=%d %.1f\n", len(olap), idx.asRects(olap))
	panic("done")
}
func init() {
	testRectIndex()
}

type rectIndex struct {
	rects      []model.PdfRectangle
	pageSize   model.PdfRectangle // Bounding box (union of words' in bins bounding boxes).
	pageHeight float64
	fontsize   float64
	orders     map[attrKind][]int
}

func makeRectIndex(rects []model.PdfRectangle) *rectIndex {
	idx := &rectIndex{rects: rects, orders: map[attrKind][]int{}}
	idx.build()
	return idx
}

func (idx *rectIndex) build() {
	for k, attr := range kindAttr {
		idx.orders[k] = idx.makeOrdering(attr)
	}
}

func (idx *rectIndex) asRects(s set) []model.PdfRectangle {
	var rects []model.PdfRectangle
	for e := range s {
		rects = append(rects, idx.rects[e])
	}
	return rects
}

func (idx *rectIndex) overlappingRect(r model.PdfRectangle) set {
	xorder := idx.le(kLlx, r.Urx).and(idx.ge(kUrx, r.Llx))
	yorder := idx.le(kLly, r.Ury).and(idx.ge(kUry, r.Lly))
	fmt.Printf(" -- xorder=%d %.1f\n", len(xorder), idx.asRects(xorder))
	fmt.Printf(" -- yorder=%d %.1f\n", len(yorder), idx.asRects(yorder))
	return xorder.and(yorder)
}

// overlappingAttr returns the indexes in idx.rects of the rectangles that overlap [`z0`..`z1`) for
// attribute `k`.
func (idx *rectIndex) overlappingAttr(k attrKind, z0, z1 float64) set {
	attr := kindAttr[k]
	order := idx.orders[k]
	val := func(i int) float64 { return attr(idx.rects[order[i]]) }
	n := len(order)
	i0 := sort.Search(n, func(i int) bool { return val(i) >= z0 })
	i1 := sort.Search(n, func(i int) bool { return val(i) > z1 })
	if !(0 <= i0 && i1 < n) {
		return nil
	}
	return makeSet(order[i0:i1])
}

func (idx *rectIndex) le(k attrKind, z float64) set {
	fmt.Printf(" -- le %s %.1f\n", k, z)
	order := idx.orders[k]
	val := idx.kVal(k)
	n := len(idx.rects)
	if z < val(0) {
		return nil
	}
	if z >= val(n-1) {
		return makeSet(order)
	}

	i := sort.Search(n, func(i int) bool { return val(i) >= z })
	if !(0 <= i) {
		panic(n)
		return nil
	}
	return makeSet(order[:i+1])
}

func (idx *rectIndex) ge(k attrKind, z float64) set {
	fmt.Printf(" -- ge %s %.1f\n", k, z)
	order := idx.orders[k]
	val := idx.kVal(k)
	n := len(idx.rects)
	i := sort.Search(n, func(i int) bool { return val(i) >= z })
	if !(0 <= i && i < n) {
		panic(z)
		return nil
	}
	return makeSet(order[i:])
}

func (idx *rectIndex) kVal(k attrKind) func(int) float64 {
	attr := kindAttr[k]
	order := idx.orders[k]
	return func(i int) float64 { return attr(idx.rects[order[i]]) }
}

// index is an ordering over i.rects by `attrib`
func (idx *rectIndex) makeOrdering(attr attribute) []int {
	order := make([]int, len(idx.rects))
	for i := range idx.rects {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return attr(idx.rects[i]) < attr(idx.rects[j]) })
	return order
}

type attribute func(model.PdfRectangle) float64

var kindAttr = map[attrKind]attribute{kLlx: attrLlx, kUrx: attrUrx, kLly: attrLly, kUry: attrUry}
var kindName = map[attrKind]string{kLlx: "attrLlx", kUrx: "attrUrx", kLly: "attrLly", kUry: "attrUry"}

func attrLlx(r model.PdfRectangle) float64 { return r.Llx }
func attrUrx(r model.PdfRectangle) float64 { return r.Urx }
func attrLly(r model.PdfRectangle) float64 { return r.Lly }
func attrUry(r model.PdfRectangle) float64 { return r.Ury }

type attrKind int

func (k attrKind) String() string { return kindName[k] }

const (
	kLlx attrKind = iota
	kUrx
	kLly
	kUry
)

type set map[int]bool

func (s set) has(e int) bool {
	return s[e]
}
func (s set) and(other set) set {
	intersection := set{}
	for e := range s {
		if other[e] {
			intersection[e] = true
		}
	}
	return intersection
}

func makeSet(order []int) set {
	s := set{}
	for _, e := range order {
		s[e] = true
	}
	return s
}
