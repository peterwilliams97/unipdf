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
	cells cellList
}

func (t *textTable) String() string {
	return fmt.Sprintf("[%dx%d] %6.2f", t.w, t.h, t.PdfRectangle)
}

func (t textTable) bbox() model.PdfRectangle {
	return t.PdfRectangle
}

type cellList paraList

const DBL_MIN, DBL_MAX = -1.0e10, +1.0e10

// extractTables converts the`paras` that are table cells to tables containing those cells.
func (paras paraList) extractTables() paraList {
	common.Log.Debug("extractTables=%d ===========x=============", len(paras))
	if len(paras) < 4 {
		return nil
	}
	showParas := paras.log

	tables := paras.extractTableAtoms()
	logTables(tables, "table atoms")
	tables = combineTables(tables)
	logTables(tables, "table molecules")
	// if len(tables) == 0 {panic("NO TABLES")}
	showParas("tables extracted")
	paras = paras.applyTables(tables)
	showParas("tables applied")
	paras = paras.trimTables()
	showParas("tables trimmed")

	return paras
}

func (paras paraList) trimTables() paraList {
	var recycledParas paraList
	seen := map[*textPara]bool{}
	for _, para := range paras {
		for _, p := range paras {
			if p == para {
				continue
			}
			table := para.table
			if table != nil && overlapped(table, p) {
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
	}

	for _, p := range paras {
		if p.table != nil && p.table.cells == nil {
			continue
		}
		recycledParas = append(recycledParas, p)
	}
	return recycledParas
}

func (paras paraList) applyTables(tables []textTable) paraList {
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
	return tabled
}

// extractTableAtome returns all the 2x2 table candidates in `paras`.
func (paras paraList) extractTableAtoms() []textTable {
	// Pre-sort by reading direction then depth
	sort.Slice(paras, func(i, j int) bool {
		return diffReadingDepth(paras[i], paras[j]) < 0
	})

	var llx0, lly0, llx1, lly1 float64
	var tables []textTable

	for i1, para1 := range paras {
		llx0, lly0 = DBL_MAX, DBL_MIN
		llx1, lly1 = DBL_MAX, DBL_MIN

		// Build a table atom of 4 cells
		//   0 1
		//   2 3
		// where
		//   0 is `para1`
		//   1 is on the right of 0 and overlaps with 0 in y axis
		//   2 is under 0 and overlaps with 0 in x axis
		//   3 is under 1 and on the right of 1 and closest to 0
		cells := make(cellList, 4)
		cells[0] = para1

		for _, para2 := range paras {
			if para1 == para2 {
				continue
			}
			if yOverlap(para1, para2) && toRight(para2, para1) && para2.Llx < llx0 {
				llx0 = para2.Llx
				cells[1] = para2
			} else if xOverlap(para1, para2) && below(para2, para1) && para2.Ury > lly0 {
				lly0 = para2.Ury
				cells[2] = para2
			} else if toRight(para2, para1) && para2.Llx < llx1 && below(para2, para1) && para2.Ury > lly1 {
				llx1 = para2.Llx
				lly1 = para2.Ury
				cells[3] = para2
			}
		}
		// Do we have a table atom?
		if !(cells[1] != nil && cells[2] != nil && cells[3] != nil) {
			continue
		}
		// cells.log(fmt.Sprintf("table atom i1=%d", i1))

		// 1 cannot overlap with 2 in x and y
		// 3 cannot overlap with 2 in x and with 1 in y
		// 3 has to overlap with 2 in y and with 1 in x
		if (xOverlap(cells[2], cells[3]) || yOverlap(cells[1], cells[3]) ||
			xOverlap(cells[1], cells[2]) || yOverlap(cells[1], cells[2])) ||
			!(xOverlap(cells[1], cells[3]) && yOverlap(cells[2], cells[3])) {
			continue
		}
		// common.Log.Info("OVERLAP A) i1=%d", i1)

		scoreX := cells.aligned2x2X(cells.fontsize() * maxIntraReadingGapR)
		scoreY := cells.aligned2x2Y(cells.fontsize() * lineDepthR)
		common.Log.Info("OVERLAP C) i1=%d scoreX=%d scoreY=%d", i1, scoreX, scoreY)

		// are blocks aligned in x and y ?
		if scoreX > 0 && scoreY > 0 {
			table := newTable(cells, 2, 2)
			tables = append(tables, table)
			table.log("New textTable")
		}
	}
	return tables
}

// 0 1
// 2 3
// A B
// C
// Extensions:
//   A[1] == B[0] right
//   A[2] == C[0] down
func combineTables(tables []textTable) []textTable {
	tablesY := combineTablesY(tables)
	tablesX := combineTablesX(tablesY)
	return tablesX
}

func combineTablesY(tables []textTable) []textTable {
	sort.Slice(tables, func(i, j int) bool { return tables[i].Ury > tables[j].Ury })
	removed := map[int]bool{}

	var combinedTables []textTable
	common.Log.Info("combineTablesY ------------------\n\t ------------------")
	for i1, t1 := range tables {
		if _, ok := removed[i1]; ok {
			continue
		}
		fontsize := t1.cells.fontsize()
		c1 := t1.corners()
		var combo *textTable
		for i2, t2 := range tables {
			if _, ok := removed[i2]; ok {
				continue
			}
			if t1.w != t2.w {
				continue
			}
			c2 := t2.corners()
			if c1[2] != c2[0] {
				continue
			}
			// common.Log.Info("Comparing i1=%d i2=%d", i1, i2)
			// t1.log("t1")
			// t2.log("t2")
			cells := cellList{
				c1[0], c1[1],
				c2[2], c2[3],
			}
			alX := cells.aligned2x2X(fontsize * maxIntraReadingGapR)
			alY := cells.aligned2x2Y(fontsize * lineDepthR)
			common.Log.Info("alX=%d alY=%d", alX, alY)
			if !(alX > 0 && alY > 0) {
				if combo != nil {
					common.Log.Info("BREAK: i1=%d i2=%d", i1, i2)
					t1.log("t1")
					t2.log("t2")
					combinedTables = append(combinedTables, *combo)
				}
				combo = nil
				continue
			}
			if combo == nil {
				combo = &t1
				removed[i1] = true
			}

			w := combo.w
			h := combo.h + t2.h - 1
			common.Log.Info("COMBINE! %dx%d i1=%d i2=%d", w, h, i1, i2)
			combined := make(cellList, w*h)
			for y := 0; y < t1.h; y++ {
				for x := 0; x < w; x++ {
					combined[y*w+x] = combo.cells[y*w+x]
				}
			}
			for y := 1; y < t2.h; y++ {
				yy := y + combo.h - 1
				for x := 0; x < w; x++ {
					combined[yy*w+x] = t2.cells[y*w+x]
				}
			}
			combo.cells = combined
			combo.h = h
			combo.PdfRectangle = rectUnion(combo.PdfRectangle, t2.PdfRectangle)
			combo.log("combo")
			removed[i2] = true
			fontsize = combo.cells.fontsize()
			c1 = combo.corners()
		}
		if combo != nil {
			combinedTables = append(combinedTables, *combo)
		}
	}

	common.Log.Info("combineTablesY a: combinedTables=%d", len(combinedTables))
	for i, t := range tables {
		if _, ok := removed[i]; ok {
			continue
		}
		combinedTables = append(combinedTables, t)
	}
	common.Log.Info("combineTablesY b: combinedTables=%d", len(combinedTables))

	return combinedTables
}

func combineTablesX(tables []textTable) []textTable {
	sort.Slice(tables, func(i, j int) bool {
		ti, tj := tables[i], tables[j]
		if ti.h != tj.h {
			return ti.h > tj.h
		}
		return ti.Llx < tj.Llx
	})
	removed := map[int]bool{}
	for i1, t1 := range tables {
		if _, ok := removed[i1]; ok {
			continue
		}
		fontsize := t1.cells.fontsize()

		t1right := t1.column(t1.w - 1)
		t1Map := t1right.cellSet()
		overlapTable := map[int]*textTable{}
		t2Width := 0
		for i2, t2 := range tables {
			if _, ok := removed[i2]; ok {
				continue
			}
			if i2 <= i1 {
				continue
			}
			// t1 is taller than t2
			t2TL := t2.cells[0]
			if _, ok := t1Map[t2TL]; !ok {
				continue
			}
			t2left := t2.column(0)
			if t2TL != t2left[0] {
				panic("t's don't agree")
			}
			v0, v1 := t1right.overlapRange(t2left)
			if v0 < 0 {
				continue
			}
			common.Log.Info("v0=%d v1=%d t1right=%d t2left=%d", v0, v1, len(t1right), len(t2left))
			aligned := true
			for k := 0; k < v1-v0; k++ {
				t1c := t1right[v0+k]
				t2c := t2left[k]
				cells := cellList{t1c, t2c}
				if cells.alignedY(fontsize*lineDepthR) == 0 {
					aligned = false
					break
				}
			}
			if aligned {
				overlapTable[v0] = &t2
				if t2.w > t2Width {
					t2Width = t2.w
				}
				removed[i2] = true
			}
		}

		if len(overlapTable) > 0 {
			w := t1.w + t2Width
			h := t1.h
			combined := textTable{
				w:     w,
				h:     h,
				cells: make(cellList, w*h),
			}
			combined.insertAt(0, 0, &t1)
			for y, t := range overlapTable {
				combined.insertAt(t1.w-1, y, t)
			}

			fontsize = combined.cells.fontsize()
		}
	}
	var reduced []textTable
	for i, t := range tables {
		if _, ok := removed[i]; ok {
			continue
		}
		reduced = append(reduced, t)
	}
	return reduced
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

func (c *textTable) corners() paraList {
	w, h := c.w, c.h
	if w == 0 || h == 0 {
		panic(c)
	}
	cnrs := paraList{
		c.cells[0],
		c.cells[w-1],
		c.cells[w*(h-1)],
		c.cells[w*h-1],
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

func newTable(cells cellList, w, h int) textTable {
	if w == 0 || h == 0 {
		panic("emprty")
	}
	for i0, c0 := range cells {
		for _, c1 := range cells[:i0] {
			if c0.serial == c1.serial {
				panic("dup")
			}
		}
	}
	rect := cells[0].PdfRectangle
	for _, c := range cells[1:] {
		rect = rectUnion(rect, c.PdfRectangle)
	}
	return textTable{
		PdfRectangle: rect,
		w:            w,
		h:            h,
		cells:        cells,
	}
}

func (table textTable) newTablePara() *textPara {
	cells := table.cells
	sort.Slice(cells, func(i, j int) bool { return diffDepthReading(cells[i], cells[j]) < 0 })
	table.cells = cells
	para := textPara{
		serial:       serial.para,
		PdfRectangle: table.PdfRectangle,
		eBBox:        table.PdfRectangle,
		table:        &table,
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
	w := minInt(table.w, x-t.w)
	h := minInt(table.h, x-t.h)
	if w < 1 || h < 1 {
		return
	}
	x0, y0 := x, y
	for y := 0; y < h; y++ {
		yy := y0 + y
		for x := 0; x < w; x++ {
			xx := x0 + x
			t.cells[yy*t.w+xx] = table.cells[y*table.w+x]
		}
	}
}

// row returns the (0-offset) `y`th row in `t`.
func (t textTable) row(y int) cellList {
	if !(0 <= y && y < t.h) {
		panic(fmt.Errorf("y=%d is an invalid row for %s", y, t))
	}
	cells := make(cellList, t.w)
	for x := 0; x < t.w; x++ {
		cells[x] = t.cells[y*t.w+x]
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
		cells[y] = t.cells[y*t.w+x]
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
	var cells [][]string
	for y := 0; y < t.h; y++ {
		var row []string
		for x := 0; x < t.w; x++ {
			row = append(row, t.cells[y*t.w+x].text())
		}
		cells = append(cells, row)
	}
	return TextTable{
		W:     t.w,
		H:     t.h,
		Cells: cells,
	}
}

// logTables logs the contents of `tables`.
func logTables(tables []textTable, title string) {
	common.Log.Info("%8s: %d tables =======!!!!!!!!=====", title, len(tables))
	for i, t := range tables {
		t.log(fmt.Sprintf("%s-%02d", title, i))
	}

}

// log logs the contents of `table`.
func (table textTable) log(title string) {
	common.Log.Info("%8s: %s: %2d x %2d %6.2f =======//////////=====\n"+
		"      %6.2f", title, fileLine(1, false),
		table.w, table.h, table.PdfRectangle, table.PdfRectangle)
	for i, p := range table.cells {
		fmt.Printf("%4d: %6.2f %q\n", i, p.PdfRectangle, truncate(p.text(), 50))
	}
}

// log logs the contents of `paras`.
func (paras paraList) log(title string) {
	common.Log.Info("%8s: %d paras =======-------=======", title, len(paras))
	for i, para := range paras {
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

func (cells cellList) log(title string) {
	paraList(cells).log(title)
}
