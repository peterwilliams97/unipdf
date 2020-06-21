/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"fmt"
	"sort"

	"github.com/unidoc/unipdf/v3/common"
	"github.com/unidoc/unipdf/v3/model"
)

type textTable struct {
	model.PdfRectangle
	w, h  int
	cells map[uint64]*textPara
}

// extractTables converts the`paras` that are table cells to tables containing those cells.
func (paras paraList) extractTables() paraList {
	if verboseTable {
		common.Log.Debug("extractTables=%d ===========x=============", len(paras))
	}
	if len(paras) < minTableParas {
		return paras
	}

	tables := paras.findTables()

	if verboseTable {
		common.Log.Info("combined tables %d ================", len(tables))
		for i, t := range tables {
			t.log(fmt.Sprintf("combined %d", i))
		}
	}

	paras = paras.applyTables(tables)

	return paras
}

// findTables returns all the 2x2 table candidateds in `paras`.
func (paras paraList) findTables() []*textTable {
	paras.addNeighbours()
	// Pre-sort by reading direction then depth
	sort.Slice(paras, func(i, j int) bool {
		return diffReadingDepth(paras[i], paras[j]) < 0
	})

	var tables []*textTable
	for _, para := range paras {
		if para.isCell {
			continue
		}
		table := para.isAtom()
		if table == nil {
			continue
		}

		table.growTable()
		if table.w*table.h < minTableParas {
			continue
		}
		table.markCells()
		table.log("grown")
		tables = append(tables, table)

	}
	return tables
}

// Build a table fragment of 4 cells
//   0 1
//   2 3
// where
//   0 is `para1`
//   1 is on the right of 0 and overlaps with 0 in y axis
//   2 is under 0 and overlaps with 0 in x axis
//   3 is under 1 and on the right of 1 and closest to 0
func (para *textPara) isAtom() *textTable {
	a := para
	b := para.right
	c := para.below
	if b != nil && !b.isCell && c != nil && !c.isCell {
		d := b.below
		if d != nil && !d.isCell && d == c.right {
			return newTable(a, b, c, d, 2, 2)
		}
	}
	return nil
}

func newTable(a, b, c, d *textPara, w, h int) *textTable {
	t := &textTable{w: w, h: h, cells: map[uint64]*textPara{}}
	t.put(0, 0, a)
	t.put(1, 0, b)
	t.put(0, 1, c)
	t.put(1, 1, d)
	return t
}

func (t *textTable) growTable() {
	growDown := func(down paraList) {
		t.h++
		for x := 0; x < t.w; x++ {
			cell := down[x]
			t.put(x, t.h-1, cell)
		}
	}
	growRight := func(right paraList) {
		t.w++
		for y := 0; y < t.h; y++ {
			cell := right[y]
			t.put(t.w-1, y, cell)
		}
	}

	for i := 0; ; i++ {
		changed := false
		down := t.getDown()
		right := t.getRight()
		if down != nil && right != nil {
			downRight := down[len(down)-1]
			if downRight != nil && !downRight.isCell && downRight == right[len(right)-1] {
				growDown(down)
				growRight(right)
				t.put(t.w-1, t.h-1, downRight)
				changed = true
			}
		}
		if !changed && down != nil {
			growDown(down)
			changed = true
		}
		if !changed && right != nil {
			growRight(right)
			changed = true
		}
		if !changed {
			break
		}
		if i > 10000 || t.w > 100 || t.h > 200 {
			err := fmt.Errorf("i=%d t=%s", i, t.String())
			panic(err)
		}
	}
}

func (t *textTable) getDown() paraList {
	cells := make(paraList, t.w)
	for x := 0; x < t.w; x++ {
		cell := t.get(x, t.h-1).below
		if cell == nil || cell.isCell {
			return nil
		}
		cells[x] = cell
	}
	for x := 0; x < t.w-1; x++ {
		if cells[x].right != cells[x+1] {
			return nil
		}
	}
	return cells
}

func (t *textTable) getRight() paraList {
	cells := make(paraList, t.h)
	for y := 0; y < t.h; y++ {
		cell := t.get(t.w-1, y).right
		if cell == nil || cell.isCell {
			return nil
		}
		cells[y] = cell
	}
	for y := 0; y < t.h-1; y++ {
		if cells[y].below != cells[y+1] {
			return nil
		}
	}
	return cells
}

// applyTables replaces the paras that re  cells in `tables` with paras containing the tables in
//`tables`. This, of course, reduces the number of paras.
func (paras paraList) applyTables(tables []*textTable) paraList {
	consumed := map[*textPara]struct{}{}
	var tabled paraList
	for _, table := range tables {
		for _, para := range table.cells {
			consumed[para] = struct{}{}
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

// markCells marks the paras that are cells in `t` with isCell=true so that the won't be considered
// as cell candidates for tables in the future.
func (t *textTable) markCells() {
	for y := 0; y < t.h; y++ {
		for x := 0; x < t.w; x++ {
			para := t.get(x, y)
			para.isCell = true
		}
	}
}

func (t *textTable) log(title string) {
	if !verboseTable {
		return
	}
	common.Log.Info("~~~ %s: %s: %d x %d\n      %6.2f", title, fileLine(1, false),
		t.w, t.h, t.PdfRectangle)
	for y := 0; y < t.h; y++ {
		for x := 0; x < t.w; x++ {
			p := t.get(x, y)
			fmt.Printf("%4d %2d: %6.2f %q\n", x, y, p.PdfRectangle, truncate(p.text(), 50))
		}
	}
}

func (t *textTable) newTablePara() *textPara {
	bbox := t.computeBbox()
	para := textPara{
		serial:       serial.para,
		PdfRectangle: bbox,
		eBBox:        bbox,
		table:        t,
	}
	t.log(fmt.Sprintf("newTablePara: serial=%d", para.serial))
	serial.para++
	return &para
}

func (t *textTable) computeBbox() model.PdfRectangle {
	r := t.get(0, 0).PdfRectangle
	for x := 1; x < t.w; x++ {
		r = rectUnion(r, t.get(x, 0).PdfRectangle)
	}
	for y := 1; y < t.h; y++ {
		for x := 0; x < t.w; x++ {
			r = rectUnion(r, t.get(x, y).PdfRectangle)
		}
	}
	return r
}

// toTextTable returns the TextTable corresponding to `t`.
func (t *textTable) toTextTable() TextTable {
	cells := make([][]string, t.h)
	for y := 0; y < t.h; y++ {
		cells[y] = make([]string, t.w)
		for x := 0; x < t.w; x++ {
			cells[y][x] = t.get(x, y).text()
		}
	}
	return TextTable{W: t.w, H: t.h, Cells: cells}
}

func cellIndex(x, y int) uint64 {
	return uint64(x)*0x1000000 + uint64(y)
}

func (t *textTable) get(x, y int) *textPara {
	return t.cells[cellIndex(x, y)]
}

func (t *textTable) put(x, y int, cell *textPara) {
	t.cells[cellIndex(x, y)] = cell
}

func (t *textTable) del(x, y int) {
	delete(t.cells, cellIndex(x, y))
}

func (t *textTable) bbox() model.PdfRectangle {
	return t.PdfRectangle
}

func (t *textTable) String() string {
	return fmt.Sprintf("%d x %d", t.w, t.h)
}
