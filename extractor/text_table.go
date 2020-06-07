/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/unidoc/unipdf/v3/common"
	"github.com/unidoc/unipdf/v3/model"
)

type textTable struct {
	model.PdfRectangle
	w, h  int
	cells cellMap
}

func newTextTable(w, h int) *textTable {
	return &textTable{w: w, h: h, cells: cellMap{}}
}

func (t *textTable) String() string {
	return fmt.Sprintf("[%dx%d] %6.2f", t.w, t.h, t.PdfRectangle)
}

func (t *textTable) bbox() model.PdfRectangle {
	rect := model.PdfRectangle{Urx: -1, Ury: -1}
	for _, cell := range t.cells {
		if rect.Urx < rect.Llx {
			rect = cell.PdfRectangle
		} else {
			rect = rectUnion(rect, cell.PdfRectangle)
		}
	}
	return rect
}

func (t *textTable) get(x, y int) *textPara {
	t.validateArgs(x, y)
	return t.cells[cellIndex{x, y}]
}
func (t *textTable) put(x, y int, cell *textPara) {
	t.validateArgs(x, y)
	t.cells[cellIndex{x, y}] = cell
}
func (t *textTable) del(x, y int) {
	t.validateArgs(x, y)
	delete(t.cells, cellIndex{x, y})
}

func (t *textTable) validateArgs(x, y int) {
	if !(0 <= x && x < t.w) {
		panic(fmt.Errorf("bad x=%d t=%s", x, t))
	}
	if !(0 <= y && y < t.h) {
		panic(fmt.Errorf("bad y=%d t=%s", y, t))
	}
}

func (t *textTable) validate() {
	if !t.isValid() {
		panic("duplicagte")
	}
}

func (t *textTable) isValid() bool {
	var cells cellList
	for _, cell := range t.cells {
		cells = append(cells, cell)
	}
	n := len(cells)
	for i := 0; i < n; i++ {
		for j := 1 + 1; j < n; j++ {
			if cells[i].serial == cells[j].serial {
				panic("ñœ")
				return false
			}
		}
	}
	return true
}

// fontsize for a table is the minimum font size of the cells.
func (t *textTable) fontsize() float64 {
	size := -1.0
	for _, p := range t.cells {
		if p != nil {
			if size < 0 {
				size = p.fontsize()
			} else {
				size = math.Min(size, p.fontsize())
			}
		}
	}
	return size
}

func (t *textTable) expand(w, h int) {
	if w < t.w {
		panic(w)
	}
	if h < t.h {
		panic(h)
	}
	t.w = w
	t.h = h
}

// !@#$%
// w := combo.w
// 		h := combo.h + t2.h - 1
// 		common.Log.Info("COMBINE! %dx%d i1=%d i2=%d", w, h, i1, i2)
// 		combined := make(cellList, w*h)
// 		for y := 0; y < t1.h; y++ {
// 			for x := 0; x < w; x++ {
// 				combined[y*w+x] = combo.cells[y*w+x]
// 			}
// 		}
// 		for y := 1; y < t2.h; y++ {
// 			yy := y + combo.h - 1
// 			for x := 0; x < w; x++ {
// 				combined[yy*w+x] = t2.cells[y*w+x]
// 			}
// 		}
// 		combo.cells = combined

type cellIndex struct{ x, y int }

type cellMap map[cellIndex]*textPara
type cellList paraList

func (cells cellList) String() string {
	return fmt.Sprintf("%6.2f %d %q", cells.bbox(), len(cells), cells.asStrings())
}

// bbox returns the union of the bounds of `cells`.
func (cells cellList) bbox() model.PdfRectangle {
	rect := cells[0].PdfRectangle
	for _, r := range cells[1:] {
		rect = rectUnion(rect, r.PdfRectangle)
	}
	return rect
}

const DBL_MIN, DBL_MAX = -1.0e10, +1.0e10

// extractTables converts the`paras` that are table cells to tables containing those cells.
func (paras paraList) extractTables() paraList {
	common.Log.Debug("extractTables=%d ===========x=============", len(paras))
	if len(paras) < 4 {
		return paras
	}

	cells := cellList(paras)
	tables := cells.findTables()
	logTables(tables, "find tables")

	// tables := paras.extractTableAtoms()
	// logTables(tables, "table atoms")
	// tables = combineTables(tables)
	// logTables(tables, "table molecules")
	// // if len(tables) == 0 {panic("NO TABLES")}
	// showParas("tables extracted")
	paras = paras.applyTables(tables)
	paras.log("tables applied")
	paras = paras.trimTables()
	paras.log("tables trimmed")

	return paras
}

