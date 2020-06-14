/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"fmt"
	"math"
	"sort"

	"github.com/unidoc/unipdf/v3/internal/transform"
	"github.com/unidoc/unipdf/v3/model"
)

// rulingList is a list of rulings, possibly in a grid;
type rulingList []*ruling

// ruling is a vertical or horizontal line segment
type ruling struct {
	kind    rulingKind // Horizonal, vertical or other.
	primary float64    // x (y) coordinate for vertical (horizontal) rulings.
	lo      float64    // Lowest y (x) value for vertical (horizontal) rulings.
	hi      float64    // Highest y (x) value for vertical (horizontal) rulings.
}

type rulingKind int // Horizonal, vertical or other.

const (
	rulingNil rulingKind = iota
	rulingHorz
	rulingVert
)

// makeStrokeGrids returns the grids it finds in `strokes`.
func makeStrokeGrids(strokes []*subpath) []rulingList {
	granularize(strokes)
	var vecs rulingList
	for _, path := range strokes {
		if len(path.points) < 2 {
			continue
		}
		p1 := path.points[0]
		for _, p2 := range path.points[1:] {
			if v, ok := makeLineRuling(p1, p2); ok {
				vecs = append(vecs, v)
			}
			p1 = p2
		}
	}
	vecs = vecs.tidied("strokes")
	return vecs.toGrids()
}

// makeFillGrids returns the grids it finds in `fills`.
func makeFillGrids(fills []*subpath) []rulingList {
	granularize(fills)
	var vecs rulingList
	for _, path := range fills {
		if !path.isRectPath() {
			continue
		}
		if v, ok := path.makeRectRuling(); ok && v.kind != rulingNil {
			vecs = append(vecs, v)
		}
	}
	vecs = vecs.tidied("fills")
	return vecs.toGrids()
}

// lineRuling is a ruling that comes from a stroked path.
type lineRuling struct {
	kind   rulingKind
	p1, p2 transform.Point
}

// rectRuling is a ruling that comes from a filled path.
type rectRuling struct {
	kind rulingKind
	model.PdfRectangle
}

// asRuling returns `v` as a ruling.
func (v lineRuling) asRuling() *ruling {
	r := ruling{kind: v.kind}
	switch v.kind {
	case rulingVert:
		r.primary = v.xMean()
		r.lo = math.Min(v.p1.Y, v.p2.Y)
		r.hi = math.Max(v.p1.Y, v.p2.Y)
	case rulingHorz:
		r.primary = v.yMean()
		r.lo = math.Min(v.p1.X, v.p2.X)
		r.hi = math.Max(v.p1.X, v.p2.X)
	default:
		panic(fmt.Errorf("bad primary kind=%d", v.kind))
	}
	return &r
}

// makeLineRuling returns the line segment between `p1` and `p2` as a ruling it it is one.
func makeLineRuling(p1, p2 transform.Point) (*ruling, bool) {
	v := lineRuling{p1: p1, p2: p2, kind: lineKind(p1, p2)}
	if v.kind == rulingNil {
		return &ruling{}, false
	}
	return v.asRuling(), true
}

// asRuling returns `v` as a ruling.
func (v rectRuling) asRuling() *ruling {
	r := ruling{kind: v.kind}

	switch v.kind {
	case rulingVert:
		r.primary = 0.5 * (v.Llx + v.Urx)
		r.lo = v.Lly
		r.hi = v.Ury
	case rulingHorz:
		r.primary = 0.5 * (v.Lly + v.Ury)
		r.lo = v.Llx
		r.hi = v.Urx
	default:
		panic(fmt.Errorf("bad primary kind=%d", v.kind))
	}
	return &r
}

