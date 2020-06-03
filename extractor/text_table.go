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
	t.validate(x, y)
	return t.cells[cellIndex{x, y}]
}
func (t *textTable) put(x, y int, cell *textPara) {
	t.validate(x, y)
	t.cells[cellIndex{x, y}] = cell
}
func (t *textTable) del(x, y int) {
	t.validate(x, y)
	delete(t.cells, cellIndex{x, y})
}

func (t *textTable) validate(x, y int) {
	if !(0 <= x && x < t.w) {
		panic(fmt.Errorf("bad x=%d t=%s", x, t))
	}
	if !(0 <= y && y < t.h) {
		panic(fmt.Errorf("bad y=%d t=%s", y, t))
	}
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
	return fmt.Sprintf("%d %q", len(cells), cells.asStrings())
}

// bbox returns the union of the bounds of `cells`.
func (cells cellList) bbox() model.PdfRectangle {
	rect := cells[0].PdfRectangle
	for _, r := range cells[1:] {
		rect = rectUnion(rect, r.PdfRectangle)
	}
	return rect
}

// type sparseCell struct {
// 	y, x int
// 	*textPara
// }

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
			common.Log.Info("overlap REMOVE:\n\ttable=%s\n\t p=%s", table.String(), p.String())
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
	common.Log.Info("applyTables: %d->%d tables=%d", len(paras), len(tabled), len(tables))
	return tabled
}

// // extractTableAtome returns all the 2x2 table candidates in `paras`.
// func (paras paraList) extractTableAtoms() []textTable {
// 	// Pre-sort by reading direction then depth
// 	sort.Slice(paras, func(i, j int) bool {
// 		return diffReadingDepth(paras[i], paras[j]) < 0
// 	})

// 	var llx0, lly0, llx1, lly1 float64
// 	var tables []textTable

// 	for i1, para1 := range paras {
// 		llx0, lly0 = DBL_MAX, DBL_MIN
// 		llx1, lly1 = DBL_MAX, DBL_MIN

// 		// Build a table atom of 4 cells
// 		//   0 1
// 		//   2 3
// 		// where
// 		//   0 is `para1`
// 		//   1 is on the right of 0 and overlaps with 0 in y axis
// 		//   2 is under 0 and overlaps with 0 in x axis
// 		//   3 is under 1 and on the right of 1 and closest to 0
// 		cells := make(cellList, 4)
// 		cells[0] = para1

// 		for _, para2 := range paras {
// 			if para1 == para2 {
// 				continue
// 			}
// 			if yOverlap(para1, para2) && toRight(para2, para1) && para2.Llx < llx0 {
// 				llx0 = para2.Llx
// 				cells[1] = para2
// 			} else if xOverlap(para1, para2) && below(para2, para1) && para2.Ury > lly0 {
// 				lly0 = para2.Ury
// 				cells[2] = para2
// 			} else if toRight(para2, para1) && para2.Llx < llx1 && below(para2, para1) && para2.Ury > lly1 {
// 				llx1 = para2.Llx
// 				lly1 = para2.Ury
// 				cells[3] = para2
// 			}
// 		}
// 		// Do we have a table atom?
// 		if !(cells[1] != nil && cells[2] != nil && cells[3] != nil) {
// 			continue
// 		}
// 		// cells.log(fmt.Sprintf("table atom i1=%d", i1))

// 		// 1 cannot overlap with 2 in x and y
// 		// 3 cannot overlap with 2 in x and with 1 in y
// 		// 3 has to overlap with 2 in y and with 1 in x
// 		if (xOverlap(cells[2], cells[3]) || yOverlap(cells[1], cells[3]) ||
// 			xOverlap(cells[1], cells[2]) || yOverlap(cells[1], cells[2])) ||
// 			!(xOverlap(cells[1], cells[3]) && yOverlap(cells[2], cells[3])) {
// 			continue
// 		}
// 		// common.Log.Info("OVERLAP A) i1=%d", i1)

