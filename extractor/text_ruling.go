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

type rulingKind int
type rulingList []*ruling

type ruling struct {
	kind    rulingKind
	primary float64
	lo      float64
	hi      float64
}

const (
	rulingNil rulingKind = iota
	rulingHor
	rulingVer
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
			if v := makeEdgeRuling(p1, p2); v.kind != rulingNil {
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
		if v, ok := path.makeBboxRuling(); ok && v.kind != rulingNil {
			vecs = append(vecs, v)
		}
	}
	vecs = vecs.tidied("fills")
	return vecs.toGrids()
}

type edgeRuling struct {
	kind   rulingKind
	p1, p2 transform.Point
}

type bboxRuling struct {
	kind rulingKind
	model.PdfRectangle
}

func (v edgeRuling) asRuling() *ruling {
	r := ruling{kind: v.kind}
	switch v.kind {
	case rulingVer:
		r.primary = v.xMean()
		r.lo = math.Min(v.p1.Y, v.p2.Y)
		r.hi = math.Max(v.p1.Y, v.p2.Y)
	case rulingHor:
		r.primary = v.yMean()
		r.lo = math.Min(v.p1.X, v.p2.X)
		r.hi = math.Max(v.p1.X, v.p2.X)
	default:
		panic(fmt.Errorf("bad primary kind=%d", v.kind))
	}
	return &r
}

func makeEdgeRuling(p1, p2 transform.Point) *ruling {
	v := edgeRuling{p1: p1, p2: p2, kind: edgeKind(p1, p2)}
	if v.kind == rulingNil {
		return &ruling{}
	}
	return v.asRuling()
}

func (v bboxRuling) asRuling() *ruling {
	r := ruling{kind: v.kind}

	switch v.kind {
	case rulingVer:
		r.primary = 0.5 * (v.Llx + v.Urx)
		r.lo = v.Lly
		r.hi = v.Ury
	case rulingHor:
		r.primary = 0.5 * (v.Lly + v.Ury)
		r.lo = v.Llx
		r.hi = v.Urx
	default:
		panic(fmt.Errorf("bad primary kind=%d", v.kind))
	}
	return &r
}