// makeRectRuling returns `path` as a ruling, if it is one.
func (path *subpath) makeRectRuling() (*ruling, bool) {
	points := path.points[:4]
	kinds := map[int]rulingKind{}
	for i, p1 := range points {
		p2 := path.points[(i+1)%4]
		kinds[i] = lineKind(p1, p2)
	}
	var verts, horzs []int
	for i, k := range kinds {
		switch k {
		case rulingHorz:
			horzs = append(horzs, i)
		case rulingVert:
			verts = append(verts, i)
		}
	}

	ok := (len(horzs) == 2 && len(verts) == 2) ||
		(len(horzs) == 2 && math.Abs(points[horzs[0]].Y-points[horzs[1]].Y) < 3) ||
		(len(verts) == 2 && math.Abs(points[verts[0]].X-points[verts[1]].X) < 3)

	if !ok {
		return &ruling{}, false
	}

	if len(verts) == 0 {
		for i, k := range kinds {
			if k != rulingHorz {
				verts = append(verts, i)
			}
		}
	}
	if len(horzs) == 0 {
		for i, k := range kinds {
			if k != rulingVert {
				horzs = append(horzs, i)
			}
		}
	}

	var left, right, top, bottom transform.Point
	if points[horzs[0]].Y > points[horzs[1]].Y {
		top, bottom = points[horzs[0]], points[horzs[1]]
	} else {
		top, bottom = points[horzs[1]], points[horzs[0]]
	}
	if points[verts[0]].X > points[verts[1]].X {
		left, right = points[verts[0]], points[verts[1]]
	} else {
		left, right = points[verts[1]], points[verts[0]]
	}

	bbox := model.PdfRectangle{Llx: left.X, Urx: right.X, Lly: bottom.Y, Ury: top.Y}
	if bbox.Llx > bbox.Urx {
		bbox.Llx, bbox.Urx = bbox.Urx, bbox.Llx
	}
	if bbox.Lly > bbox.Ury {
		bbox.Lly, bbox.Ury = bbox.Ury, bbox.Lly
	}

	v := rectRuling{PdfRectangle: bbox, kind: rectKind(bbox)}
	if v.kind == rulingNil {
		return &ruling{}, false
	}
	return v.asRuling(), true
}

// String returns a description of `k`.
func (k rulingKind) String() string {
	s, ok := rulingString[k]
	if !ok {
		return fmt.Sprintf("Not a ruling: %d", k)
	}
	return s
}

var rulingString = map[rulingKind]string{
	rulingNil:  "none",
	rulingHorz: "horizontal",
	rulingVert: "vertical",
}

const rulingTol = 1.0
const rulingSignificant = 10.0

// String returns a description of `v`.
func (v *ruling) String() string {
	if v.kind == rulingNil {
		return "NOT RULING"
	}
	pri, sec := "x", "y"
	if v.kind == rulingHorz {
		pri, sec = "y", "x"
	}
	return fmt.Sprintf("%10s %s=%6.2f %s=%6.2f - %6.2f (%6.2f)",
		v.kind, pri, v.primary, sec, v.lo, v.hi, v.hi-v.lo)
}

func (v *ruling) equals(v2 *ruling) bool {
	return v.kind == v2.kind && v.primary == v2.primary && v.lo == v2.lo && v.hi == v2.hi
}

func lineKind(p1, p2 transform.Point) rulingKind {
	dx := math.Abs(p1.X - p2.X)
	dy := math.Abs(p1.Y - p2.Y)
	kind := rulingNil
	if dx >= rulingSignificant && dy <= rulingTol {
		kind = rulingHorz
	} else if dy >= rulingSignificant && dx <= rulingTol {
		kind = rulingVert
	}
	return kind
}

func rectKind(r model.PdfRectangle) rulingKind {
	dx := r.Width()
	dy := r.Height()
	kind := rulingNil
	if dx >= rulingSignificant && dy <= rulingTol {
		kind = rulingHorz
	} else if dy >= rulingSignificant && dx <= rulingTol {
		kind = rulingVert
	} else {
		// common.Log.Error("rectKind: %6.2f %6.2f x %6.2f", r, r.Width(), r.Height())
	}
	return kind
}

func (v lineRuling) xMean() float64 {
	return 0.5 * (v.p1.X + v.p2.X)
}

func (v lineRuling) xDelta() float64 {
	return math.Abs(v.p2.X - v.p2.X)
}

func (v lineRuling) yMean() float64 {
	return 0.5 * (v.p1.Y + v.p2.Y)
}

