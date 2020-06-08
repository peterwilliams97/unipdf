/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"fmt"
	"math"
	"sort"

	"github.com/unidoc/unipdf/v3/common"
	"github.com/unidoc/unipdf/v3/model"
)

// Corridors
// ---------
// N x 1 and 1 x N rectangles that contain cells and are not overlapped by any other cellls.
// These are the columns and rows in tables

// llx   urx
//  |  x  |   x    x      x     x     x
//  |     |
//  |  x  |
//  |  x  |   x
//  |  x  |
//  |  x  |   x           x
//  |  x  |
//  |  x  |

// ury --------------------------------
//     x     x    x      x     x     x
// lly ---------------------------------
//     x
//     x     x
//     x
//     x     x           x
//     x
//     x

// corridorY(cell0):
//    llx, urx := cell0.lly, cell0.urx
//    Ellx, EUrx := +∞ , -∞
//    leftCells := {cells: cell.urx <= llx}
//    rightCells :=  {cells: cell.llx >= urx}
//    y := cell0.ury
//    find candidates := {cells: cell.ury <= y sorted by cell.ury descreasing}
//    for cell1 in candidates:
//       Ellx := min(Ellx, max(cell.urx of left cells that y overlap cell1))
//       Eurx := max(Eurx, min(cell.llx of right cells that y overlap cell1))
//       llx := min(llx, cell1.llx)
//       urx := max(urx, cell1.urx)
//       if Ellx > llx or Eurx < urx: break

func (cells cellList) findCorridors() ([]corridor, []corridor) {
	cells.sort(getLlx)
	cells.sort(getUry)
	cp := cells.newCellPartition()
	var yCorridors []corridor
	common.Log.Info("findCorridors")
	for i, cell := range cells {
		// if !strings.Contains(cell.text(), "BIRTH:") {
		// 	continue
		// }
		// if !strings.Contains(cell.text(), "EDUCATION:") {
		// 	continue
		// }
		// if !strings.Contains(cell.text(), "SUMMARY CURRICULUM VITAE") {
		// 	continue
		// }

		corr := cp.corridorX(cell, model.PdfRectangle{Ury: 800, Urx: 600})
		if len(corr.cells) < 2 {
			continue
		}
		yCorridors = append(yCorridors, corr)
		fmt.Printf("%4d: %6.2f %s\n", i, corr.PdfRectangle, corr.cells)
		for j, c := range corr.cells {
			fmt.Printf("%8d: %s\n", j, c)
		}
	}
	sort.Slice(yCorridors, func(i, j int) bool {
		ci, cj := yCorridors[i].cells, yCorridors[j].cells
		return len(ci) > len(cj)
	})

	yCorridors = uniqueCorridors(yCorridors)

	common.Log.Info("findCorridors:Done:yCorridors")
	for i, corr := range yCorridors {
		fmt.Printf("%4d: %6.2f %s\n", i, corr.PdfRectangle, corr.cells)
		for j, c := range corr.cells {
			fmt.Printf("%8d: %s\n", j, c)
		}
	}
	return yCorridors, nil
}

// corridorX returns the longest x corridor to the right of `cell0`.
func (cp cellPartition) corridorX(cell0 *textPara, pageSize model.PdfRectangle) corridor {
	lly, ury := cell0.Lly, cell0.Ury
	aboveCells := cp.above(ury)
	belowCells := cp.below(lly)
	common.Log.Info("cell0=%s", cell0)
	for i, cell := range aboveCells.sorted(getLlx) {
		fmt.Printf("%4d << %s\n", i, cell)
	}
	for i, cell := range belowCells.sorted(getLlx) {
		fmt.Printf("%4d >> %s\n", i, cell)
	}
	x := cell0.Llx
	// candidates := cp.below(y).sorted(getLlx).reversed().sorted(getUry).reversed()
	candidates := cp.rightOf(x).tableSorted()

	var cells cellList
	bbox := model.PdfRectangle{
		Lly: pageSize.Lly,
		Ury: pageSize.Ury,
		Llx: x}

	for i, cell := range candidates {
		sameColumn := cp.xOverlapped(cell)
		corrCells := sameColumn.subtract(aboveCells).subtract(belowCells)
		if len(corrCells) == 0 {
			continue
		}
		if _, ok := corrCells[cell]; !ok {
			continue
		}

		immediateAbove := sameColumn.intersect(aboveCells)
		immediateBelow := sameColumn.intersect(belowCells)
		llyE := immediateBelow.max(getUry, bbox.Lly)
		uryE := immediateAbove.min(getLly, bbox.Ury)
		lly = math.Min(lly, cell.Lly)
		ury = math.Max(ury, cell.Ury)
		fmt.Printf("%4d ** %d-%d-%d=%d %s\n", i,
			len(sameColumn), len(aboveCells), len(belowCells), len(corrCells), cell)
		fmt.Printf("%4s ~~   sameRow=%d %s\n", "", len(sameColumn), sameColumn.sorted(getLlx))
		fmt.Printf("%4s ~~ corrCells=%d %s\n", "", len(corrCells), corrCells.sorted(getLlx))
		fmt.Printf("%4s -- inner=%6.2f-%6.2f outer=%6.2f-%6.2f\n", "", lly, ury, llyE, uryE)
		if !(llyE <= lly && ury <= uryE) {
			break
		}
		bbox.Lly = llyE
		bbox.Ury = uryE
		bbox.Urx = cell.Urx
		cells = append(cells, cell)
		belowCells = cp.below(lly)
		aboveCells = cp.above(ury)
		fmt.Printf("%4s -- cells=%d %s\n", "", len(cells), cells)
	}
	return corridor{PdfRectangle: bbox, cells: cells}
}