func (paras paraList) trimTables() paraList {
	var recycledParas paraList
	seen := map[*textPara]bool{}
	for _, para := range paras {
		table := para.table
		if table == nil {
			continue
		}
		for _, p := range paras {
			if p == para {
				continue
			}
			if !overlapped(table, p) {
				continue
			}
			// common.Log.Info("overlap REMOVE:\n\ttable=%s\n\t p=%s", table.String(), p.String())
			table.log("REMOVE")
			for _, cell := range table.cells {
				if _, ok := seen[cell]; ok {
					continue
				}
				recycledParas = append(recycledParas, cell)
				seen[cell] = true
			}
			para.table.cells = nil

		}
	}

	for _, p := range paras {
		if p.table != nil && p.table.cells == nil {
			continue
		}
		recycledParas = append(recycledParas, p)
	}
	return recycledParas
}

func (paras paraList) applyTables(tables []*textTable) paraList {
	// if len(tables) == 0 {panic("no tables")}
	consumed := map[*textPara]bool{}
	for _, table := range tables {
		if len(table.cells) == 0 {
			panic("no cells")
		}
		for _, para := range table.cells {
			consumed[para] = true
		}
	}
	// if len(consumed) == 0 {panic("no paras consumed")}

	var tabled paraList
	for _, table := range tables {
		if table.cells == nil {
			panic(table)
		}
		tabled = append(tabled, table.newTablePara())
	}
	for _, para := range paras {
		if _, ok := consumed[para]; !ok {
			tabled = append(tabled, para)
		}
	}
	if verboseTable {
		common.Log.Info("applyTables: %d->%d tables=%d", len(paras), len(tabled), len(tables))
	}
	return tabled
}

func yOverlap(para1, para2 *textPara) bool {
	//  blk2->yMin <= blk1->yMax &&blk2->yMax >= blk1->yMin
	return para2.Lly <= para1.Ury && para1.Lly <= para2.Ury
}
func xOverlap(para1, para2 *textPara) bool {
	//  blk2->yMin <= blk1->yMax &&blk2->yMax >= blk1->yMin
	return para2.Llx <= para1.Urx && para1.Llx <= para2.Urx
}
func toRight(para2, para1 *textPara) bool {
	//  blk2->yMin <= blk1->yMax &&blk2->yMax >= blk1->yMin
	return para2.Llx > para1.Urx
}
func below(para2, para1 *textPara) bool {
	//  blk2->yMin <= blk1->yMax &&blk2->yMax >= blk1->yMin
	return para2.Ury < para1.Lly
}

// func (paras cellList) cellDepths() []float64 {
// 	topF := func(p *textPara) float64 { return p.Ury }
// 	botF := func(p *textPara) float64 { return p.Lly }
// 	top := paras.calcCellDepths(topF)
// 	bottom := paras.calcCellDepths(botF)
// 	if len(bottom) < len(top) {
// 		return bottom
// 	}
// 	return top
// }

// func (paras cellList) calcCellDepths(getY func(*textPara) float64) []float64 {
// 	depths := []float64{getY(paras[0])}
// 	delta := paras.fontsize() * maxIntraDepthGapR
// 	for _, para := range paras {
// 		newDepth := true
// 		y := getY(para)
// 		for _, d := range depths {
// 			if math.Abs(d-getY(para)) < delta {
// 				newDepth = false
// 				break
// 			}
// 		}
// 		if newDepth {
// 			depths = append(depths, y)
// 		}
// 	}
// 	return depths
// }

func (t *textTable) __corners() paraList {
	w, h := t.w, t.h
	if w == 0 || h == 0 {
		panic(t)
	}
	cnrs := paraList{
		t.get(0, 0),
		t.get(w-1, 0),
		t.get(0, h-1),
		t.get(w-1, h-1),
	}
	for i0, c0 := range cnrs {
		for _, c1 := range cnrs[:i0] {
			if c0.serial == c1.serial {
				panic("dup")
			}
		}
	}
	return cnrs
}

// func newTable(cells cellList, w, h int) textTable {
// 	if w == 0 || h == 0 {
// 		panic("emprty")
// 	}
// 	for i0, c0 := range cells {
// 		for _, c1 := range cells[:i0] {
// 			if c0.serial == c1.serial {
// 				panic("dup")
// 			}
// 		}
// 	}
// 	rect := cells[0].PdfRectangle
// 	for _, c := range cells[1:] {
// 		rect = rectUnion(rect, c.PdfRectangle)
// 	}
// 	return textTable{
// 		PdfRectangle: rect,
// 		w:            w,
// 		h:            h,
// 		cells:        cells,
// 	}
// }

func (table *textTable) newTablePara() *textPara {
	// var cells cellList
	// for _, cell := range table.cells {
	// 	if cell != nil {
	// 		cells = append(cells, cell)
	// 	}
	// }
	// sort.Slice(cells, func(i, j int) bool { return diffDepthReading(cells[i], cells[j]) < 0 })
	// table.cells = cells
	bbox := table.bbox()
	para := textPara{
		serial:       serial.para,
		PdfRectangle: bbox,
		eBBox:        bbox,
		table:        table,
	}
	table.log(fmt.Sprintf("newTablePara: serial=%d", para.serial))

	serial.para++
	return &para
}