func (v lineRuling) yDelta() float64 {
	return math.Abs(v.p2.Y - v.p2.Y)
}

// tidied returns a cleaned up version of `vecs`.
// - duplicate rulings are removed.
// - aligned rulings with small gaps are merged.
func (vecs rulingList) tidied(title string) rulingList {
	uniques := vecs.removeDuplicates()
	coallesced := uniques.collasce()
	coallesced.sort()
	return coallesced
}

// removeDuplicates returns `vecs` with duplicates eremoved
func (vecs rulingList) removeDuplicates() rulingList {
	if len(vecs) == 0 {
		return nil
	}
	vecs.sort()
	uniques := rulingList{vecs[0]}
	for _, v := range vecs[1:] {
		if v.equals(uniques[len(uniques)-1]) {
			continue
		}
		uniques = append(uniques, v)
	}
	return uniques
}

// collasce returns `vecs` with small gaps are merged.
func (vecs rulingList) collasce() rulingList {
	if len(vecs) == 0 {
		return nil
	}
	vecs.sortStrict()
	v0 := vecs[0]
	var uniques rulingList
	for _, v := range vecs[1:] {
		merging := v0.kind == v.kind && v0.primary == v.primary && v.lo <= v0.hi+1.0
		if merging {
			v00 := *v0
			v0.hi = v.hi
			if v0.hi < v0.lo {
				panic(fmt.Errorf("v0.hi < v0.lo\n\tv0=%s\n\t v=%s\n\t ->%s",
					v00.String(), v.String(), v0.String()))
			}
		} else {
			// fmt.Printf("%4d:\n\t%s ==\n\t%s\n", i, asString(v0), asString(v))
			uniques = append(uniques, v0)
			v0 = v
		}
	}

	uniques = append(uniques, v0)
	return uniques
}

// toGrids returns all the grids that can be formed from `vecs`.
func (vecs rulingList) toGrids() []rulingList {
	if len(vecs) == 0 {
		return nil
	}
	intersects := vecs.findIntersects()
	connections := map[int]map[int]bool{}
	for i := range vecs {
		connections[i] = vecs.findConnections(intersects, i)
	}

	// ordering puts rulings with more connections first then falls back to standard order.
	ordering := makeOrdering(len(vecs), func(i, j int) bool {
		ci, cj := len(connections[i]), len(connections[j])
		if ci != cj {
			return ci > cj
		}
		return vecs.comp(i, j)
	})

	// igrids is the list of  lists of `vecs` indexes of mutually connected rulings.
	igrids := [][]int{[]int{ordering[0]}}
outer:
	for o := 1; o < len(vecs); o++ {
		iv := ordering[o]
		for ig, g := range igrids {
			for _, i := range g {
				if connections[i][iv] {
					igrids[ig] = append(g, iv)
					continue outer
				}
			}
		}
		igrids = append(igrids, []int{iv})
	}

	// Return igrids with most rulings first.
	sort.SliceStable(igrids, func(i, j int) bool { return len(igrids[i]) > len(igrids[j]) })
	for i, g := range igrids {
		if len(g) <= 1 {
			continue
		}
		sort.Slice(g, func(i, j int) bool { return vecs.comp(g[i], g[j]) })
		igrids[i] = g
	}

	// Make the grids from the indexes.
	grids := make([]rulingList, len(igrids))
	for i, g := range igrids {
		grid := make(rulingList, len(g))
		for j, idx := range g {
			grid[j] = vecs[idx]
		}
		grids[i] = grid
	}

	// actualGrids are the grids we treat as significangt
	var actualGrids []rulingList
	for _, grid := range grids {
		if grid.isActualGrid() {
			actualGrids = append(actualGrids, grid)
		}
	}
	return actualGrids
}

