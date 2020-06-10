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

func (cells cellList) findCorridorTables(pageSize model.PdfRectangle) []*textTable {
	rowCorridors, colCorridors := cells.findCorridors(pageSize)
	var candidates [][2]int
	for y, row := range rowCorridors {
		for x, col := range colCorridors {
			if row.cells[0] != col.cells[0] {
				continue
			}
			candidates = append(candidates, [2]int{x, y})
		}
	}
	cm := makeCrossingMap(rowCorridors, colCorridors)
	var tables []*textTable
	for i, cand := range candidates {
		x, y := cand[0], cand[1]
		top := rowCorridors[y]
		left := colCorridors[x]
		common.Log.Info("candidate[%d]\n\ty=%d  top=%s\n\tx=%d left=%s", i,
			y, top.String(), x, left.String())

		table := cm.isTable(y, x, top, left)
		common.Log.Info("candidate[%d]=%s", i, table)
		if table == nil {
			continue
		}
		tables = append(tables, table)
	}
	return tables
}

func (cells cellList) findCorridors(pageSize model.PdfRectangle) (corridorList, corridorList) {
	cells.sort(getLlx)
	cells.sort(getUry)
	cp := cells.newCellPartition()
	var rowCorridors, colCorridors corridorList
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

		corr := cp.corridorX(cell, pageSize)
		if len(corr.cells) >= 2 {
			rowCorridors = append(rowCorridors, corr)
			fmt.Printf("%4d: %6.2f %s\n", i, corr.PdfRectangle, corr.cells)
			for j, c := range corr.cells {
				fmt.Printf("%8d: %s\n", j, c)
			}
		}
		corr = cp.corridorY(cell, pageSize)
		if len(corr.cells) >= 2 {
			colCorridors = append(colCorridors, corr)
			fmt.Printf("%4d: %6.2f %s\n", i, corr.PdfRectangle, corr.cells)
			for j, c := range corr.cells {
				fmt.Printf("%8d: %s\n", j, c)
			}
		}
	}

	rowCorridors = rowCorridors.uniques()
	colCorridors = colCorridors.uniques()

	common.Log.Info("findCorridors:Done:rowCorridors")
	for i, corr := range rowCorridors {
		fmt.Printf("%4d: %6.2f %s\n", i, corr.PdfRectangle, corr.cells)
		for j, c := range corr.cells {
			fmt.Printf("%8d: %s\n", j, c)
		}
	}
	common.Log.Info("findCorridors:Done:colCorridors")
	for i, corr := range colCorridors {
		fmt.Printf("%4d: %6.2f %s\n", i, corr.PdfRectangle, corr.cells)
		for j, c := range corr.cells {
			fmt.Printf("%8d: %s\n", j, c)
		}
	}
	return rowCorridors, colCorridors
}

type crossingMap struct {
	model.PdfRectangle
	rowCorridors, colCorridors corridorList
	rowCrossings, colCrossings map[*textPara][]crossing
	rowIndex, colIndex         map[*textPara]int
}
type crossing struct {
	corrIdx int
	cellIdx int
}

func makeCrossingMap(rowCorridors, colCorridors corridorList) crossingMap {
	bbox := rectUnion(rowCorridors[0].PdfRectangle, colCorridors[0].PdfRectangle)
	return crossingMap{
		PdfRectangle: bbox,
		rowCorridors: rowCorridors,
		colCorridors: colCorridors,
		rowIndex:     rowCorridors.makeIndex("rows"),
		colIndex:     colCorridors.makeIndex("cols"),
		rowCrossings: rowCorridors.makeCrossings(),
		colCrossings: colCorridors.makeCrossings(),
	}
}

func (cm crossingMap) String() string {
	return fmt.Sprintf("%6.2f [%d %d]", cm.PdfRectangle, len(cm.colCorridors), len(cm.rowCorridors))
}

//  build a table with `top` as its top row and `left` as its left
// is table if
//    all row cells are in a column
//    all column cells are in a row
//    all cells in rect are in a row and a column
//    min occupancy
func (cm crossingMap) isTable(y, x int, top, left corridor) *textTable {
	if top.cells[0] != left.cells[0] {
		panic("mismatch")
	}
	cols := make(corridorList, len(top.cells))
	rows := make(corridorList, len(left.cells))
	common.Log.Notice("isTable: cols")
	for x, cell := range top.cells {
		cols[x] = cm.column(cell)
		fmt.Printf("%4d: %s\n", x, cols[x])
	}
	common.Log.Notice("isTable: rows")
	for y, cell := range left.cells {
		rows[y] = cm.row(cell)
		fmt.Printf("%4d: %s\n", y, rows[y])
	}
	colSet := rows.cellSet()
	rowSet := rows.cellSet()
	if !colSet.equals(rowSet) {
		common.Log.Notice("colSet!=rowSet\n\tcolSet=%s\n\trowSet=%s",
			colSet.String(), rowSet.String())
		return nil
	}
	for cell := range colSet {
		if !cm.encloses(cell) {
			common.Log.Notice("cm=%s doesn't enclose cell=%s", cm.String(), cell.String())
			return nil
		}
	}
	for i, col := range cols {
		if len(col.cells) < 2 {
			common.Log.Notice("column %d has %d cells", i, len(col.cells))
			return nil
		}
	}
	for i, row := range rows {
		if len(row.cells) < 2 {
			common.Log.Notice("row %d has %d cells", i, len(row.cells))
			return nil
		}
	}
	occupancy := float64(len(colSet)) / float64(len(left.cells)*len(top.cells))
	if occupancy < 0.1 {
		common.Log.Notice("occupancy=%.1f%%", 100.0*occupancy)
		return nil
	}

	return cm.makeTable(cols, rows)
}