// corridorY returns the longest y corridor  below `cell0`.
func (cp cellPartition) corridorY(cell0 *textPara, pageSize model.PdfRectangle) corridor {
	llx, urx := cell0.Llx, cell0.Urx
	leftCells := cp.leftOf(llx)
	rightCells := cp.rightOf(urx)
	common.Log.Info("cell0=%s", cell0)
	for i, cell := range leftCells.sorted(getUry).reversed() {
		fmt.Printf("%4d << %s\n", i, cell)
	}
	for i, cell := range rightCells.sorted(getUry).reversed() {
		fmt.Printf("%4d >> %s\n", i, cell)
	}
	y := cell0.Ury
	// candidates := cp.below(y).sorted(getLlx).reversed().sorted(getUry).reversed()
	candidates := cp.below(y).tableSorted()

	var cells cellList
	bbox := model.PdfRectangle{
		Llx: pageSize.Llx,
		Urx: pageSize.Urx,
		Ury: y}

	for i, cell := range candidates {
		sameRow := cp.yOverlapped(cell)
		corrCells := sameRow.subtract(leftCells).subtract(rightCells)
		if len(corrCells) == 0 {
			continue
		}
		if _, ok := corrCells[cell]; !ok {
			continue
		}

		immediateLeft := sameRow.intersect(leftCells)
		immediateRight := sameRow.intersect(rightCells)
		llxE := immediateLeft.max(getUrx, bbox.Llx)
		urxE := immediateRight.min(getLlx, bbox.Urx)
		llx = math.Min(llx, cell.Llx)
		urx = math.Max(urx, cell.Urx)
		fmt.Printf("%4d ** %d-%d-%d=%d %s\n", i,
			len(sameRow), len(leftCells), len(rightCells), len(corrCells), cell)
		fmt.Printf("%4s ~~   sameRow=%d %s\n", "", len(sameRow), sameRow.sorted(getUrx))
		fmt.Printf("%4s ~~ corrCells=%d %s\n", "", len(corrCells), corrCells.sorted(getUrx))
		fmt.Printf("%4s -- inner=%6.2f-%6.2f outer=%6.2f-%6.2f\n", "", llx, urx, llxE, urxE)
		if !(llxE <= llx && urx <= urxE) {
			break
		}
		bbox.Llx = llxE
		bbox.Urx = urxE
		bbox.Lly = cell.Lly
		cells = append(cells, cell)
		leftCells = cp.leftOf(llx)
		rightCells = cp.rightOf(urx)
		fmt.Printf("%4s -- cells=%d %s\n", "", len(cells), cells)
	}
	return corridor{PdfRectangle: bbox, cells: cells}
}

type corridor struct {
	model.PdfRectangle
	cells cellList
}
type cellPartition struct {
	baseOrder map[basisT]ordering
	allCells  cellSet
}

func (cells cellList) newCellPartition() cellPartition {
	baseOrder := map[basisT]ordering{}
	bases := []basisT{getLlx, getUrx, getLly, getUry}
	for _, basis := range bases {
		baseOrder[basis] = cells.newOrdering(basis)
	}
	return cellPartition{baseOrder: baseOrder, allCells: cells.set()}
}

// xOverlapped returns the cells in that overlap `cell` in the x direction.
func (cp cellPartition) xOverlapped(cell *textPara) cellSet {
	leftOrEqual := cp.baseOrder[getLlx].le(cell.Urx)
	rightOrEqual := cp.baseOrder[getUrx].ge(cell.Llx)
	return leftOrEqual.intersect(rightOrEqual)
}

// yOverlapped returns the cells in that overlap `cell` in the y direction.
func (cp cellPartition) yOverlapped(cell *textPara) cellSet {
	aboveOrEqual := cp.baseOrder[getUry].ge(cell.Lly)
	belowOrEqual := cp.baseOrder[getLly].le(cell.Ury)
	return aboveOrEqual.intersect(belowOrEqual)
}

// below returns a set of cells: cell.ury <= y
func (cp cellPartition) below(y float64) cellSet {
	return cp.baseOrder[getUry].le(y)
}

// above returns a set of cells: cell.Lly >= y
func (cp cellPartition) above(y float64) cellSet {
	return cp.baseOrder[getLly].ge(y)
}