// 		scoreX := cells.aligned2x2X(cells.fontsize() * maxIntraReadingGapR)
// 		scoreY := cells.aligned2x2Y(cells.fontsize() * lineDepthR)
// 		common.Log.Info("OVERLAP C) i1=%d scoreX=%d scoreY=%d", i1, scoreX, scoreY)

// 		// are blocks aligned in x and y ?
// 		if scoreX > 0 && scoreY > 0 {
// 			table := newTable(cells, 2, 2)
// 			tables = append(tables, table)
// 			table.log("New textTable")
// 		}
// 	}
// 	return tables
// }

// 0 1
// 2 3
// A B
// C
// Extensions:
//   A[1] == B[0] right
//   A[2] == C[0] down
// func __combineTables(tables []*textTable) []*textTable {
// 	tablesY := combineTablesY(tables)
// 	tablesX := combineTablesX(tablesY)
// 	return tablesX
// }

// func combineTablesY(tables []*textTable) []*textTable {
// 	sort.Slice(tables, func(i, j int) bool { return tables[i].Ury > tables[j].Ury })
// 	removed := map[int]bool{}

// 	var combinedTables []*textTable
// 	common.Log.Info("combineTablesY ------------------\n\t ------------------")
// 	for i1, t1 := range tables {
// 		if _, ok := removed[i1]; ok {
// 			continue
// 		}
// 		fontsize := t1.fontsize()
// 		c1 := t1.corners()
// 		var combo *textTable
// 		for i2, t2 := range tables {
// 			if _, ok := removed[i2]; ok {
// 				continue
// 			}
// 			if t1.w != t2.w {
// 				continue
// 			}
// 			c2 := t2.corners()
// 			if c1[2] != c2[0] {
// 				continue
// 			}
// 			// common.Log.Info("Comparing i1=%d i2=%d", i1, i2)
// 			// t1.log("t1")
// 			// t2.log("t2")
// 			cells := cellList{
// 				c1[0], c1[1],
// 				c2[2], c2[3],
// 			}
// 			alX := cells.aligned2x2X(fontsize * maxIntraReadingGapR)
// 			alY := cells.aligned2x2Y(fontsize * lineDepthR)
// 			common.Log.Info("alX=%d alY=%d", alX, alY)
// 			if !(alX > 0 && alY > 0) {
// 				if combo != nil {
// 					common.Log.Info("BREAK: i1=%d i2=%d", i1, i2)
// 					t1.log("t1")
// 					t2.log("t2")
// 					combinedTables = append(combinedTables, combo)
// 				}
// 				combo = nil
// 				continue
// 			}
// 			if combo == nil {
// 				combo = t1
// 				removed[i1] = true
// 			}

// 			w, h := combo.w, combo.h
// 			combo.expand(combo.w, h-1+t2.h)
// 			combo.insertAt(0, h-1, t2)
// 			common.Log.Info("COMBINE! %dx%d i1=%d i2=%d", w, h, i1, i2)

// 			combo.PdfRectangle = rectUnion(combo.PdfRectangle, t2.PdfRectangle)
// 			combo.log("combo")
// 			removed[i2] = true
// 			fontsize = combo.fontsize()
// 			c1 = combo.corners()
// 		}
// 		if combo != nil {
// 			combinedTables = append(combinedTables, combo)
// 		}
// 	}

// 	common.Log.Info("combineTablesY a: combinedTables=%d", len(combinedTables))
// 	for i, t := range tables {
// 		if _, ok := removed[i]; ok {
// 			continue
// 		}
// 		combinedTables = append(combinedTables, t)
// 	}
// 	common.Log.Info("combineTablesY b: combinedTables=%d", len(combinedTables))

// 	return combinedTables
// }

