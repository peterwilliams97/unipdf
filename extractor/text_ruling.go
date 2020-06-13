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
	"github.com/unidoc/unipdf/v3/internal/transform"
	"github.com/unidoc/unipdf/v3/model"
)

type ruling interface {
	kind() rulingKind
	primary() float64
	lo() float64
	hi() float64
}

type genericRuling struct {
	_kind    rulingKind
	_primary float64
	_lo      float64
	_hi      float64
}

func newGenericRuling(v ruling) *genericRuling {
	return &genericRuling{
		_kind:    v.kind(),
		_primary: v.primary(),
		_lo:      v.lo(),
		_hi:      v.hi(),
	}
}

func (r *genericRuling) kind() rulingKind { return r._kind }
func (r *genericRuling) primary() float64 { return r._primary }
func (r *genericRuling) lo() float64      { return r._lo }
func (r *genericRuling) hi() float64      { return r._hi }

type rulingKind int
type rulingList []ruling

const (
	rulingNil rulingKind = iota
	rulingHor
	rulingVer
)

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

func asString(v ruling) string {
	if v.kind() == rulingNil {
		return "NOT RULING"
	}
	pri, sec := "x", "y"
	if v.kind() == rulingHor {
		pri, sec = "y", "x"
	}
	return fmt.Sprintf("%10s %s=%6.2f %s=%6.2f - %6.2f (%6.2f)",
		v.kind(), pri, v.primary(), sec, v.lo(), v.hi(), v.hi()-v.lo())
}

func equalRulings(v1, v2 ruling) bool {
	return v1.kind() == v2.kind() &&
		v1.primary() == v2.primary() &&
		v1.lo() == v2.lo() &&
		v1.hi() == v2.hi()
}

type edgeRuling struct {
	p1, p2 transform.Point
	_kind  rulingKind
}

type bboxRuling struct {
	model.PdfRectangle
	_kind rulingKind
}

