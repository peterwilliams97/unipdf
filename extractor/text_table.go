/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/unidoc/unipdf/v3/common"
	"github.com/unidoc/unipdf/v3/model"
)

// textTable is a table of `w` x `h` textPara cells.
type textTable struct {
	model.PdfRectangle                      // Bounding rectangle.
	w, h               int                  // w=number of columns. h=number of rows.
	cells              map[uint64]*textPara // The cells
}

// String returns a description of `t`.
func (t *textTable) String() string {
	return fmt.Sprintf("%d x %d", t.w, t.h)
}

// bbox makes textLine implement the `bounded` interface.
func (t *textTable) bbox() model.PdfRectangle {
	return t.PdfRectangle
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
	return paras.applyTables(tables)
}

// findTables returns all the tables  in `paras`.
func (paras paraList) findTables() []*textTable {
	paras.addNeighbours()
	// Pre-sort by reading direction then depth
	sort.Slice(paras, func(i, j int) bool {
		return diffReadingDepth(paras[i], paras[j]) < 0
	})

	var tables []*textTable
	for _, para := range paras {
		if para.taken() || para.Width() == 0 {
			continue
		}
		var table *textTable
		if !advancedTables {
			table = para.isAtom()
			if table == nil {
				continue
			}
			table.growTable()
			if table.w*table.h < minTableParas {
				continue
			}
			table.markCells()
		} else {
			candidate := para.sparseCandidate()
			if candidate == nil {
				continue
			}
			candidate.markCells()
			table = candidate.toTable()
		}

		table.log("fully grown")
		tables = append(tables, table)

	}
	return tables
}

// isAtom atempts to build the smallest possible table fragment of 2 x 2 cells.
// If a table can be built then it is returned. Otherwise nil is returned.
// The smallest possible table is
//   a b
//   c d
// where
//   a is `para`.
//   b is immediately to the right of a and overlaps it in the y axis.
//   c is immediately below a and overlaps it in the x axis.
//   d is immediately to the right of c and overlaps it in the y axis and
//        immediately below b and ooverlaps it in the s axis.
//   None of a, b, c or d are cells in existing tables.
func (para *textPara) isAtom() *textTable {
	a := para
	b := para.right
	c := para.below
	if !(b != nil && !b.isCell && c != nil && !c.isCell) {
		return nil
	}
	d := b.below
	if !(d != nil && !d.isCell && d == c.right) {
		return nil
	}
	return newTableAtom(a, b, c, d)
}

func (para *textPara) taken() bool {
	return para == nil || para.isCell
}

// newTable returns a table containing the a, b, c, d elements from isAtom().
func newTableAtom(a, b, c, d *textPara) *textTable {
	t := &textTable{w: 2, h: 2, cells: map[uint64]*textPara{}}
	t.put(0, 0, a)
	t.put(1, 0, b)
	t.put(0, 1, c)
	t.put(1, 1, d)
	return t
}

// growTable grows `t` to the largest w x h it can while remaining a valid table.
// It repeatedly tries to extend by one row and/or column
//    - down and right, then
//    - down, then
//    - right.
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

	for {
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
	}
}

// getDown returns the row of cells below `t` if they are a valid extension to `t` or nil if they aren't.
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

// getRight returns the column of cells to the right `t` if they are a valid extension to `t` or nil
// if they aren't.
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

func (t *textTable) row(y int) paraList {
	cells := make(paraList, t.w)
	for x := 0; x < t.w; x++ {
		cells[x] = t.get(x, y)
	}
	return cells
}

func (t *textTable) column(x int) paraList {
	cells := make(paraList, t.h)
	for y := 0; y < t.h; y++ {
		cells[x] = t.get(x, y)
	}
	return cells
}