// func combineTablesX(tables []*textTable) []*textTable {
// 	sort.Slice(tables, func(i, j int) bool {
// 		ti, tj := tables[i], tables[j]
// 		if ti.h != tj.h {
// 			return ti.h > tj.h
// 		}
// 		return ti.Llx < tj.Llx
// 	})
// 	removed := map[int]bool{}
// 	for i1, t1 := range tables {
// 		if _, ok := removed[i1]; ok {
// 			continue
// 		}
// 		fontsize := t1.fontsize()

// 		t1right := t1.column(t1.w - 1)
// 		t1Map := t1right.cellSet()
// 		overlapTable := map[int]*textTable{}
// 		t2Width := 0
// 		for i2, t2 := range tables {
// 			if _, ok := removed[i2]; ok {
// 				continue
// 			}
// 			if i2 <= i1 {
// 				continue
// 			}
// 			// t1 is taller than t2
// 			t2TL := t2.cells[0]
// 			if _, ok := t1Map[t2TL]; !ok {
// 				continue
// 			}
// 			t2left := t2.column(0)
// 			if t2TL != t2left[0] {
// 				panic("t's don't agree")
// 			}
// 			v0, v1 := t1right.overlapRange(t2left)
// 			if v0 < 0 {
// 				continue
// 			}
// 			common.Log.Info("v0=%d v1=%d t1right=%d t2left=%d", v0, v1, len(t1right), len(t2left))
// 			aligned := true
// 			for k := 0; k < v1-v0; k++ {
// 				t1c := t1right[v0+k]
// 				t2c := t2left[k]
// 				cells := cellList{t1c, t2c}
// 				if cells.alignedY(fontsize*lineDepthR) == 0 {
// 					aligned = false
// 					break
// 				}
// 			}
// 			if aligned {
// 				overlapTable[v0] = t2
// 				if t2.w > t2Width {
// 					t2Width = t2.w
// 				}
// 				removed[i2] = true
// 			}
// 		}

// 		if len(overlapTable) > 0 {
// 			w := t1.w + t2Width
// 			h := t1.h
// 			combined := textTable{
// 				w:     w,
// 				h:     h,
// 				cells: make(cellList, w*h),
// 			}
// 			combined.insertAt(0, 0,&t1)
// 			for y, t := range overlapTable {
// 				combined.insertAt(t1.w-1, y, t)
// 			}

// 			fontsize = combined.cells.fontsize()
// 		}
// 	}
// 	var reduced []*textTable
// 	for i, t := range tables {
// 		if _, ok := removed[i]; ok {
// 			continue
// 		}
// 		reduced = append(reduced, t)
// 	}
// 	return reduced
// }

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
	z1 := get(para1)
	z2 := get(para2)
	return math.Abs(z1-z2) <= delta
}