// aligned2x2X return an X alignment score for the 2x2 table atom `cells`.
func (cells cellList) aligned2x2X(delta float64) int {
	if len(cells) != 4 {
		panic(fmt.Errorf("cells=%d", len(cells)))
	}
	matches := 0
	for _, get := range gettersX {
		if cells.aligned(get, delta, 0, 2) && cells.aligned(get, delta, 1, 3) {
			matches++
		}
	}
	return matches
}

// aligned2x2Y return a Y alignment score for the 2x2 table atom `cells`.
func (cells cellList) aligned2x2Y(delta float64) int {
	if len(cells) != 4 {
		panic(fmt.Errorf("cells=%d", len(cells)))
	}
	matches := 0
	for _, get := range gettersY {
		if cells.aligned(get, delta, 0, 1) && cells.aligned(get, delta, 2, 3) {
			matches++
		}
	}
	return matches
}

func (cells cellList) alignedY(delta float64) int {
	worstMatches := 100
	for i := 1; i < len(cells); i++ {
		matches := 0
		for _, get := range gettersY {
			if cells.aligned(get, delta, i-1, i) {
				matches++
			}
		}
		if matches < worstMatches {
			worstMatches = matches
		}
	}
	return worstMatches
}

// aligned returns true if `cells` are aligned on attribute `get` for indexes `i` and 'j`.
func (cells cellList) aligned(get getter, delta float64, i, j int) bool {
	if !(0 <= i && i < len(cells) && 0 <= j && j < len(cells)) {
		panic(fmt.Errorf("i=%d j=%d cells=%d", i, j, len(cells)))
	}
	return parasAligned(get, delta, cells[i], cells[j])
}

// parasAligned returns true if `para1` and `para2` are aligned within `delta` for attribute `get`.
func parasAligned(get getter, delta float64, para1, para2 *textPara) bool {
	if get == nil {
		panic("no get")
	}
	z1 := get(para1)
	z2 := get(para2)
	return math.Abs(z1-z2) <= delta
}

// parasAligned2 returns true if `para1` and `para2` are aligned within `delta` for any attribute in
// `getters`.
func parasAligned2(getters []getter, delta float64, para1, para2 *textPara) bool {
	for _, get := range getters {
		z1 := get(para1)
		z2 := get(para2)
		if math.Abs(z1-z2) <= delta {
			return true
		}
	}
	return false
}

// parasAligned2 returns true if `para1` and `para2` are aligned within `delta` for any attribute in
// `getters`.
func parasAligned3(getters []getter, delta float64, cells cellList, cell *textPara) getter {
	// cells2 := append(cells, cell)
	cells2 := make(cellList, len(cells)+1)
	copy(cells2, cells)
	cells2[len(cells)] = cell
	// for i, c := range cells {

	// }
	for _, get := range getters {
		if cells2.alignedGetter(get, delta) {
			return get
		}
	}
	return nil
}

// parasAligned2 returns true if `para1` and `para2` are aligned within `delta` for any attribute in
// `getters`.
func parasAligned4(delta float64, getCells *alignment, cell *textPara) bool {
	// cells2 := append(cells, c{{ell)
	if len(getCells.cells) == 0 {
		return true
	}
	cells2 := make(cellList, len(getCells.cells)+1)
	copy(cells2, getCells.cells)
	cells2[len(getCells.cells)] = cell

	get := getCells.getter()
	return cells2.alignedGetter(get, delta)
}

func (cells cellList) alignedGetter(get getter, delta float64) bool {
	if len(cells) == 0 {
		return true
	}
	cell0 := cells[0]
	for _, cell := range cells[1:] {
		if !parasAligned(get, delta, cell0, cell) {
			return false
		}
	}
	return true
}

// fontsize for a paraList is the minimum font size of the paras.
func (cells cellList) fontsize() float64 {
	size := -1.0
	for _, p := range cells {
		if p != nil {
			if size < 0 {
				size = p.fontsize()
			} else {
				size = math.Min(size, p.fontsize())
			}
		}
	}
	return size
}

// insertAt inserts `table` in `t` at `x`, `y`.
func (t *textTable) insertAt(x, y int, table *textTable) {
	if !(0 <= x && x < t.w) {
		panic(fmt.Errorf("x=%d is an invalid insertion for %s", x, t))
	}
	if !(0 <= y && y < t.h) {
		panic(fmt.Errorf("y=%d is an invalid insertion for %s", y, t))
	}
	if t.w < x+table.w {
		panic(fmt.Errorf("x=%d is an invalid insertion for %s", x, t))
	}
	if t.h < y+table.h {
		panic(fmt.Errorf("y=%d is an invalid insertion for %s", y, t))
	}
	for idx, cell := range table.cells {
		idx.x += x
		idx.y += y
		t.cells[idx] = cell
		t.PdfRectangle = rectUnion(t.PdfRectangle, cell.PdfRectangle)
	}
}