// sparseCandidate returns the biggest sparse table it can grow
func (para *textPara) sparseCandidate() *tableCandidate {
	a := para
	b := para.right
	c := para.below
	if b.taken() || c.taken() {
		return nil
	}
	d := b.below
	if d.taken() || d != c.right {
		d = nil
	}

	// Look for top and left of up to 4 elements
	top := paraList{a, b}
	left := paraList{a, c}
	for len(top) < (2*tableCircumf)/3 {
		b := top[len(top)-1]
		if b.right.taken() {
			break
		}
		top = append(top, b.right)
	}
	for len(left) < (2*tableCircumf)/3 {
		c := left[len(left)-1]
		if c.below.taken() {
			break
		}
		left = append(left, c.below)
	}

	w, h := len(top), len(left)
	if w+h < tableCircumf {
		return nil
	}

	w, h = 2, 2
	left = paraList{a, c}    // left[:h]
	top = paraList{a, b}     // top[:w]
	right := paraList{b, c}  // left[:]
	bottom := paraList{c, b} //top[:]
	// right[0] = top[1]
	// bottom[0] = left[1]
	occupied := 3
	if d != nil {
		occupied++
		right[1] = d
		bottom[1] = d
	}
	candidate := &tableCandidate{
		w:        w,
		h:        h,
		top:      top,
		left:     left,
		right:    right,
		bottom:   bottom,
		occupied: occupied,
	}
	candidate.validate()
	candidate.log("atom")
	candidates := candidate.growTableSparse()
	best := candidates.best()
	if best == nil {
		return nil
	}
	if best.w+best.h < tableCircumf {
		panic(best)
		return nil
	}
	return best
}

// func (t *tableCandidate) valid() bool {
// 	if t.w+t.h < tableCircumf {
// 		return false
// 	}
// 	rowCounts := make([]int, t.h)
// 	colCounts := make([]int, t.w)
// 	for y := 0; y < t.y; y++ {
// 		for x := 0; x < t.w; x++ {
// 			cell := t.toTable().getDown()
// 		}
// 	}
// }

// growTable grows `t` to the largest w x h it can while remaining a valid table.
// It repeatedly tries to extend by one row and/or column
//    - down and right, then
//    - down, then
//    - right.
func (t *tableCandidate) growTableSparse() candidateList {
	candidates := candidateList{t}
	for goingDown, goingRight := true, true; goingDown || goingRight; {
		common.Log.Info("-growTableSparse: %d candidates goingDown=%t goingRight=%t",
			len(candidates), goingDown, goingRight)
		for _, c := range candidates {
			c.validate()
			if goingDown {
				down := c.growDown()
				if down != nil {
					down.validate()
					if goingRight {
						downRight := down.growRight()
						if downRight != nil {
							downRight.validate()
							downRight.log("downRight")
							candidates.add(downRight)
							continue
						}
					} else {
						candidates.add(down)
						down.log("down")
					}
				} else {
					common.Log.Info("done down")
					goingDown = false
				}
			}
			if goingRight {
				right := c.growRight()
				if right != nil {
					right.validate()
					right.log("right")
					candidates.add(right)
				} else {
					common.Log.Info("done right")
					goingRight = false
				}
			}
		}
		common.Log.Info("+growTableSparse: %d candidates goingDown=%t goingRight=%t",
			len(candidates), goingDown, goingRight)
	}
	return candidates
}

func (cl *candidateList) add(candidate *tableCandidate) {
	*cl = append(*cl, candidate)
	w, h := 0, 0
	for _, c := range *cl {
		if c.w > w {
			w = c.w
		}
		if c.h > h {
			h = c.h
		}
	}
	var bestW, bestH candidateList
	for _, c := range *cl {
		if c.w == w {
			bestW = append(bestW, c)
		}
		if c.h == h {
			bestH = append(bestH, c)
		}
	}
	cellW := bestW[0]
	cellH := bestH[0]
	for _, c := range bestH[1:] {
		if c.w > cellH.w {
			cellH = c
		}
	}
	for _, c := range bestW[1:] {
		if c.h > cellW.h {
			cellW = c
		}
	}
	var survivors candidateList
	if cellH == cellW {
		survivors = candidateList{cellW}
	} else {
		survivors = candidateList{cellW, cellH}
	}
	*cl = survivors
	common.Log.Info("add ----------- %d survivors", len(survivors))
	for i, c := range survivors {
		fmt.Printf("%4d: %d x %d\n", i, c.w, c.h)
	}
}

func (cl *candidateList) best() *tableCandidate {
	best := (*cl)[0]
	for _, c := range (*cl)[1:] {
		betterW := c.w > best.w
		betterH := c.h > best.h
		if betterW && (betterH || c.h > best.h) {
			best = c
		}
		if betterH && (betterW || c.w > best.w) {
			best = c
		}
	}
	return best
}

type candidateList []*tableCandidate

