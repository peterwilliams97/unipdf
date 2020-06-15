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

//  {Llx: 7, Urx: 15, Lly: 4, Ury: 7} 0  2 4
var myRects = []model.PdfRectangle{
	{Llx: 0, Urx: 10, Lly: 1, Ury: 6},   // 0 x x  X
	{Llx: 4, Urx: 16, Lly: 11, Ury: 16}, // 1 x
	{Llx: 5, Urx: 15, Lly: 2, Ury: 7},   // 2 x x  X
	{Llx: 6, Urx: 14, Lly: 10, Ury: 15}, // 3 x
	{Llx: 9, Urx: 20, Lly: 0, Ury: 7},   // 4 x x  X
}

func mySubset(vals ...int) []model.PdfRectangle {
	rects := make([]model.PdfRectangle, len(vals))
	for i, v := range vals {
		rects[i] = myRects[v]
	}
	return sortedRects(rects)
}

func testRectIndex() {
	fmt.Println("testRectIndex -------------")
	for i, r := range myRects {
		fmt.Printf("%4d: %4.1f\n", i, r)
	}
	idx := makeRectIndex(myRects)
	if true {
		_leLlx := idx.le(kLlx, 5)
		_geLlx := idx.ge(kLlx, 5)
		leLlx := idx.asRects(_leLlx)
		geLlx := idx.asRects(_geLlx)
		leLlxExp := mySubset(0, 1, 2)
		geLlxExp := mySubset(2, 3, 4)
		fmt.Printf("leLlx=%d %.1f\n", len(leLlx), leLlx)
		fmt.Printf("geLlx=%d %.1f\n", len(geLlx), geLlx)

		if !sameRects(leLlx, leLlxExp) {
			panic(fmt.Errorf("leLlx\n\t got %.2f\n\t exp %.2f", leLlx, leLlxExp))
		}
		if !sameRects(geLlx, geLlxExp) {
			panic(fmt.Errorf("geLlx\n\t got %.2f\n\t exp %.2f", geLlx, geLlxExp))
		}
	}
	if true {
		_leUry := idx.le(kUry, 6)
		_geLly := idx.ge(kLly, 5)
		leUry := idx.asRects(_leUry)
		geLly := idx.asRects(_geLly)
		leUryExp := mySubset(0)
		geLlyExp := mySubset(1, 3)

		fmt.Printf("leUry=%d %+v\n", len(leUry), leUry)
		fmt.Printf("geLly=%d %+v\n", len(geLly), geLly)

		if !sameRects(leUry, leUryExp) {
			panic(fmt.Errorf("leUry\n\t got %.2f\n\t exp %.2f", leUry, leUryExp))
		}
		if !sameRects(geLly, geLlyExp) {
			panic(fmt.Errorf("geLly\n\t got %.2f\n\t exp %.2f", geLly, geLlyExp))
		}
	}
	if true {
		r := model.PdfRectangle{Llx: 7, Urx: 15, Lly: 4, Ury: 7}
		_olap := idx.overlappingRect(r)
		olap := idx.asRects(_olap)
		olapExp := mySubset(0, 2, 4)
		fmt.Printf("     r=%.1f\n", r)
		fmt.Printf("olap=%d %.1f\n", len(olap), olap)
		if !sameRects(olap, olapExp) {
			panic(fmt.Errorf("rectangle %.2f\n\t got %.2f\n\t exp %.2f", r, olap, olapExp))
		}
	}
	// panic("done")
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

	kinds := []attrKind{kLlx, kUrx, kLly, kUry}
	fmt.Printf("makeRectIndex: %s\n", kinds)
	for _, k := range kinds {
		fmt.Printf("%s %v\n", k, idx.orders[k])
	}
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
	return sortedRects(rects)
}

func (idx *rectIndex) overlappingRect(r model.PdfRectangle) set {
	fmt.Printf(" overlappingRect: r=%.1f ====================\n", r)
	o1 := idx.le(kLlx, r.Urx)
	fmt.Printf(" le(kLlx, r.Urx)=%d %.1f\n", len(o1), idx.asRects(o1))
	o2 := idx.ge(kUrx, r.Llx)
	fmt.Printf(" ge(kUrx, r.Llx)=%d %.1f\n", len(o2), idx.asRects(o2))
	o3 := idx.le(kLly, r.Ury)
	fmt.Printf(" le(kLly, r.Ury)=%d %.1f\n", len(o3), idx.asRects(o3))
	o4 := idx.ge(kUry, r.Lly)
	fmt.Printf(" ge(kUry, r.Lly)=%d %.1f\n", len(o4), idx.asRects(o4))

	xorder := o1.and(o2)
	yorder := o3.and(o4)
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
		fmt.Printf("##le %s %.1f => nil (%.1f)\n", k, z, val(0))
		return nil
	}
	if z >= val(n-1) {
		return makeSet(order)
	}

	// i is the lowest i: val(i) > z so i-1 is the greatest i: val(i) <= z
	i := sort.Search(n, func(i int) bool { return val(i) > z })
	fmt.Printf("##le %s %.1f >= %.1f => i=%d\n", k, val(i), z, i)
	if !(0 <= i) {
		panic(n)
		return nil
	}

	return makeSet(order[:i])
}

func (idx *rectIndex) ge(k attrKind, z float64) set {
	fmt.Printf(" -- ge %s %.1f\n", k, z)
	order := idx.orders[k]
	val := idx.kVal(k)
	n := len(idx.rects)
	if z <= val(0) {
		return makeSet(order)
	}
	if z > val(n-1) {
		return nil
	}
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
	sort.Slice(order, func(i, j int) bool {
		oi, oj := order[i], order[j]
		return attr(idx.rects[oi]) < attr(idx.rects[oj])
	})
	return order
}

type attribute func(model.PdfRectangle) float64

var kindAttr = map[attrKind]attribute{
	kLlx: attrLlx,
	kUrx: attrUrx,
	kLly: attrLly,
	kUry: attrUry}
var kindName = map[attrKind]string{kLlx: "Llx", kUrx: "Urx", kLly: "Lly", kUry: "Ury"}

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
	fmt.Printf("and ------------\n\t  s=%+v\n\toth=%+v\n", s, other)
	intersection := set{}
	for e := range s {
		if other[e] {
			intersection[e] = true
		}
		fmt.Printf("%4d:  %t\n", e, other[e])
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

func hasRect(rects []model.PdfRectangle, r0 model.PdfRectangle) bool {
	for _, r := range rects {
		if rectEquals(r0, r) {
			return true
		}
	}
	fmt.Printf("** r0=%.1f rects=%.1f\n", r0, rects)
	return false
}

func sameRects(rects1, rects2 []model.PdfRectangle) bool {
	for _, r := range rects1 {
		if !hasRect(rects2, r) {
			return false
		}
	}
	for _, r := range rects2 {
		if !hasRect(rects1, r) {
			return false
		}
	}
	return true
}

// rectEquals returns true if `b1` and `b2` corners are within `tol` of each other.
// NOTE: All the coordinates in this source file are in points.
func rectEquals(b1, b2 model.PdfRectangle) bool {
	return math.Abs(b1.Llx-b2.Llx) <= tol &&
		math.Abs(b1.Lly-b2.Lly) <= tol &&
		math.Abs(b1.Urx-b2.Urx) <= tol &&
		math.Abs(b1.Ury-b2.Ury) <= tol
}

const tol = 0.01

func sortedRects(rects []model.PdfRectangle) []model.PdfRectangle {
	sort.Slice(rects, func(i, j int) bool { return rects[i].Llx < rects[j].Llx })
	return rects
}
