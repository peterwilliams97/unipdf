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
		if !strings.Contains(cell.text(), "HONOURS:") {
			continue
		}
		corr := cp.corridorY(cell, model.PdfRectangle{Ury: 800, Urx: 600})
		if len(corr.cells) < 2 {
			continue
		}
		yCorridors = append(yCorridors, corr)
		fmt.Printf("%4d: %6.2f %s\n", i, corr.PdfRectangle, corr.cells)
		for j, c := range corr.cells {
			fmt.Printf("%8d: %s\n", j, c)
		}
	}
	return yCorridors, nil
}

// corridorY returns the longest y corridor  below `cell0`.
func (cp cellPartition) corridorY(cell0 *textPara, pageSize model.PdfRectangle) corridor {
	llx, urx := cell0.Llx, cell0.Urx
	leftCells := cp.leftOf(llx)
	rightCells := cp.rightOf(urx)
	common.Log.Info("cell0=%s", cell0)
	for i, cell := range leftCells.sorted(getUry) {
		fmt.Printf("%4d << %s\n", i, cell)
	}
	for i, cell := range rightCells.sorted(getUry) {
		fmt.Printf("%4d >> %s\n", i, cell)
	}
	y := cell0.Ury
	candidates := cp.below(y).sorted(getUry)
	// for i, cell := range candidates {
	// 	fmt.Printf("%4d ** %s\n", i, cell)
	// }
	var cells cellList
	bbox := model.PdfRectangle{Llx: DBL_MAX, Urx: DBL_MIN, Ury: y}

	for i, cell := range candidates {
		sameRow := cp.yOverlapped(cell)
		fmt.Printf("%4d ** %s\n", i, cell)
		fmt.Printf("%4s ~~ sameRow=%d %s\n", "", len(sameRow), sameRow.sorted(getUrx))
		llxE := math.Min(bbox.Llx, leftCells.intersection(sameRow).max(getUrx, pageSize.Llx))
		urxE := math.Max(bbox.Urx, rightCells.intersection(sameRow).min(getLlx, pageSize.Urx))
		llx = math.Min(llx, cell.Llx)
		urx = math.Max(urx, cell.Urx)
		fmt.Printf("%4s -- %6.2f-%6.2f %6.2f-%6.2f\n", "", llx, urx, llxE, urxE)
		if llxE > llx || urxE < urx {
			break
		}
		bbox.Llx = llxE
		bbox.Urx = urxE
		bbox.Lly = cell.Lly
		cells = append(cells, cell)
	}
	return corridor{PdfRectangle: bbox, cells: cells}
}

type corridor struct {
	model.PdfRectangle
	cells cellList
}
type cellPartition struct {
	baseOrder map[basisT]ordering
}

func (cells cellList) newCellPartition() cellPartition {
	baseOrder := map[basisT]ordering{}
	bases := []basisT{getLlx, getUrx, getLly, getUry}
	for _, basis := range bases {
		baseOrder[basis] = cells.newOrdering(basis)
	}
	return cellPartition{baseOrder: baseOrder}
}

// yOverlapped returns the cells in that overlap `cell` in the y direction.
func (cp cellPartition) yOverlapped(cell *textPara) cellSet {
	above := cp.above(cell.Lly)
	below := cp.below(cell.Ury)
	return above.intersection(below)
}

// below returns a set of cells: cell.ury <= y
func (cp cellPartition) below(y float64) cellSet {
	return cp.baseOrder[getUry].le(y)
}

// above returns a set of cells: cell.lly >= y
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

// cellsIntersection returns intersection of s1 and s2
func (s cellSet) intersection(s2 cellSet) cellSet {
	i := cellSet{}
	for cell := range s {
		if _, ok := s2[cell]; ok {
			i[cell] = true
		}
	}
	return i
}

func (s cellSet) sorted(basis basisT) cellList {
	cells := make(cellList, len(s))
	i := 0
	for cell := range s {
		cells[i] = cell
		i++
	}
	cells.sort(basis)
	return cells
}

func (cells cellList) sort(basis basisT) {
	sort.SliceStable(cells,
		func(i, j int) bool { return cells[i].at(basis) < cells[j].at(basis) })
}

func (s cellSet) min(basis basisT, defVal float64) float64 {
	z := defVal
	for cell := range s {
		z = math.Min(z, cell.at(basis))
	}
	return z
}

func (s cellSet) max(basis basisT, defVal float64) float64 {
	z := defVal
	for cell := range s {
		z = math.Max(z, cell.at(basis))
	}
	return z
}

type cellSet map[*textPara]bool
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