// tableCandidate is a candidate for a new sparse table.
type tableCandidate struct {
	w, h     int      // Width and height of table in cells
	top      paraList // Top row of table. This must be dense.
	left     paraList // Left column of table. This must be dense.
	right    paraList // Right-most occupied calls in each row.
	bottom   paraList // Bottom-most occupied calls in each column.
	occupied int      // Number of occupied cells
}

func (t *tableCandidate) String() string {
	return fmt.Sprintf("%d x %d: left=%d right=%d top=%d bottom=%d", t.w, t.h,
		len(t.left), len(t.right), len(t.top), len(t.bottom))
}

// growDown attempts to grow `t` one row down.
func (t *tableCandidate) growDown() *tableCandidate {
	cell0 := t.bottom[0].below
	if cell0 == nil {
		return nil
	}
	cells := make(paraList, t.w)
	cells[0] = cell0
	n := 0
	for x := 1; x < t.w; x++ {
		cell := t.bottom[x].below
		if cell != cell0.right {
			continue
		}
		if cell == nil {
			break
		}

		cells[x] = cell
		cell0 = cell
		n++
	}
	bottom := t.bottom.update(cells)
	left := append(t.left, bottom[0])
	right := append(t.right, bottom[t.w-1])
	c := &tableCandidate{
		w:        t.w,
		h:        t.h + 1,
		left:     left,
		top:      t.top,
		right:    right,
		bottom:   bottom,
		occupied: t.occupied + n,
	}
	c.validate()
	return c
}

// getRight attempts to grow `t` one column to the right.
func (t *tableCandidate) growRight() *tableCandidate {
	cell0 := t.right[0].right
	if cell0 == nil {
		return nil
	}
	cells := make(paraList, t.h)
	cells[0] = cell0
	n := 0
	for y := 1; y < t.h; y++ {
		cell := t.right[y].right
		if cell != cell0.below {
			continue
		}
		if cell == nil {
			break
		}
		cells[y] = cell
		cell0 = cell
		n++
	}
	right := t.right.update(cells)
	top := append(t.top, right[0])
	bottom := append(t.bottom, cell0)
	c := &tableCandidate{
		w:        t.w + 1,
		h:        t.h,
		left:     t.left,
		top:      top,
		right:    right,
		bottom:   bottom,
		occupied: t.occupied + n,
	}
	c.validate()
	return c
}

func (t *tableCandidate) markCells() {
	t.validate()
	for x, cell0 := range t.top {
		cell := cell0
		for {
			cell.isCell = true
			if cell == t.bottom[x] {
				break
			}
			cell = cell.below
		}
	}
}

func (t *tableCandidate) validate() {
	if len(t.top) != t.w {
		panic(t)
	}
	if len(t.bottom) != t.w {
		panic(t)
	}
	if len(t.left) != t.h {
		panic(t)
	}
	if len(t.right) != t.h {
		panic(t)
	}
	for x, cell0 := range t.top {
		cell := cell0
		for y := 0; ; y++ {
			if cell == nil {
				panic(fmt.Errorf("x=%d y=%d t=%s", x, y, t.String()))
			}
			if cell == t.bottom[x] {
				break
			}
			cell = cell.below
		}
	}
}

func (t *tableCandidate) toTable() *textTable {
	table := textTable{w: t.w, h: t.h, cells: map[uint64]*textPara{}}
	common.Log.Info("toTable: %s top=%d left=%d", table.String(), len(t.top), len(t.left))
	t.validate()

	for y, cell := range t.top {
		fmt.Printf("%4d: %p %s\n", y, cell, cell)
	}

	left := t.left
	cellY := map[*textPara]int{}
	for y, cell := range t.left {
		fmt.Printf("%4d: %p %s\n", y, cell, cell)
		if _, ok := cellY[cell]; ok {
			panic("duplicate")
		}
		cellY[cell] = y
	}

	complete := map[uint64]bool{}

	for x, cell := range t.top {
		fmt.Printf("    x=%d\n\t    top=%s\n\t bottom=%s\n", x, cell, t.bottom[x])
		c := cell
		if y := cellY[c]; y != 0 {
			panic("first index")
		}
		for {
			y := cellY[c]
			fmt.Printf("%6d %2d: %s\n", x, y, c)
			{
				i := cellIndex(x, y)
				if _, ok := complete[i]; ok {
					panic(fmt.Errorf("Duplicate x=%d y=%d %s", x, y, cell))
				}
				complete[i] = true
			}
			table.put(x, y, c)

			left[y] = c.right
			if c.right != nil {
				cellY[c.right] = y
			}

			if c == t.bottom[x] {
				fmt.Printf("\tbottom\n")
				break
			}
			c = c.below
		}
	}
	return &table
}