// subTable returns the `w` x `h` subtable of `t` at 0,0.
func (t *textTable) subTable(w, h int) *textTable {
	if !(1 <= w && w <= t.w) {
		panic(fmt.Errorf("w=%d is an invalid sub-width for %s", w, t))
	}
	if !(1 <= h && h <= t.h) {
		panic(fmt.Errorf("h=%d is an invalid sub-height for %s", h, t))
	}
	table := newTextTable(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			cell := t.get(x, y)
			if cell == nil {
				continue
			}
			table.put(x, y, cell)
			table.PdfRectangle = rectUnion(table.PdfRectangle, cell.PdfRectangle)
		}
	}
	return table
}

// row returns the (0-offset) `y`th row in `t`.
func (t textTable) row(y int) cellList {
	if !(0 <= y && y < t.h) {
		panic(fmt.Errorf("y=%d is an invalid row for %s", y, t.String()))
	}
	cells := make(cellList, t.w)
	for x := 0; x < t.w; x++ {
		cells[x] = t.get(x, y)
	}
	return cells
}

// column returns the (0-offset) `x`th column in `t`.
func (t textTable) column(x int) cellList {
	if !(0 <= x && x < t.w) {
		panic(fmt.Errorf("x=%d is an invalid column for %s", x, t.String()))
	}
	cells := make(cellList, t.h)
	for y := 0; y < t.h; y++ {
		cells[y] = t.get(x, y)
	}
	return cells
}

// cellSet returns `cells` as a set.
func (cells cellList) cellSet() map[*textPara]bool {
	set := map[*textPara]bool{}
	for _, cell := range cells {
		set[cell] = true
	}
	return set
}

// overlapRange returns i0, i1 where cells[i0,i1] is the maximum overlap with `other`.
func (cells cellList) overlapRange(other cellList) (int, int) {
	i0, i1 := -1, len(cells)
	for i, c := range cells {
		if i0 < 0 {
			if c == other[0] {
				i0 = i
			}
			continue
		}
		if i-i0 >= len(other) || c != other[i-i0] {
			i1 = i
			break
		}
	}
	if i0 < 0 {
		panic("no match")
	}
	return i0, i1
}

// toTextTable returns the TextTable corresponding to `t`.
func (t textTable) toTextTable() TextTable {
	cells := make([][]string, t.h)
	for y := 0; y < t.h; y++ {
		cells[y] = make([]string, t.w)
		for x := 0; x < t.w; x++ {
			cell := t.get(x, y)
			if cell != nil {
				cells[y][x] = cell.text()
			}
		}
	}
	return TextTable{W: t.w, H: t.h, Cells: cells}
}

//
// Cell sorting
//
//   x     x    x      x     x     x
//   x
//   x     x
//   x
//   x     x           x
//   x
//   x

// 1. Compute all row candidates
//      alignedY  No intervening paras
// 2. Compute all column candidates
//      alignedX  No intervening paras

// Table candidate
// 1. Top row fully populated
// 2. Left column fully populated
// 3. All cells in table are aligned with 1 top row element and 1 left column candidate
// 4. Mininum number of cells must be filled

// Computation time
// 1. Row candidates  O(N)
//    Sort top to bottom, left to right
//    Search
// 2. Column candidates O(N)
//    Sort left to right, top to bottom
//    Search
// 3. Find intersections  O(N^2)
//    For each row
//       Find columns that start at row -> table candiates
//    Sort table candidates by w x h descending
// 4. Test each candidate O(N^4)

func (cells cellList) findTables() []*textTable {
	if verboseTable {
		common.Log.Info("findTables @@1: cells=%d", len(cells))
		common.Log.Info("cols <- findGetterCandidates(getXLl, maxIntraReadingGapR, false)")
	}
	cols := cells.findGetterCandidates(_getXLl, gettersX, maxIntraReadingGapR, false)
	sortContents(_getYUr, true, cols)

	if verboseTable {
		common.Log.Info("rows <- findGetterCandidates(getYUr, lineDepthR, true)")
	}
	rows := cells.findGetterCandidates(_getYUr, gettersY, lineDepthR, true)
	sortContents(_getXLl, false, rows)

	if verboseTable {
		common.Log.Info("findTables @@2a: rows=%d", len(rows))
		for i, getCell := range rows {
			col := getCell.cells
			fmt.Printf("%4d: %6.2f %2d: %q\n", i, col.bbox(), len(col), truncate(col.text(), 80))
			col.validate()
		}
		common.Log.Info("findTables @@2b: cols=%d", len(cols))
		for i, getCell := range cols {
			col := getCell.cells
			fmt.Printf("%4d: %6.2f %2d: %q\n", i, col.bbox(), len(col), truncate(col.text(), 80))
			col.validate()
		}
		common.Log.Info("findTables @@2: candidates cols=%d rows=%d", len(cols), len(rows))
	}
	if len(cols) == 0 || len(rows) == 0 {
		return nil
	}

	alignmentMap := makeAlignmentMap(cols, rows)

	tables := cells.findTableCandidates(cols, rows, alignmentMap)
	for _, table := range tables {
		table.validate()
	}
	logTables(tables, "candidates")
	tables = removeDuplicateTables((tables))
	logTables(tables, "distinct")
	return tables
}