// makeTable builds a table from `cells`.
func (cm crossingMap) makeTable(cols, rows corridorList) *textTable {
	w := len(cols)
	h := len(rows)
	cellX := map[*textPara]int{}
	for x, col := range cols {
		for _, cell := range col.cells {
			cellX[cell] = x
		}
	}
	table := newTextTable(w, h)
	for y, row := range rows {
		for _, cell := range row.cells {
			x, ok := cellX[cell]
			if !ok {
				panic(cell)
			}
			common.Log.Notice("cell %d %d = %s", x, y, cell)
			table.put(x, y, cell)
		}
	}
	return table
}

// column returns the vertical corridor below `cell`.
func (cm crossingMap) column(cell *textPara) corridor {
	idx, ok := cm.colIndex[cell]
	if !ok {
		panic(cell)
	}
	col := cm.colCorridors[idx]
	return col.within(cm.PdfRectangle)
}

func (cm crossingMap) row(cell *textPara) corridor {
	idx, ok := cm.rowIndex[cell]
	if !ok {
		panic(cell)
	}
	col := cm.rowCorridors[idx]
	return col.within(cm.PdfRectangle)
}

func (cm crossingMap) encloses(cell *textPara) bool {
	return rectContainsBounded(cm.PdfRectangle, cell)
}

type corridorList []corridor

func (corridors corridorList) uniques() corridorList {
	if len(corridors) <= 1 {
		return corridors
	}
	sort.Slice(corridors, func(i, j int) bool {
		ci, cj := corridors[i].cells, corridors[j].cells
		if len(ci) != len(cj) {
			return len(ci) > len(cj)
		}
		ri, rj := corridors[i].PdfRectangle, corridors[j].PdfRectangle
		if ri.Ury != rj.Ury {
			return ri.Ury > rj.Ury
		}
		return ri.Llx < rj.Llx
	})
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

func (corridors corridorList) cellSet() cellSet {
	cells := cellSet{}
	for _, corr := range corridors {
		for _, cell := range corr.cells {
			cells[cell] = true
		}
	}
	return cells
}

// makeIndex returns th map {cell: index in `corridors`}
func (corridors corridorList) makeIndex(title string) map[*textPara]int {
	corridorsIndex := map[*textPara]int{}
	for o, corr := range corridors {
		for _, cell := range corr.cells {
			if o2, ok := corridorsIndex[cell]; ok {
				panic(fmt.Errorf("cell is multiple %s corridors %d %d cell=%s",
					title, o2, o, cell.String()))
			}
			corridorsIndex[cell] = o
		}
	}
	var zero *textPara
	for cell, idx := range corridorsIndex {
		if idx == 0 {
			zero = cell
			break
		}
	}
	if zero == nil {
		panic(corridorsIndex)
	}
	return corridorsIndex
}

func (corridors corridorList) makeCrossings() map[*textPara][]crossing {
	cellCrossings := map[*textPara][]crossing{}
	for o, corr := range corridors {
		for e, cell := range corr.cells {
			cellCrossings[cell] = append(cellCrossings[cell], crossing{corrIdx: o, cellIdx: e})
		}
	}
	for _, crossings := range cellCrossings {
		sort.Slice(crossings, func(i, j int) bool {
			xi, xj := crossings[i], crossings[j]
			if xi.corrIdx != xj.corrIdx {
				return xi.corrIdx < xj.corrIdx
			}
			return xi.cellIdx < xj.cellIdx
		})
	}
	return cellCrossings
}

type corridor struct {
	model.PdfRectangle
	cells cellList
}

func (corr corridor) String() string {
	return fmt.Sprintf("%6.2f %s", corr.PdfRectangle, corr.cells.String())
}

// contains returns true if `other` is a subset of `corr`.
func (corr corridor) contains(other corridor) bool {
	if len(other.cells) > len(corr.cells) {
		panic("len(other.cells) > len(corr.cells)")
	}
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

// within returns the subset of `corr` bounded by `bbox`
func (corr corridor) within(bbox model.PdfRectangle) corridor {
	var cells cellList
	for _, cell := range corr.cells {
		if rectContainsBounded(bbox, cell) {
			cells = append(cells, cell)
		}
	}
	return corridor{PdfRectangle: bbox, cells: cells}
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

type cellSet map[*textPara]bool

func (set cellSet) String() string {
	return set.cellList().String()
}

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

// equals returns true if `set` and `other` are the same.
func (set cellSet) equals(other cellSet) bool {
	for cell := range set {
		if _, ok := other[cell]; !ok {
			return false
		}
	}
	for cell := range other {
		if _, ok := set[cell]; !ok {
			return false
		}
	}
	return true
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