// leftOf returns a set of cells: cell.urx <= x
func (cp cellPartition) leftOf(x float64) cellSet {
	return cp.baseOrder[getUrx].le(x)
}

// rightOf returns a set of cells: cell.llx >= x
func (cp cellPartition) rightOf(x float64) cellSet {
	return cp.baseOrder[getLlx].ge(x)
}

type ordering struct {
	posCells map[float64]cellList
	forward  []float64
	reverse  []float64
}

func (cells cellList) newOrdering(basis basisT) ordering {
	posCells := map[float64]cellList{}
	for _, cell := range cells {
		z := cell.at(basis)
		posCells[z] = append(posCells[z], cell)
	}
	n := len(posCells)
	forward := make([]float64, n)
	i := 0
	for z := range posCells {
		forward[i] = z
		i++
	}
	sort.Float64s(forward)
	reverse := make([]float64, n)
	for i, z := range forward {
		reverse[n-1-i] = z
	}
	return ordering{posCells: posCells, forward: forward, reverse: reverse}
}

func (o ordering) le(z float64) cellSet {
	cells := cellSet{}
	for _, pos := range o.forward {
		if pos > z {
			break
		}
		for _, cell := range o.posCells[pos] {
			cells[cell] = true
		}
	}
	return cells
}

func (o ordering) ge(z float64) cellSet {
	cells := cellSet{}
	for _, pos := range o.reverse {
		if pos < z {
			break
		}
		for _, cell := range o.posCells[pos] {
			cells[cell] = true
		}
	}
	return cells
}

func uniqueCorridors(corridors []corridor) []corridor {
	if len(corridors) <= 1 {
		return corridors
	}
	uniques := []corridor{corridors[0]}
	for _, corr := range corridors[1:] {
		duplicate := false
		for _, u := range uniques {
			if u.contains(corr) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			uniques = append(uniques, corr)
		}
	}
	return uniques
}

func (corr corridor) contains(other corridor) bool {
	for i, cell := range corr.cells[:len(corr.cells)-len(other.cells)+1] {
		if other.cells[0] != cell {
			continue
		}
		for j, o := range other.cells {
			if o != corr.cells[i+j] {
				return false
			}
		}
		return true
	}
	return false
}

type cellSet map[*textPara]bool

// subtract returns the elements of `set` not in `other`.
func (set cellSet) subtract(other cellSet) cellSet {
	out := cellSet{}
	for cell := range set {
		if _, ok := other[cell]; !ok {
			out[cell] = true
		}
	}
	return out
}

// intersect returns the intersection of `set` and `other`.
func (set cellSet) intersect(other cellSet) cellSet {
	out := cellSet{}
	for cell := range set {
		if _, ok := other[cell]; ok {
			out[cell] = true
		}
	}
	return out
}

// cellList returns `set` as cellList.
func (set cellSet) cellList() cellList {
	cells := make(cellList, len(set))
	i := 0
	for cell := range set {
		cells[i] = cell
		i++
	}
	return cells
}

// // tableSorted returns set sorted by `basis`
func (set cellSet) sorted(basis basisT) cellList {
	return set.cellList().sorted(basis)
}

// tableSorted returns set sorted for table discovery.
func (set cellSet) tableSorted() cellList {
	cells := set.cellList()
	sort.Slice(cells,
		func(i, j int) bool {
			ci, cj := cells[i], cells[j]
			if ci.Ury != cj.Ury {
				return ci.Ury > cj.Ury
			}
			return ci.Llx < cj.Llx
		})
	return cells
}

func (set cellSet) tableSortedX() cellList {
	cells := set.cellList()
	sort.Slice(cells,
		func(i, j int) bool {
			ci, cj := cells[i], cells[j]
			if ci.Llx != cj.Llx {
				return ci.Llx < cj.Llx
			}
			return ci.Ury > cj.Ury
		})
	return cells
}

// min returns the smaller of `defVal` and the minimum value of `set` at `basis`.
func (set cellSet) min(basis basisT, defVal float64) float64 {
	z := defVal
	for cell := range set {
		z = math.Min(z, cell.at(basis))
	}
	return z
}

// max returns the larger of `defVal` and the maximum value of `set` at `basis`.
func (set cellSet) max(basis basisT, defVal float64) float64 {
	z := defVal
	for cell := range set {
		z = math.Max(z, cell.at(basis))
	}
	return z
}

func (cells cellList) sorted(basis basisT) cellList {
	dup := make(cellList, len(cells))
	copy(dup, cells)
	dup.sort(basis)
	return dup
}

func (cells cellList) sort(basis basisT) {
	sort.SliceStable(cells, func(i, j int) bool { return cells[i].at(basis) < cells[j].at(basis) })
}

func (cells cellList) reversed() cellList {
	n := len(cells)
	rev := make(cellList, n)
	for i, cell := range cells {
		rev[n-1-i] = cell
	}
	return rev
}

func (cells cellList) set() cellSet {
	set := make(cellSet, len(cells))
	for _, cell := range cells {
		set[cell] = true
	}
	return set
}