// findIntersects return the set of sets of ruling intersections in `vecs`.
// intersects[i] is the set of the indexes in `vecs` of the rulings that intersect with vecs[i]
func (vecs rulingList) findIntersects() map[int]map[int]bool {
	var verts, horzs []int
	for i, v := range vecs {
		switch v.kind {
		case rulingVert:
			verts = append(verts, i)
		case rulingHorz:
			horzs = append(horzs, i)
		}
	}
	intersects := map[int]map[int]bool{}
	for _, i := range verts {
		intersects[i] = map[int]bool{}
	}
	for _, j := range horzs {
		intersects[j] = map[int]bool{}
	}
	for _, v := range verts {
		for _, h := range horzs {
			if vecs[v].intersects(vecs[h]) {
				intersects[v][h] = true
				intersects[h][v] = true
			}
		}
	}
	return intersects
}

// findConnections returns the set of indexes of `vecs` connected to `vecs`[`i`] by the
// intersections in `intersects`.
// TODO(petewilliams97): The vertical and horizontal rulings are the nodes of a bipartite graph
// their intersections are the edges in this graph, so there is probably some efficient way of doing
// this with.
// For now. we do a simple breadth first search.
func (vecs rulingList) findConnections(intersects map[int]map[int]bool, i int) map[int]bool {
	connections := map[int]bool{}
	visited := map[int]bool{}
	var bfs func(int)
	bfs = func(i0 int) {
		if !visited[i0] {
			visited[i0] = true
			for i := range vecs {
				if intersects[i][i0] {
					connections[i] = true
				}
			}
			for i := range vecs {
				if connections[i] {
					bfs(i)
				}
			}
		}
	}

	bfs(i)
	return connections
}

// isActualGrid is a decision function that tells whether a list of rulings is a grid.
func (vecs rulingList) isActualGrid() bool {
	numVert, numHorz := 0, 0
	for _, v := range vecs {
		switch v.kind {
		case rulingVert:
			numVert++
		case rulingHorz:
			numHorz++
		}
	}
	return numVert >= 2 && numHorz >= 2
}

// isRectPath returns true if `path` is an axis-aligned rectangle.
func (path *subpath) isRectPath() bool {
	if len(path.points) < 4 || len(path.points) > 5 {
		return false
	}
	if len(path.points) == 5 {
		p1 := path.points[0]
		p2 := path.points[4]
		if p1.X != p2.X || p1.Y != p2.Y {
			return false
		}
	}
	return true
}

// intersects returns true if `v` intersects `v2`.
func (v *ruling) intersects(v2 *ruling) bool {
	othogonal := (v.kind == rulingVert && v2.kind == rulingHorz) ||
		(v2.kind == rulingVert && v.kind == rulingHorz)
	overlap := func(v1, v2 *ruling) bool {
		return v1.lo <= v2.primary && v2.primary <= v1.hi
	}
	return othogonal && overlap(v, v2) && overlap(v2, v)
}

// sort sorts `vecs` by comp.
func (vecs rulingList) sort() {
	sort.Slice(vecs, func(i, j int) bool { return vecs.comp(i, j) })
}

// comp is the comparison function for standard rulingList oroder.
// - verticals before horizontals
// - top to bottom
// - left to right
func (vecs rulingList) comp(i, j int) bool {
	vi, vj := vecs[i], vecs[j]
	ki, kj := vi.kind, vj.kind
	if ki != kj {
		return ki > kj
	}
	if ki == rulingNil {
		return false
	}
	order := func(b bool) bool {
		if ki == rulingHorz {
			return b
		}
		return !b
	}

	mi, mj := vi.primary, vj.primary
	if mi != mj {
		return order(mi > mj)
	}
	mi, mj = vi.lo, vj.lo
	if mi != mj {
		return order(mi < mj)
	}
	return order(vi.hi < vj.hi)
}

// sort sorts `vecs`
// - verticals before horizontals
// - bottom to top
// - left to right
func (vecs rulingList) sortStrict() {
	sort.Slice(vecs, func(i, j int) bool {
		vi, vj := vecs[i], vecs[j]
		ki, kj := vi.kind, vj.kind
		if ki != kj {
			return ki > kj
		}

		mi, mj := vi.primary, vj.primary
		if mi != mj {
			return mi < mj
		}
		mi, mj = vi.lo, vj.lo
		if mi != mj {
			return mi < mj
		}
		return vi.hi < vj.hi
	})
}