func (path *subpath) makeBboxRuling() (*ruling, bool) {
	points := path.points[:4]
	kinds := map[int]rulingKind{}
	for i, p1 := range points {
		p2 := path.points[(i+1)%4]
		kinds[i] = edgeKind(p1, p2)
		// fmt.Printf("%4d: %7.2f %7.2f %s\n", i, p1, p2, kinds[i])
	}
	var verts, horzs []int
	for i, k := range kinds {
		// fmt.Printf("%3d: %7.2f %s\n", i, path.points[i], k)
		switch k {
		case rulingHor:
			horzs = append(horzs, i)
		case rulingVer:
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
			if k != rulingHor {
				verts = append(verts, i)
			}
		}
	}
	if len(horzs) == 0 {
		for i, k := range kinds {
			if k != rulingVer {
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

	bbox := model.PdfRectangle{
		Llx: left.X,
		Urx: right.X,
		Lly: bottom.Y,
		Ury: top.Y,
	}
	if bbox.Llx > bbox.Urx {
		bbox.Llx, bbox.Urx = bbox.Urx, bbox.Llx
	}
	if bbox.Lly > bbox.Ury {
		bbox.Lly, bbox.Ury = bbox.Ury, bbox.Lly
	}

	v := bboxRuling{PdfRectangle: bbox, kind: bboxKind(bbox)}
	// fmt.Printf("### %6.2f %6.2f %s\n", points, bbox, asString(v))
	if v.kind == rulingNil {
		return &ruling{}, false
	}
	return v.asRuling(), true

}

func (r rulingKind) String() string {
	s, ok := rulingString[r]
	if !ok {
		return fmt.Sprintf("Not a ruling: %d", r)
	}
	return s
}

var rulingString = map[rulingKind]string{
	rulingNil: "none",
	rulingHor: "horizontal",
	rulingVer: "vertical",
}

const rulingTol = 1.0
const rulingSignificant = 10.0

func (v *ruling) String() string {
	if v.kind == rulingNil {
		return "NOT RULING"
	}
	pri, sec := "x", "y"
	if v.kind == rulingHor {
		pri, sec = "y", "x"
	}
	return fmt.Sprintf("%10s %s=%6.2f %s=%6.2f - %6.2f (%6.2f)",
		v.kind, pri, v.primary, sec, v.lo, v.hi, v.hi-v.lo)
}

func equalRulings(v1, v2 *ruling) bool {
	return v1.kind == v2.kind &&
		v1.primary == v2.primary &&
		v1.lo == v2.lo &&
		v1.hi == v2.hi
}

func edgeKind(p1, p2 transform.Point) rulingKind {
	dx := math.Abs(p1.X - p2.X)
	dy := math.Abs(p1.Y - p2.Y)
	kind := rulingNil
	if dx >= rulingSignificant && dy <= rulingTol {
		kind = rulingHor
	} else if dy >= rulingSignificant && dx <= rulingTol {
		kind = rulingVer
	}
	return kind
}

func bboxKind(r model.PdfRectangle) rulingKind {
	dx := r.Width()
	dy := r.Height()
	kind := rulingNil
	if dx >= rulingSignificant && dy <= rulingTol {
		kind = rulingHor
	} else if dy >= rulingSignificant && dx <= rulingTol {
		kind = rulingVer
	} else {
		// common.Log.Error("bboxKind: %6.2f %6.2f x %6.2f", r, r.Width(), r.Height())
	}
	return kind
}

func (v edgeRuling) xMean() float64 {
	return 0.5 * (v.p1.X + v.p2.X)
}

func (v edgeRuling) xDelta() float64 {
	return math.Abs(v.p2.X - v.p2.X)
}

func (v edgeRuling) yMean() float64 {
	return 0.5 * (v.p1.Y + v.p2.Y)
}

func (v edgeRuling) yDelta() float64 {
	return math.Abs(v.p2.Y - v.p2.Y)
}

func (vecs rulingList) tidied(title string) rulingList {
	uniques := vecs.removeDuplicates()
	coallesced := uniques.collasce()
	coallesced.sort()
	// common.Log.Info("tidied %s: %d->%d->%d", title, len(vecs), len(uniques), len(coallesced))
	return coallesced
}

func (vecs rulingList) removeDuplicates() rulingList {
	if len(vecs) == 0 {
		return nil
	}
	vecs.sort()
	uniques := rulingList{vecs[0]}
	for _, v := range vecs[1:] {
		if equalRulings(v, uniques[len(uniques)-1]) {
			continue
		}
		uniques = append(uniques, v)
	}
	return uniques
}

func (vecs rulingList) collasce() rulingList {
	if len(vecs) == 0 {
		return nil
	}
	vecs.sortStrict()
	v0 := vecs[0]
	var uniques rulingList
	for _, v := range vecs[1:] {
		// if v0.hi < v0.lo {
		// 	panic(fmt.Errorf("v0.hi < v0.lo\n\tv0=%s\n\t v=%s", asString(v00), asString(v)))
		// }
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

func (vecs rulingList) kinds(elements []int) []rulingKind {
	kinds := make([]rulingKind, len(elements))
	for i, e := range elements {
		v := vecs[e]
		kinds[i] = v.kind
	}
	return kinds
}

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

	// common.Log.Notice("ordering")
	// for i, o := range ordering {
	// 	fmt.Printf("%4d: %d\n", i, len(connections[o]))
	// }

	// igrids is the list of  lists of `vecs` indexes of mutually connected rulings.
	// TODO(peterwilliams97): This can be ambiguous. Maybe sort by elements with most connections
	igrids := [][]int{[]int{ordering[0]}}
outer:
	for ov := 1; ov < len(vecs); ov++ {
		iv := ordering[ov]
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

	// common.Log.Notice("sording")
	sort.SliceStable(igrids, func(i, j int) bool { return len(igrids[i]) > len(igrids[j]) })
	for i, g := range igrids {
		if len(g) <= 1 {
			continue
		}
		// fmt.Printf("%2d: %v %v\n", i, g, vecs.kinds(g))
		sort.Slice(g, func(i, j int) bool { return vecs.comp(g[i], g[j]) })
		// fmt.Printf("    %v %v\n", g, vecs.kinds(g))
		igrids[i] = g
	}
	// common.Log.Notice("results")
	// for i, g := range igrids {
	// 	if len(g) <= 1 {
	// 		continue
	// 	}
	// 	fmt.Printf("%2d: %v %v\n", i, g, vecs.kinds(g))
	// 	fmt.Printf("%4d: %2d: %v\n", 0, g[0], vecs[g[0]])
	// 	for j := 1; j < len(g); j++ {
	// 		gk := g[j-1]
	// 		gj := g[j]
	// 		fmt.Printf("%4d: %2d: %v\n", j, gj, vecs[gj])
	// 		// if !vecs.comp(gk, gj) {
	// 		// 	panic(fmt.Errorf("j=%d\n\t%s\n\t%s", j, vecs[gk], vecs[gj]))
	// 		// }
	// 		if vecs[gk].kind == rulingHor && vecs[gj].kind == rulingVer {
	// 			panic(fmt.Errorf("j=%d\n\t%s\n\t%s", j, vecs[gk].String(), vecs[gj]))
	// 		}
	// 	}
	// }
	// igrids is the list of  lists of  of connected rulings.
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
		case rulingVer:
			verts = append(verts, i)
		case rulingHor:
			horzs = append(horzs, i)
		}
	}
	// key := func(i,j) int { return 1000*i + j}
	intersects := map[int]map[int]bool{}
	for _, i := range verts {
		intersects[i] = map[int]bool{}
	}
	for _, j := range horzs {
		intersects[j] = map[int]bool{}
	}
	// common.Log.Notice("compute intersections ----------")
	for _, v := range verts {
		for _, h := range horzs {
			// fmt.Printf("%4d %2d:", v, h)
			if rulingsIntersect(vecs[v], vecs[h]) {
				intersects[v][h] = true
				intersects[h][v] = true
			}
		}
	}
	return intersects
}

// findConnections returns the set of indexes of `vecs` connected to vecs[i00] by the intersections
// in `intersects`.
// TODO(petewilliams97): The vertical and horizontal rulings are the nodes of a bipartite graph
// their intersections are the edges in this graph, so there is probably some efficient way of doing
// this with.
// For now. we do a simple breadth first search.
func (vecs rulingList) findConnections(intersects map[int]map[int]bool, i00 int) map[int]bool {
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
	bfs(i00)
	return connections
}

// isActualGrid is a decision function that tells whether a list of rulings is a grid.
func (vecs rulingList) isActualGrid() bool {
	numVert, numHorz := 0, 0
	for _, v := range vecs {
		switch v.kind {
		case rulingVer:
			numVert++
		case rulingHor:
			numHorz++
		}
	}
	return numVert >= 2 && numHorz >= 2
}

func (path *subpath) isRectPath() bool {
	if len(path.points) < 4 || len(path.points) > 5 {
		return false
	}
	if len(path.points) == 5 {
		p1 := path.points[0]
		p2 := path.points[4]
		if p1.X != p2.X || p1.Y != p2.Y {
			// common.Log.Notice("Not rect: %s", path.String())
			return false
		}
	}
	return true
}

func rulingsIntersect(v1, v2 *ruling) bool {
	othogonal := (v1.kind == rulingVer && v2.kind == rulingHor) ||
		(v2.kind == rulingVer && v1.kind == rulingHor)
	overlap := func(v1, v2 *ruling) bool {
		return v1.lo <= v2.primary && v2.primary <= v1.hi
	}
	return othogonal && overlap(v1, v2) && overlap(v2, v1)
}

func (vecs rulingList) sort() {
	sort.Slice(vecs, func(i, j int) bool {
		return vecs.comp(i, j)
		vi, vj := vecs[i], vecs[j]
		ki, kj := vi.kind, vj.kind
		if ki != kj {
			return ki > kj
		}
		if ki == rulingNil {
			return false
		}
		order := func(b bool) bool {
			if ki == rulingHor {
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
	})
}

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
		if ki == rulingHor {
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
