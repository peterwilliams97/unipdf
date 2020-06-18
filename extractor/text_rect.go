/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"fmt"
	"math"
	"sort"

	"github.com/RoaringBitmap/roaring"
	"github.com/unidoc/unipdf/v3/common"
	"github.com/unidoc/unipdf/v3/model"
)

// func init() {
// 	testRectIndex()
// }

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
		fmt.Printf("leLlx=%d %v %.1f\n", len(leLlx), _leLlx.ToArray(), leLlx)
		fmt.Printf("geLlx=%d %v %.1f\n", len(geLlx), _geLlx.ToArray(), geLlx)

		if !sameRects(leLlx, leLlxExp) {
			panic(fmt.Errorf("leLlx\n\t got %.2f\n\t exp %.2f", leLlx, leLlxExp))
		}
		if !sameRects(geLlx, geLlxExp) {
			panic(fmt.Errorf("geLlx\n\t got %.2f\n\t exp %.2f", geLlx, geLlxExp))
		}
	}
	if false {
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
	if false {
		r := tr(7, 15, 4, 7)
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

//  {Llx: 7, Urx: 15, Lly: 4, Ury: 7} 0  2 4
var myRects = []textRect{
	tr(0, 10, 1, 6),   // 0 x x  X
	tr(4, 16, 11, 16), // 1 x
	tr(5, 15, 2, 7),   // 2 x x  X
	tr(6, 14, 10, 15), // 3 x
	tr(9, 20, 0, 7),   // 4 x x  X
}

func mySubset(vals ...int) []textRect {
	rects := make([]textRect, len(vals))
	for i, v := range vals {
		rects[i] = myRects[v]
	}
	rects = sortedRects(rects)
	fmt.Printf("mySubset(%v)->%v\n", vals, rects)
	return rects
}

type textRect struct {
	model.PdfRectangle
	depth    float64
	fontsize float64
}

func tr(llx, urx, lly, ury float64) textRect {
	r := model.PdfRectangle{Llx: llx, Urx: urx, Lly: lly, Ury: ury}
	return textRect{PdfRectangle: r}
}

type rectIndex struct {
	rects      []textRect
	pageSize   model.PdfRectangle // Bounding box (union of words' in bins bounding boxes).
	pageHeight float64
	fontsize   float64
	orders     map[attrKind][]uint32
}

// func makeBoundedIndex(boundedList []bounded) *rectIndex {
// 	rects := make([]textRect, len(boundedList))
// 	for i, b := range boundedList {
// 		rects[i] = textRect{PdfRectangle: b.bbox()}
// 	}
// 	return makeRectIndex(rects)
// }

func makeRectIndex(rects []textRect) *rectIndex {
	idx := &rectIndex{rects: rects, orders: map[attrKind][]uint32{}}
	idx.build()

	kinds := []attrKind{kLlx, kUrx, kLly, kUry, kDepth}
	common.Log.Info("makeRectIndex: %s\n", kinds)
	// for _, k := range kinds {
	// 	fmt.Printf("%s %v\n", k, idx.orders[k])
	// }
	return idx
}

func (idx *rectIndex) build() {
	for k, attr := range kindAttr {
		idx.orders[k] = idx.makeOrdering(attr)
	}
}

// index is an ordering over i.rects by `attrib`
func (idx *rectIndex) makeOrdering(attr attribute) []uint32 {
	order := make([]uint32, len(idx.rects))
	for i := range idx.rects {
		order[i] = uint32(i)
	}
	sort.Slice(order, func(i, j int) bool {
		oi, oj := order[i], order[j]
		return attr(idx.rects[oi]) < attr(idx.rects[oj])
	})
	return order
}

func (idx *rectIndex) asRects(s *roaring.Bitmap) []textRect {
	var rects []textRect
	for _, e := range s.ToArray() {
		rects = append(rects, idx.rects[e])
	}
	return sortedRects(rects)
}

func (idx *rectIndex) overlappingRect(r textRect) *roaring.Bitmap {
	show := func(title string, o *roaring.Bitmap) {
		fmt.Printf("  %s=%d %.1f\n", title, o.GetCardinality(), idx.asRects(o))
	}
	fmt.Printf(" overlappingRect: r=%.1f ====================\n", r)
	o1 := idx.le(kLlx, r.Urx)
	o2 := idx.ge(kUrx, r.Llx)
	o3 := idx.le(kLly, r.Ury)
	o4 := idx.ge(kUry, r.Lly)
	show("le(kLlx, r.Urx)", o1)
	show("ge(kUrx, r.Llx)", o2)
	show("le(kLly, r.Ury)", o3)
	show("ge(kUry, r.Lly)", o4)

	xorder := o1
	xorder.And(o2)
	yorder := o3
	yorder.And(o4)
	show(" -- xorder", xorder)
	show(" -- yorder", yorder)
	xorder.And(yorder)
	return xorder
}

type rectQuery struct {
	attr attrKind
	comp compKind
	val  float64
	val2 float64
}

type compKind int

const (
	compLe compKind = iota
	compGe
	compLeGe
)

func (idx *rectIndex) filter(conditions []rectQuery, elements *roaring.Bitmap) {
	for _, q := range conditions {
		idx.filterOne(q, elements)
	}
}

func (idx *rectIndex) filterOne(q rectQuery, elements *roaring.Bitmap) {
	switch q.comp {
	case compLe:
		idx.filterLE(q.attr, q.val, elements)
	case compGe:
		idx.filterGE(q.attr, q.val, elements)
	case compLeGe:
		idx.filterLEGE(q.attr, q.val, q.val2, elements)
	default:
		panic(fmt.Errorf("comp not implemented q=%+v", q))
	}

}

// overlappingAttr returns the indexes in idx.rects of the rectangles that overlap [`z0`..`z1`) for
// attribute `k`.
func (idx *rectIndex) overlappingAttr(k attrKind, z0, z1 float64) *roaring.Bitmap {
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

func (idx *rectIndex) le(k attrKind, z float64) *roaring.Bitmap {
	// fmt.Printf(" -- le %s %.1f\n", k, z)
	order := idx.orders[k]
	val := idx.kVal(k)
	n := len(idx.rects)
	if z < val(0) {
		// fmt.Printf("##le %s %.1f => nil (%.1f)\n", k, z, val(0))
		return roaring.New()
	}
	if z >= val(n-1) {
		return makeSet(order)
	}

	// i is the lowest i: val(i) > z so i-1 is the greatest i: val(i) <= z
	i := sort.Search(n, func(i int) bool { return val(i) > z })
	// fmt.Printf("##le %s %.1f >= %.1f => i=%d\n", k, val(i), z, i)
	if !(0 <= i) {
		panic(n)
		return nil
	}
	return makeSet(order[:i])
}

func (idx *rectIndex) filterLE(k attrKind, z float64, elements *roaring.Bitmap) {
	filter := idx.le(k, z)
	elements.And(filter)
}

// func (idx *rectIndex) ile(k attrKind, z float64) int {
// 	fmt.Printf(" -- ile %s %.1f\n", k, z)
// 	val := idx.kVal(k)
// 	n := len(idx.rects)
// 	if z < val(0) {
// 		return 0
// 	}
// 	if z >= val(n-1) {
// 		return n
// 	}
// 	// i is the lowest i: val(i) > z so i-1 is the greatest i: val(i) <= z
// 	i := sort.Search(n, func(i int) bool { return val(i) > z })
// 	return i
// }

func (idx *rectIndex) ge(k attrKind, z float64) *roaring.Bitmap {
	// fmt.Printf(" -- ge %s %.1f\n", k, z)
	order := idx.orders[k]
	val := idx.kVal(k)
	n := len(idx.rects)
	if z <= val(0) {
		return makeSet(order)
	}
	if z > val(n-1) {
		return roaring.New()
	}
	i := sort.Search(n, func(i int) bool { return val(i) >= z })
	if !(0 <= i && i < n) {
		panic(z)
		return nil
	}
	return makeSet(order[i:])
}

func (idx *rectIndex) filterGE(k attrKind, z float64, elements *roaring.Bitmap) {
	if elements == nil {
		panic("no elements")
	}
	filter := idx.ge(k, z)
	elements.And(filter)
}

func (idx *rectIndex) filterLEGE(k attrKind, lo, hi float64, elements *roaring.Bitmap) {
	// fmt.Printf(" -- le %s %.1f\n", k, z)
	order := idx.orders[k]
	val := idx.kVal(k)
	n := len(idx.rects)
	if hi < val(0) {
		// fmt.Printf("##le %s %.1f => nil (%.1f)\n", k, z, val(0))
		common.Log.Error("%.2f < %.2f", hi, val(0))
	}
	if lo > val(n-1) {
		common.Log.Error("%.2f > %.2f", lo, val(n-1))

	}

	// i0 is the lowest i: val(i) > z so i-1 is the greatest i: val(i) <= z
	i0 := sort.Search(n, func(i int) bool { return val(i) >= lo })
	// fmt.Printf("##le %s %.1f >= %.1f => i=%d\n", k, val(i), z, i)
	if !(0 <= i0) {
		panic(n)
	}

	// i1 is the lowest i: val(i) > z so i-1 is the greatest i: val(i) <= z
	i1 := sort.Search(n, func(i int) bool { return val(i) > hi })
	// fmt.Printf("##le %s %.1f >= %.1f => i=%d\n", k, val(i), z, i)
	if !(0 <= i1) {
		panic(n)
	}
	filter := makeSet(order[i0:i1])
	elements.And(filter)
}

func (idx *rectIndex) kVal(k attrKind) func(int) float64 {
	attr := kindAttr[k]
	order := idx.orders[k]
	return func(i int) float64 { return attr(idx.rects[order[i]]) }
}

type attribute func(textRect) float64

var kindAttr = map[attrKind]attribute{
	kLlx:   attrLlx,
	kUrx:   attrUrx,
	kLly:   attrLly,
	kUry:   attrUry,
	kDepth: attrDepth,
}
var kindName = map[attrKind]string{kLlx: "Llx", kUrx: "Urx", kLly: "Lly", kUry: "Ury", kDepth: "depth"}

func attrLlx(r textRect) float64   { return r.Llx }
func attrUrx(r textRect) float64   { return r.Urx }
func attrLly(r textRect) float64   { return r.Lly }
func attrUry(r textRect) float64   { return r.Ury }
func attrDepth(r textRect) float64 { return r.depth }

type attrKind int

func (k attrKind) String() string { return kindName[k] }

const (
	kLlx attrKind = iota
	kUrx
	kLly
	kUry
	kDepth
	kReading
)

var makeSetCaller = map[string]int{}

func makeSet(order []uint32) *roaring.Bitmap {
	// caller := fileLine(1, false)
	// makeSetCaller[caller]++
	return roaring.BitmapOf(order...)
}

func hasRect(rects []textRect, r0 textRect) bool {
	for _, r := range rects {
		if rectEquals(r0, r) {
			return true
		}
	}
	fmt.Printf("** r0=%.1f rects=%.1f\n", r0, rects)
	return false
}

func sameRects(rects1, rects2 []textRect) bool {
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
func rectEquals(b1, b2 textRect) bool {
	return math.Abs(b1.Llx-b2.Llx) <= tol &&
		math.Abs(b1.Lly-b2.Lly) <= tol &&
		math.Abs(b1.Urx-b2.Urx) <= tol &&
		math.Abs(b1.Ury-b2.Ury) <= tol
}

const tol = 0.01

func sortedRects(rects []textRect) []textRect {
	sort.Slice(rects, func(i, j int) bool { return rects[i].Llx < rects[j].Llx })
	return rects
}