func (cells cellList) text() string {
	texts := make([]string, len(cells))
	for i, para := range cells {
		texts[i] = truncate(para.text(), 20)
	}
	return strings.Join(texts, "|")
}

func removeDuplicateTables(tables []*textTable) []*textTable {
	if len(tables) == 0 {
		return nil
	}
	sort.Slice(tables, func(i, j int) bool {
		ti, tj := tables[i], tables[j]
		ai, aj := ti.w*ti.h, tj.w*tj.h
		if ai != aj {
			return ai > aj
		}
		return ti.Ury > tj.Ury
	})
	distinct := []*textTable{tables[0]}
	tables[0].log("removeDuplicateTables 0")
outer:
	for _, t := range tables[1:] {
		for _, d := range distinct {
			if overlapped(t, d) {
				continue outer
			}
		}
		t.log("removeDuplicateTables x")
		distinct = append(distinct, t)
	}
	return distinct
}

// !@#$
// For each row. column pair row0, col9
//   Find
//      (maximum columns starting at row0) x
//      (maxumn rows starting at col0)
//   For each column check alignment with col0
//   For each row check alignment with row0
//   Check for other paras polluting grid
func (cells cellList) findTableCandidates(cols, rows []*alignment,
	alignmentMap map[*textPara]xyAlignment) []*textTable {
	if verboseTable {
		common.Log.Info("findTableCandidates: cols=%d rows=%d", len(cols), len(rows))
		fmt.Printf("\tCandidates\n")
	}

	var candidates [][2]cellList
	for x, col := range cols {
		for y, row := range rows {
			gcol2, grow2 := makeCandidate(col, row)
			col2 := gcol2.cells
			row2 := grow2.cells
			if col2 != nil && len(col2) >= 2 && len(row2) >= 2 {
				if verboseTable {
					fmt.Printf("\t\t%d: [%2d %2d]\n"+
						"\t\t\t  col=%s\n"+
						"\t\t\t  row=%s\n"+
						"\t\t\t->col=%s\n"+
						"\t\t\t  row=%s\n",
						len(candidates), x, y, col, row, col2, row2)
				}
				candidates = append(candidates, [2]cellList{col2, row2})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		ci, cj := candidates[i], candidates[j]
		ai := len(ci[0]) * len(ci[1])
		aj := len(cj[0]) * len(cj[1])
		if ai == 0 || aj == 0 {
			panic("emprty")
		}
		if ai != aj {
			return ai > aj
		}
		return i < j
	})
	if verboseTable {
		common.Log.Info("sorted candidates")
	}
	var tables []*textTable
	for i, cand := range candidates {
		col, row := cand[0], cand[1]
		if verboseTable {
			fmt.Printf("\t %2d: findTableCandidates: col=%2d row=%2d \n"+
				"\t\tcol=%6.2f%s\n"+
				"\t\trow=%6.2f%s\n",
				i, len(col), len(row),
				col.bbox(), col,
				row.bbox(), row)
		}

		if col.equals(row) {
			// panic(fmt.Errorf("columns can't be rows\n\tcol=%6.2f %q\n\trow=%6.2f %q",
			// 	col.bbox(), col.asStrings(), row.bbox(), row.asStrings()))
			// common.Log.Error("columns can't be rows\n\tcol=%6.2f %q\n\trow=%6.2f %q",
			// 	col.bbox(), col.asStrings(), row.bbox(), row.asStrings())
			continue
		}
		if len(col) == 0 || len(row) == 0 {
			panic("emmmpty")
		}
		boundary := append(row, col...).bbox()

		subset := cells.within(boundary)
		table := subset.validTable(col, row, alignmentMap)
		// fmt.Printf("%12s boundary=%6.2f subset=%3d=%6.2f valid=%t\n", "",
		// 	boundary, len(subset), subset.bbox(), table != nil)
		if table != nil {
			table.log("VALID!!")
			table.validate()
			tables = append(tables, table)
		}
	}
	return tables
}

// within returns the elements of `cells` that are within `boundary`.
func (cells cellList) within(boundary model.PdfRectangle) cellList {
	var subset cellList
	for _, cell := range cells {
		if rectContainsBounded(boundary, cell) {
			subset = append(subset, cell)
		}
	}
	return subset
}

// makeCandidate returns (col`, row`) where
//    col` is the subslice of col starting at row[0]
//    row` is the subslice of row starting at col[0]
// If no element of col intersects with row[0] and no no element of row intersects with col[0] then
//  nil, nil is returned.
// The returned (col`, row`) are candidates to be the left column and top row of a table.
func makeCandidate(gcol, grow *alignment) (*alignment, *alignment) {
	col := gcol.cells
	row := grow.cells
	repack := func(col, row cellList) (*alignment, *alignment) {
		ggcol := &alignment{getCount: gcol.getCount, cells: col}
		ggrow := &alignment{getCount: grow.getCount, cells: row}
		return ggcol, ggrow
	}

	var col1, row1 cellList
	for i, c := range col {
		if c == row[0] {
			col1 = col[i:]
			row1 = row
			break
		}
	}
	var col2, row2 cellList
	for i, c := range row {
		if c == col[0] {
			col2 = col
			row2 = row[i:]
			break
		}
	}
	if col1 == nil && col2 == nil {
		return repack(nil, nil)
	}
	if col1 != nil {
		if col2 != nil {
			if len(col1)*len(row1) >= len(col2)*len(row2) {
				return repack(col1, row1)
			}
			return repack(col2, row2)
		}
		return repack(col1, row1)
	}
	return repack(col2, row2)
}

// validTable returns a sparse table containing `cells`if `cells` make up a valid table with `col`
// on its left and `row` on its top.
// nil is returned if there is no valid table
func (cells cellList) validTable(col, row cellList, alignmentMap map[*textPara]xyAlignment) *textTable {
	w, h := len(row), len(col)
	if col.equals(row) {
		panic("columns can't be rows")
	}
	if col[0] != row[0] {
		panic("bad intersection")
	}
	if verboseTable {
		common.Log.Info("validTable: w=%d h=%d cells=%d", w, h, len(cells))
	}

	table := newTextTable(w, h)
	for x, cell := range row {
		table.put(x, 0, cell)
	}
	for y, cell := range col {
		table.put(0, y, cell)
	}
	fontsize := table.fontsize()
	for i, cell := range cells {
		xya := alignmentMap[cell]
		getX, getY := xya.x, xya.y
		y := col.getAlignedIndex(getY, fontsize*lineDepthR, cell)
		x := row.getAlignedIndex(getX, fontsize*maxIntraReadingGapR, cell)
		if x < 0 || y < 0 {
			if verboseTable {
				common.Log.Error("bad element: x=%d y=%d cell=%s", x, y, cell.String())
			}
			return nil
		}
		if verboseTable {
			fmt.Printf("%4d: y=%d x=%d %q\n", i, y, x, truncate(cell.text(), 50))
		}
		table.put(x, y, cell)
		fontsize = table.fontsize()
	}

	w, h = table.maxDense()
	if verboseTable {
		common.Log.Info("maxDense: w=%d h=%d", w, h)
	}
	if w < 0 {
		return nil
	}
	if !table.isValid() {
		return nil
	}
	return table.subTable(w, h)
}

func (t *textTable) maxDense() (int, int) {
	var product [][2]int
	for h := 2; h <= t.h; h++ {
		for w := 2; w <= t.w; w++ {
			product = append(product, [2]int{w, h})
		}
	}
	if len(product) == 0 {
		return -1, -1
	}
	sort.Slice(product, func(i, j int) bool {
		pi, pj := product[i], product[j]
		ai := pi[0] * pi[1]
		aj := pj[0] * pj[1]
		if ai != aj {
			return ai > aj
		}
		if pi[1] != pj[1] {
			return pi[1] > pj[1]
		}
		return i < j
	})
	for i, p := range product {
		w, h := p[0], p[1]
		dense, reason := t.isDense(w, h)
		if verboseTable {
			fmt.Printf("%d: isDense w=%d h=%d dense=%5t %s\n", i, w, h, dense, reason)
		}
		if dense {
			return w, h
		}
	}
	return -1, -1
}

func (t *textTable) isDense(w, h int) (bool, string) {
	minOccRow := 2
	minOccCol := 2
	minOccR := 0.3

	count := 0
	for x := 0; x < w; x++ {
		n := t.column(x).count()
		if n < minOccCol {
			// common.Log.Error("col %d has %d entries", x, n, t.column(x).asStrings())
			return false, fmt.Sprintf("col %d has %d entries %s", x, n, t.column(x).asStrings())
		}
		count += n
	}
	for y := 0; y < h; y++ {
		n := t.row(y).count()
		if n < minOccRow {
			// common.Log.Error("row %d has %d entries %s", y, n, t.row(y).asStrings())
			return false, fmt.Sprintf("row %d has %d entries %s", y, n, t.row(y).asStrings())
		}
	}
	occupancy := float64(count) / float64(w*h)
	if occupancy < minOccR {
		// common.Log.Error("table has %d of %d = %.2f entries", count, t.w*t.h, occupancy)
		return false, fmt.Sprintf("table has %d of %d = %.2f entries", count, w*h, occupancy)
	}
	return true, ""
}

func (cells cellList) count() int {
	n := 0
	for _, c := range cells {
		if c != nil {
			n++
		}
	}
	return n
}

func (cells cellList) validate() {
	for i := 0; i < len(cells); i++ {
		for j := i + 1; j < len(cells); j++ {
			if cells[i] == cells[j] {
				panic(fmt.Errorf("duplicate cell: i=%d j=%d %s", i, j, cells[i].String()))
			}
		}
	}
}

// func (cells cellList) getAlignedIndexX(delta float64, targetCell *textPara) int {
// 	for _, get := range gettersX {
// 		for i, cell := range cells {
// 			if parasAligned(get, delta, targetCell, cell) {
// 				return i
// 			}
// 		}
// 	}
// 	return -1
// }

// // getAlignedIndexY returns the index in `cells` that is aligned with `targetCell` in the Y axis.
// // nil is returned if no cell is aligned.
// func (cells cellList) getAlignedIndexY(delta float64, targetCell *textPara) int {
// 	for _, get := range gettersY {
// 		for i, cell := range cells {
// 			if parasAligned(get, delta, targetCell, cell) {
// 				return i
// 			}
// 		}
// 	}
// 	return -1
// }

func (cells cellList) getAlignedIndex(get getter, delta float64, targetCell *textPara) int {
	if targetCell == nil {
		panic("no targetCell")
	}
	for i, cell := range cells {
		if cell == nil {
			panic("no cell")
		}
		if parasAligned(get, delta, targetCell, cell) {
			return i
		}
	}
	return -1
}

func sortContents(get getter, reverse bool, cols []*alignment) {
	for _, alm := range cols {
		cells := alm.cells
		sort.Slice(cells, func(i, j int) bool {
			ci, cj := cells[i], cells[j]
			if reverse {
				return get(ci) > get(cj)
			}
			return get(ci) < get(cj)
		})
	}
}

// alignment is a column.row candidate
type alignment struct {
	getCount map[reflect.Value]int // getter: number of cells matched by getter
	cells    cellList              // cells
}

func newAlignment(cell *textPara) *alignment {
	return &alignment{getCount: map[reflect.Value]int{}, cells: cellList{cell}}
}

func (col *alignment) String() string {
	return col.cells.String()
}

// getter returns the getter used for the majority cell alignments.
func (col *alignment) getter() getter {
	var getters []reflect.Value
	for getVal := range col.getCount {
		getters = append(getters, getVal)
	}
	sort.Slice(getters, func(i, j int) bool {
		ci := col.getCount[getters[i]]
		cj := col.getCount[getters[i]]
		return ci > cj
	})
	if len(getters) >= 2 && col.getCount[getters[0]] == col.getCount[getters[1]] {
		panic(col)
	}
	getVal := getters[0]
	return valueGetter[getVal]
}

// add adds a new cell and its alignment method to `col`.
func (col *alignment) add(get getter, cell *textPara) {
	col.getCount[reflect.ValueOf(get)]++
	col.cells = append(col.cells, cell)
	col.cells.validate()
}

// findGetterCandidates returns list of elements of `cells` that are within `delta` for attribute `get`.
func (cells cellList) findGetterCandidates(get getter, getters []getter, deltaR float64, reverse bool) []*alignment {
	delta := cells.fontsize() * deltaR
	index := cells.makeIndex(get)
	var columns []*alignment
	seen := map[string]bool{}
	addCol := func(col *alignment) {
		if len(col.cells) > 1 {
			if verboseTable {
				fmt.Printf("%8s-> %3d: %s\n", "", len(columns), col.String())
			}
			if _, ok := seen[col.String()]; ok {
				panic(col.String())
			}
			seen[col.String()] = true
			columns = append(columns, col)
		}
	}
	if verboseTable {
		common.Log.Info("findGetterCandidates: %d", len(cells))
	}
	for i0 := 0; i0 < len(cells); i0++ {
		cell0 := cells[index[i0]]
		if verboseTable {
			fmt.Printf("%4d: %s\n", i0, cell0)
		}
		col := newAlignment(cell0)
		for i := i0 + 1; i < len(cells); i++ {
			cell := cells[index[i]]
			if verboseTable {
				fmt.Printf("%8d: %s\n", i, cell)
			}
			if get(cell) < get(cell0) {
				panic("order")
			}
			// if get(cell) > get(cell0)+delta {
			// 	addCol(col)
			// 	col = cellList{cell}
			// } else if parasAligned2(getters, delta, cell0, cell) {
			// 	col = append(col, cell)
			// 	col.validate()
			// }
			// if parasAligned2(getters, delta, cell0, cell) {
			get2 := parasAligned3(getters, delta, col.cells, cell)
			if get2 != nil {
				col.add(get2, cell)
			} else {
				addCol(col)
				col = newAlignment(cell)
				break
			}
		}
		addCol(col)
	}
	sort.Slice(columns, func(i, j int) bool {
		ci, cj := columns[i].cells, columns[j].cells
		if len(ci) != len(cj) {
			return len(ci) > len(cj)
		}
		if reverse {
			return get(ci[0]) > get(cj[0])
		}
		return get(ci[0]) < get(cj[0])
	})
	return columns
}

type xyAlignment struct {
	x, y getter
}

// makeAlignmentMap returns a map of cells to their x and y alignments in `cols` and `rows`.
func makeAlignmentMap(cols, rows []*alignment) map[*textPara]xyAlignment {
	// xa maps cell: aligned col
	xa := map[*textPara]*alignment{}
	for _, a := range cols {
		for _, cell := range a.cells {
			if b, ok := xa[cell]; ok {
				if len(a.cells) <= len(b.cells) {
					continue
				}
				panic(fmt.Errorf("2 alignments\n\tcell=%s\n\ta=%s\n\tb=%s",
					cell.String(), a.String(), b.String()))
			}
			xa[cell] = a
		}
	}
	ya := map[*textPara]*alignment{}
	for _, a := range rows {
		for _, cell := range a.cells {
			if b, ok := ya[cell]; ok {
				if len(a.cells) <= len(b.cells) {
					continue
				}
				panic(cell)
			}
			ya[cell] = a
		}
	}
	xya := map[*textPara]xyAlignment{}
	for cell, col := range xa {
		if row, ok := ya[cell]; ok {
			xya[cell] = xyAlignment{x: col.getter(), y: row.getter()}
		}
	}
	return xya
}

func (cells cellList) equals(other cellList) bool {
	if len(cells) != len(other) {
		return false
	}
	for i, cell := range cells {
		if other[i] != cell {
			return false
		}
	}
	return true
}

// // makeIndex returns an indexes over cells on the `Llx` and `Ury `attributes.
// func (cells cellList) xyIndexes() ([]int, []int) {
// 	xIndex := cells.makeIndex(_getXLl)
// 	yIndex := cells.makeIndex(_getYUr)
// 	return xIndex, yIndex
// }

// makeIndex returns an index over cells on the `get` attributes.
func (cells cellList) makeIndex(get getter) []int {
	index := make([]int, len(cells))
	for i := range cells {
		index[i] = i
	}
	sort.Slice(index, func(i, j int) bool {
		zi := get(cells[index[i]])
		zj := get(cells[index[j]])
		return zi < zj
	})
	return index
}

type getter func(*textPara) float64

var (
	// gettersX get the x-center, left and right of cells.
	gettersX = []getter{_getXCe, _getXLl, _getXUr}
	// gettersX get the y-center, bottom and top of cells.
	gettersY = []getter{_getYCe, _getYLl, _getYUr}
	// valueGetter is the reverse map reflect.ValueOf(get)] -> get
	valueGetter = makeValueGetter()
)

func makeValueGetter() map[reflect.Value]getter {
	gettersAll := [][]getter{gettersX, gettersY}
	valueGetter := map[reflect.Value]getter{}
	for _, getters := range gettersAll {
		for _, get := range getters {
			valueGetter[reflect.ValueOf(get)] = get
		}
	}
	return valueGetter
}

func _getXCe(para *textPara) float64 { return 0.5 * (para.Llx + para.Urx) }
func _getXLl(para *textPara) float64 { return para.Llx }
func _getXUr(para *textPara) float64 { return para.Urx }
func _getYCe(para *textPara) float64 { return 0.5 * (para.Lly + para.Ury) }
func _getYLl(para *textPara) float64 { return para.Lly }
func _getYUr(para *textPara) float64 { return para.Ury }
func _getTop(para *textPara) float64 { return -para.Ury }

func (cells cellList) log(title string) {
	paraList(cells).log(title)
}

// logTables logs the contents of `tables`.
func logTables(tables []*textTable, title string) {
	if !verboseTable {
		return
	}
	common.Log.Info("%8s: %d tables =======!!!!!!!!=====", title, len(tables))
	for i, t := range tables {
		t.log(fmt.Sprintf("%s-%02d", title, i))
	}
}

// log logs the contents of `table`.
func (t *textTable) log(title string) {
	if !verboseTable {
		return
	}
	fmt.Printf("%4s[%dx%d] %s ++++++++++\n", "", t.w, t.h, title)
	if t.w == 0 || t.h == 0 {
		return
	}
	top := t.row(0)
	left := t.column(0)
	fmt.Printf("%8s top=%q\n", "", top.asStrings())
	fmt.Printf("%8sleft=%q\n", "", left.asStrings())
	// return
	// common.Log.Info("%8s: %s: %2d x %2d %6.2f =======//////////=====\n"+
	// 	"      %6.2f", title, fileLine(1, false),
	// 	table.w, table.h, table.PdfRectangle, table.PdfRectangle)
	// for i, p := range table.cells {
	// 	if p == nil {
	// 		continue
	// 	}
	// 	fmt.Printf("%4d: %6.2f %q\n", i, p.PdfRectangle, truncate(p.text(), 50))
	// }
}

func (cells cellList) asStrings() []string {
	n := minInt(5, len(cells))
	parts := make([]string, n)
	for i, cell := range cells[:n] {
		if cell != nil {
			parts[i] = truncate(cell.text(), 20)
		}
	}
	if len(cells) > len(parts) {
		cell := cells[len(cells)-1]
		parts = append(parts, "...")
		parts = append(parts, truncate(cell.text(), 20))
	}
	return parts
}
