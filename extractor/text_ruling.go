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
	for i, v := range vecs[1:] {
		merging = v0.kind() == v.kind() && isZero(v0.primary()-v.primary()) && v.lo() <= v0.hi()+1.0
		if merging {
			v0._hi = v.hi()
			if v0._hi < v0._lo {
				panic(asString(v0))
			}
		} else {
			fmt.Printf("%4d:\n\t%s ==\n\t%s\n", i, asString(v0), asString(v))
			uniques = append(uniques, v0)
			v0 = newGenericRuling(v)
		}
	}

	uniques = append(uniques, v0)

	return uniques
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
		common.Log.Error("bboxKind: %6.2f %6.2f x %6.2f", r, r.Width(), r.Height())
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

func makeStrokeRulings(strokes []*subpath) rulingList {
	var vecs rulingList
	for _, path := range strokes {
		vecs = append(vecs, path._strokeRulings()...)
	}
	return vecs.tidied("strokes")
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

func makeFillRulings(fills []*subpath) rulingList {
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
	return vecs.tidied("fills")
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