// fontsize for a paraList is the minimum font size of the paras.
func (paras cellList) fontsize() float64 {
	size := -1.0
	for _, p := range paras {
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
		panic(fmt.Errorf("y=%d is an invalid row for %s", y, t))
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
		panic(fmt.Errorf("x=%d is an invalid column for %s", x, t))
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
	common.Log.Info("findTables @@1: cells=%d", len(cells))
	cols := cells.findGetterCandidates(getXLl, maxIntraReadingGapR, false)
	sortContents(getYUr, true, cols)
	common.Log.Info("findTables @@2: cols=%d", len(cols))
	rows := cells.findGetterCandidates(getYUr, lineDepthR, true)
	sortContents(getXLl, false, rows)
	common.Log.Info("findTables @@3: rows=%d", len(rows))
	if len(cols) == 0 || len(rows) == 0 {
		return nil
	}
	common.Log.Info("cols %d =================", len(cols))
	// for i, v := range cols[:10] {
	// 	fmt.Printf("%4d: %d %6.2f %q\n", i, len(v), v.bbox(), v.asStrings())
	// 	if i > 3 {
	// 		continue
	// 	}
	// 	for j, c := range v[:3] {
	// 		fmt.Printf("%8d: %6.2f %q\n", j, c.bbox(), c.text())
	// 	}
	// }
	common.Log.Info("rows %d =================", len(rows))
	// for i, v := range rows[:10] {
	// 	fmt.Printf("%4d: %d %6.2f %q\n", i, len(v), v.bbox(), v.asStrings())
	// }
	tables := cells.findTableCandidates(cols, rows)
	common.Log.Info("findTables @@4: tables=%d", len(tables))
	logTables(tables, "candidtates")
	tables = removeDuplicateTables((tables))
	common.Log.Info("findTables @@5: tables=%d", len(tables))
	logTables(tables, "distinct")
	return tables
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

func makeCandidate(col, row cellList) (cellList, cellList) {
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
	if col1 != nil && col2 != nil {
		if len(col1)*len(row1) >= len(col2)*len(row2) {
			return col1, row1
		}
		return col2, row2
	}
	if col1 != nil {
		return col1, row1
	}
	return col2, row2
}

func (cells cellList) findTableCandidates(cols, rows []cellList) []*textTable {
	common.Log.Info("findTableCandidates: cols=%d rows=%d\n\tcols=%s\n\trows=%s",
		len(cols), len(rows), cols[0].String(), rows[0].String())

	var candidates [][2]cellList
	for _, col := range cols {
		for _, row := range rows {
			col2, row2 := makeCandidate(col, row)
			if col2 != nil && len(col2) >= 2 && len(row2) >= 2 {
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
	var tables []*textTable
	for i, cand := range candidates {
		col, row := cand[0], cand[1]

		fmt.Printf("%8d: findTableCandidates: col=%2d %6.2f row=%2d %6.2f\n\tcol=%s\n\trow=%s\n",
			i, len(col), col.bbox(), len(row), row.bbox(), col.asStrings(), row.asStrings())

		if col.equals(row) {
			// panic(fmt.Errorf("columns can't be row\n\tcol=%6.2f %q\n\trow=%6.2f %q",
			// 	col.bbox(), col.asStrings(), row.bbox(), row.asStrings()))
			common.Log.Error("columns can't be row\n\tcol=%6.2f %q\n\trow=%6.2f %q",
				col.bbox(), col.asStrings(), row.bbox(), row.asStrings())
			continue
		}
		if len(col) == 0 || len(row) == 0 {
			panic("emmmpty")
		}
		boundary := append(row, col...).bbox()

		subset := cells.within(boundary)
		table := subset.validTable(col, row)
		// fmt.Printf("%12s boundary=%6.2f subset=%3d=%6.2f valid=%t\n", "",
		// 	boundary, len(subset), subset.bbox(), table != nil)
		if table != nil {
			table.log("VALID!!")
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

// validTable returns a sparse table containing `cells`if `cells` make up a valid table with `col`
// on its left and `row` on its top.
// nil is returned if there is no valid table
func (cells cellList) validTable(col, row cellList) *textTable {
	w, h := len(row), len(col)
	if col.equals(row) {
		panic("columns can't be rows")
	}
	if col[0] != row[0] {
		panic("bad intersection")
	}

	common.Log.Info("validTable: w=%d h=%d cells=%d", w, h, len(cells))

	table := newTextTable(w, h)
	for x, cell := range row {
		table.put(x, 0, cell)
	}
	for y, cell := range col {
		table.put(0, y, cell)
	}
	fontsize := table.fontsize()
	for i, cell := range cells {
		y := col.getAlignedIndex(getYUr, fontsize*lineDepthR, cell)
		x := row.getAlignedIndex(getXLl, fontsize*maxIntraReadingGapR, cell)
		if x < 0 || y < 0 {
			common.Log.Error("bad element: x=%d y=%d cell=%s", x, y, cell.String())
			return nil
		}
		fmt.Printf("%4d: y=%d x=%d %q\n", i, y, x, truncate(cell.text(), 50))
		table.put(x, y, cell)
		fontsize = table.fontsize()
	}

	// if !table.isDense(table.w, table.h) {
	// 	return nil
	// }
	w, h = table.maxDense()
	common.Log.Info("maxDense: w=%d h=%d", w, h)
	if w < 0 {
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
		fmt.Printf("%d: isDense w=%d h=%d dense=%5t %s\n", i, w, h, dense, reason)
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

func (cells cellList) getAlignedIndex(get getter, delta float64, targetCell *textPara) int {
	for i, cell := range cells {
		if parasAligned(get, delta, targetCell, cell) {
			return i
		}
	}
	return -1
}

func sortContents(get getter, reverse bool, cols []cellList) {
	for _, cells := range cols {
		sort.Slice(cells, func(i, j int) bool {
			ci, cj := cells[i], cells[j]
			if reverse {
				return get(ci) > get(cj)
			}
			return get(ci) < get(cj)
		})
	}
}

// findGetterCandidates returns list of elements of `cells` that are within `delta` for attribute `get`.
func (cells cellList) findGetterCandidates(get getter, deltaR float64, reverse bool) []cellList {
	delta := cells.fontsize() * deltaR
	xIndex := cells.makeIndex(getXLl)
	var columns []cellList
	addCol := func(col cellList) {
		if len(col) > 1 {
			columns = append(columns, col)
		}
	}
	for i0, idx0 := range xIndex[:len(xIndex)-1] {
		cell0 := cells[idx0]
		col := cellList{cell0}
		for _, idx := range xIndex[i0+1:] {
			cell := cells[idx]
			if getXLl(cell) > get(cell0)+delta {
				addCol(col)
				col = cellList{cell}
			} else if parasAligned(get, delta, cell0, cell) {
				col = append(col, cell)
			}
		}
		addCol(col)
	}
	sort.Slice(columns, func(i, j int) bool {
		ci, cj := columns[i], columns[j]
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

// makeIndex returns an indexes over cells on the `Llx` and `Ury `attributes.
func (cells cellList) xyIndexes() ([]int, []int) {
	xIndex := cells.makeIndex(getXLl)
	yIndex := cells.makeIndex(getYUr)
	return xIndex, yIndex
}

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
	gettersX = []getter{getXCe, getXLl, getXUr}
	// gettersX get the y-center, bottom and top of cells.
	gettersY = []getter{getYCe, getYLl, getYUr}
)

func getXCe(para *textPara) float64 { return 0.5 * (para.Llx + para.Urx) }
func getXLl(para *textPara) float64 { return para.Llx }
func getXUr(para *textPara) float64 { return para.Urx }
func getYCe(para *textPara) float64 { return 0.5 * (para.Lly + para.Ury) }
func getYLl(para *textPara) float64 { return para.Lly }
func getYUr(para *textPara) float64 { return para.Ury }
func getTop(para *textPara) float64 { return -para.Ury }

func (cells cellList) log(title string) {
	paraList(cells).log(title)
}

// logTables logs the contents of `tables`.
func logTables(tables []*textTable, title string) {
	common.Log.Info("%8s: %d tables =======!!!!!!!!=====", title, len(tables))
	for i, t := range tables {
		t.log(fmt.Sprintf("%s-%02d", title, i))
	}
}

// log logs the contents of `table`.
func (t *textTable) log(title string) {
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
	return parts

}

// log logs the contents of `paras`.
func (paras paraList) log(title string) {
	common.Log.Info("%8s: %d paras =======-------=======", title, len(paras))
	for i, para := range paras {
		if para == nil {
			continue
		}
		text := para.text()
		tabl := "  "
		if para.table != nil {
			tabl = fmt.Sprintf("[%dx%d]", para.table.w, para.table.h)
		}
		fmt.Printf("%4d: %6.2f %s %q\n", i, para.PdfRectangle, tabl, truncate(text, 50))
		if len(text) == 0 {
			panic("empty")
		}
		if para.table != nil && len(para.table.cells) == 0 {
			panic(para)
		}
	}
}