func (path *subpath) makeBboxRuling() (bboxRuling, bool) {
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
		common.Log.Info("verts=%d, horzs=%d", len(verts), len(horzs))
		return bboxRuling{}, false
		panic(fmt.Errorf("rect: %q", path.String()))
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

	v := bboxRuling{PdfRectangle: bbox, _kind: bboxKind(bbox)}
	// fmt.Printf("### %6.2f %6.2f %s\n", points, bbox, asString(v))
	return v, true
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

func makeEdgeRuling(p1, p2 transform.Point) edgeRuling {
	return edgeRuling{p1: p1, p2: p2, _kind: edgeKind(p1, p2)}
}

func (v edgeRuling) kind() rulingKind { return v._kind }
func (v bboxRuling) kind() rulingKind { return v._kind }

func (v edgeRuling) primary() float64 {
	switch v._kind {
	case rulingVer:
		return v.xMean()
	case rulingHor:
		return v.yMean()
	default:
		panic(v)
	}
}

func (v edgeRuling) lo() float64 {
	switch v._kind {
	case rulingVer:
		return math.Min(v.p1.Y, v.p2.Y)
	case rulingHor:
		return math.Min(v.p1.X, v.p2.X)
	default:
		panic(v)
	}
}

func (v edgeRuling) hi() float64 {
	switch v._kind {
	case rulingVer:
		return math.Max(v.p1.Y, v.p2.Y)
	case rulingHor:
		return math.Max(v.p1.X, v.p2.X)
	default:
		panic(v)
	}
}

// {145.88 146.62 2082.75 2106.00}   vertical x=146.25 y=145.88 - 146.62
func (v bboxRuling) primary() float64 {
	switch v._kind {
	case rulingVer:
		return 0.5 * (v.Llx + v.Urx)
	case rulingHor:
		return 0.5 * (v.Lly + v.Ury)
	default:
		panic(v)
	}
}
func (v bboxRuling) lo() float64 {
	switch v._kind {
	case rulingVer:
		return v.Lly
	case rulingHor:
		return v.Llx
	default:
		panic(v)
	}
}

func (v bboxRuling) hi() float64 {
	switch v._kind {
	case rulingVer:
		return v.Ury
	case rulingHor:
		return v.Urx
	default:
		panic(v)
	}
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

func makeStrokeRulings(strokes []*subpath) []rulingList {
	var vecs rulingList
	for _, path := range strokes {
		vecs = append(vecs, path._strokeRulings()...)
	}
	vecs = vecs.tidied("strokes")
	return vecs.toGrids()
}

func (path *subpath) _strokeRulings() rulingList {
	if len(path.points) < 2 {
		return nil
	}
	var vecs rulingList
	p1 := path.points[0]
	for _, p2 := range path.points[1:] {
		v := makeEdgeRuling(p1, p2)
		vecs = append(vecs, v)
		p1 = p2
	}
	return vecs
}

func makeFillRulings(fills []*subpath) []rulingList {
	var vecs rulingList
	for _, path := range fills {
		if !path.isRectPath() {
			continue
		}
		v, ok := path.makeBboxRuling()
		if !ok {
			continue
		}
		if v.kind() == rulingNil {
			// common.Log.Info("Bad ruling: %s", asString(v))
			continue
		}
		vecs = append(vecs, v)
	}

	vecs = vecs.tidied("fills")
	return vecs.toGrids()
}

func (vecs rulingList) tidied(title string) rulingList {
	vecs.sort()
	uniques := vecs.removeDuplicates()
	uniques.sortStrict()
	coallesced := uniques.collasce()
	coallesced.sort()
	common.Log.Info("tidied %s: %d->%d->%d", title, len(vecs), len(uniques), len(coallesced))
	return coallesced
}

func (vecs rulingList) removeDuplicates() rulingList {
	if len(vecs) == 0 {
		return nil
	}
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
	v0 := newGenericRuling(vecs[0])
	var uniques rulingList
	merging := false
	for _, v := range vecs[1:] {
		merging = v0.kind() == v.kind() && isZero(v0.primary()-v.primary()) && v.lo() <= v0.hi()+1.0
		if merging {
			v0._hi = v.hi()
			if v0._hi < v0._lo {
				panic(asString(v0))
			}
		} else {
			// fmt.Printf("%4d:\n\t%s ==\n\t%s\n", i, asString(v0), asString(v))
			uniques = append(uniques, v0)
			v0 = newGenericRuling(v)
		}
	}

	uniques = append(uniques, v0)

	return uniques
}

func (vecs rulingList) _toGrids() []rulingList {
	if len(vecs) == 0 {
		return nil
	}
	grids := []rulingList{rulingList{vecs[0]}}
outer:
	for _, v := range vecs[1:] {
		for i, g := range grids {
			if g.intersect(v) {
				grids[i] = append(g, v)
				continue outer
			}
		}
		grids = append(grids, rulingList{v})
	}
	return grids
}

func (vecs rulingList) toGrids() []rulingList {
	if len(vecs) == 0 {
		return nil
	}
	var verts, horzs []int
	for i, v := range vecs {
		switch v.kind() {
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
	common.Log.Notice("compute intersections ----------")
	for _, v := range verts {
		for _, h := range horzs {
			fmt.Printf("%4d %2d:", v, h)
			if rulingsIntersect(vecs[v], vecs[h]) {
				intersects[v][h] = true
				intersects[h][v] = true
			}
		}
	}

	var keys []int
	for i := range intersects {
		keys = append(keys, i)
	}
	sort.Ints(keys)
	common.Log.Notice("intersections ----------")
	for _, i := range keys {
		row := intersects[i]
		var keys2 []int
		for j := range row {
			keys2 = append(keys2, j)
		}
		sort.Ints(keys2)
		s := fmt.Sprintf("%2d", keys2)
		fmt.Printf("%4d: %-40s %s\n", i, s, asString(vecs[i]))
	}

	findConnections := func(i00 int) map[int]bool {
		connections := map[int]bool{}
		visited := map[int]bool{}
		var dfs func(i0, depth int)
		dfs = func(i0, depth int) {
			// fmt.Printf("  %sdfs i0=%2d\n", depthString(depth), i0)
			if visited[i0] {
				return
			}
			visited[i0] = true
			if depth > len(vecs)*2 {
				panic(depth)
			}
			for i := range vecs {
				if visited[i] {
					continue
				}
				if !intersects[i][i0] {
					continue
				}
				connections[i] = true
				// fmt.Printf("    %si=%2d %t\n", depthString(depth), i, connections[i])
				// if !connections[i] {
				// 	continue
				// }
			}
			for i := range vecs {
				if !connections[i] {
					continue
				}
				dfs(i, depth+1)
			}
		}
		dfs(i00, 0)
		return connections
	}

	connections := map[int]map[int]bool{}
	for i := range vecs {
		connections[i] = findConnections(i)
	}

	common.Log.Notice("connections ----------")
	for i := range vecs {
		fmt.Printf("%4d: %v\n", i, connections[i])
	}
	// os.Exit(1)

	igrids := [][]int{[]int{0}}
outer:
	for iv := 1; iv < len(vecs); iv++ {
		// fmt.Printf("%4d: == igrids=%2d\n", iv, len(igrids))
		for ig, g := range igrids {
			// fmt.Printf("%8d: %2d\n", ig, g)
			for _, i := range g {
				if /*i != iv && */ connections[i][iv] {
					// fmt.Printf("%12d: %2d\n", i, iv)
					igrids[ig] = append(g, iv)
					continue outer
				}
			}
		}
		igrids = append(igrids, []int{iv})
	}

	common.Log.Info("igrids -----------------------")
	for i, g := range igrids {
		fmt.Printf("%4d: %2d\n", i, g)
	}

	var grids []rulingList
	for _, g := range igrids {
		var grid rulingList
		for _, i := range g {
			grid = append(grid, vecs[i])
		}
		grids = append(grids, grid)
	}

	return grids
	var actualGrids []rulingList
	for _, grid := range grids {
		if grid.isActualGrid() {
			actualGrids = append(actualGrids, grid)
		}
	}
	return actualGrids
}

func (vecs rulingList) isActualGrid() bool {
	numVert, numHorz := 0, 0
	for _, v := range vecs {
		switch v.kind() {
		case rulingVer:
			numVert++
		case rulingHor:
			numHorz++
		}
	}
	return numVert >= 1 && numHorz >= 1
}

func depthString(depth int) string {
	parts := make([]string, depth)
	for i := range parts {
		parts[i] = "    "
	}
	return strings.Join(parts, "")
}
func (vecs rulingList) intersect(v0 ruling) bool {
	for _, v := range vecs {
		if rulingsIntersect(v0, v) {
			return true
		}
	}
	return false
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

func equalPoints(p1, p2 transform.Point) bool {
	return p1.X == p2.X && p1.Y == p2.Y
}

func rulingsIntersect(v1, v2 ruling) bool {
	othogonal := (v1.kind() == rulingVer && v2.kind() == rulingHor) ||
		(v2.kind() == rulingVer && v1.kind() == rulingHor)
	overlap := func(v1, v2 ruling) bool {
		return v1.lo() <= v2.primary() && v2.primary() <= v1.hi()
	}
	// if othogonal && !(overlap(v1, v2) && overlap(v2, v1)) {
	// 	fmt.Printf("%5t %5t\n\t\t%s\n\t\t%s\n",
	// 		overlap(v1, v2), overlap(v2, v1),
	// 		asString(v1), asString(v2))
	// }
	return othogonal && overlap(v1, v2) && overlap(v2, v1)
}

func (vecs rulingList) sort() {
	sort.Slice(vecs, func(i, j int) bool {
		vi, vj := vecs[i], vecs[j]
		ki, kj := vi.kind(), vj.kind()
		if ki != kj {
			return ki > kj
		}
		order := func(b bool) bool {
			if ki == rulingHor {
				return b
			}
			return !b
		}

		mi, mj := vi.primary(), vj.primary()
		if mi != mj {
			return order(mi > mj)
		}
		mi, mj = vi.lo(), vj.lo()
		if mi != mj {
			return order(mi < mj)
		}
		return order(vi.hi() < vj.hi())
	})
}

func (vecs rulingList) sortStrict() {
	sort.Slice(vecs, func(i, j int) bool {
		vi, vj := vecs[i], vecs[j]
		ki, kj := vi.kind(), vj.kind()
		if ki != kj {
			return ki > kj
		}

		mi, mj := vi.primary(), vj.primary()
		if mi != mj {
			return mi < mj
		}
		mi, mj = vi.lo(), vj.lo()
		if mi != mj {
			return mi < mj
		}
		return mi < mj
	})
}