func (t *tableCandidate) log(title string) {
	return
	common.Log.Info("XXXX: %s: %d x %d top=%d left=%d", title, t.w, t.h, len(t.top), len(t.left))
	t.validate()
	cellIndex := map[*textPara]int{}
	if true {
		left := t.left
		for i, cell := range left {
			cellIndex[cell] = i
		}
		for x, cell := range t.top {
			c := cell
			y := cellIndex[c.left]
			if y != 0 {
				panic("first index")
			}
			for {
				y := cellIndex[c.left]
				left[y] = c
				fmt.Printf("%6d %2d: %s\n", x, y, c)
				if c == t.bottom[x] {
					break
				}
				c = c.below
			}
		}
	} else {
		top := t.top
		for i, cell := range top {
			cellIndex[cell] = i
		}
		for y, cell := range t.left {
			c := cell
			x := cellIndex[c.above]
			if x != 0 {
				panic("first index")
			}
			for {
				x := cellIndex[c.left]
				top[x] = c
				fmt.Printf("%6d %2d: %s\n", x, y, c)
				if c == t.right[y] {
					break
				}
				c = c.right
			}
		}
	}
}

func (paras paraList) update(cells paraList) paraList {
	if len(paras) != len(cells) {
		panic("negatory")
	}
	updated := make(paraList, len(paras))
	copy(updated, paras)
	for i, c := range cells {
		if c != nil {
			updated[i] = c
		}
	}
	return updated
}

// applyTables replaces the paras that are cells in `tables` with paras containing the tables in
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

// newTablePara returns a textPara containing `t`.
func (t *textTable) newTablePara() *textPara {
	bbox := t.computeBbox()
	return &textPara{
		PdfRectangle: bbox,
		eBBox:        bbox,
		table:        t,
	}
}

// computeBbox computes and returns the bounding box of `t`.
func (t *textTable) computeBbox() model.PdfRectangle {
	r := t.get(0, 0).PdfRectangle
	for x := 1; x < t.w; x++ {
		r = rectUnion(r, t.get(x, 0).PdfRectangle)
	}
	for y := 1; y < t.h; y++ {
		for x := 0; x < t.w; x++ {
			cell := t.get(x, y)
			if cell != nil {
				r = rectUnion(r, cell.PdfRectangle)
			}
		}
	}
	return r
}

// toTextTable returns the TextTable corresponding to `t`.
func (t *textTable) toTextTable() TextTable {
	common.Log.Info("toTextTable: %d x %d", t.w, t.h)
	cells := make([][]TableCell, t.h)
	for y := 0; y < t.h; y++ {
		cells[y] = make([]TableCell, t.w)
		for x := 0; x < t.w; x++ {
			c := t.get(x, y)
			fmt.Printf("%4d %2d: %s\n", x, y, c)
			if c == nil {
				continue
			}
			cells[y][x].Text = c.text()
			offset := 0
			cells[y][x].Marks.marks = c.toTextMarks(&offset)
		}
	}
	return TextTable{W: t.w, H: t.h, Cells: cells}
}

// get returns the cell at `x`, `y`.
func (t *textTable) get(x, y int) *textPara {
	return t.cells[cellIndex(x, y)]
}

// put sets the cell at `x`, `y` to `cell`.
func (t *textTable) put(x, y int, cell *textPara) {
	t.cells[cellIndex(x, y)] = cell
}

// cellIndex returns a number that will be different for different `x` and `y` for any table found
// in a PDF which will less than 2^32 wide and hight.
func cellIndex(x, y int) uint64 {
	return uint64(x)*0x1000000 + uint64(y)
}

func (t *textTable) log(title string) {
	if !verboseTable {
		return
	}
	common.Log.Info("~~~ %s: %d x %d\n      %6.2f", title,
		t.w, t.h, t.PdfRectangle)
	for y := 0; y < t.h; y++ {
		for x := 0; x < t.w; x++ {
			p := t.get(x, y)
			fmt.Printf("%4d %2d: %6.2f %q %d\n", x, y, p.PdfRectangle, truncate(p.text(), 50), utf8.RuneCountInString(p.text()))
		}
	}
	// panic("table")
}
