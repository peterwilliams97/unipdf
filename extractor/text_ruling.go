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
	// String() string
}

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

const rulingTol = 1.0 / 200.0
const rulingSignificant = 10.0

func asString(v ruling) string {
	if v.kind() == rulingNil {
		return "NOT RULING"
	}
	return fmt.Sprintf("%10s x=%6.2f %6.2f - %6.2f (%6.2f)",
		v.kind(), v.primary(), v.lo(), v.hi(), v.hi()-v.lo())
}

func (vecs rulingList) sort() {
	sort.Slice(vecs, func(i, j int) bool {
		vi, vj := vecs[i], vecs[j]
		if vi.kind() != vj.kind() {
			return vi.kind() > vj.kind()
		}
		order := func(b bool) bool {
			if vi.kind() == rulingVer {
				return !b
			}
			return b
		}

		mi, mj := vi.primary(), vj.primary()
		if mi != mj {
			return order(mi < mj)
		}
		mi, mj = vi.lo(), vj.lo()
		if mi != mj {
			return !order(mi < mj)
		}
		return !order(vi.hi() < vj.hi())
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

func (path subpath) makeBboxRuling() bboxRuling {
	kinds := map[int]rulingKind{}
	for i, p1 := range path {
		p2 := path[(i+1)%4]
		kinds[i] = edgeKind(p1, p2)
		// fmt.Printf("%4d: %7.2f %7.2f %s\n", i, p1, p2, kinds[i])
	}
	var verts, horzs []int
	for i, k := range kinds {
		// fmt.Printf("%3d: %7.2f %s\n", i, path[i], k)
		switch k {
		case rulingHor:
			horzs = append(horzs, i)
		case rulingVer:
			verts = append(verts, i)
		}
	}
	// common.Log.Info("verts=%d, horzs=%d", len(verts), len(horzs))
	if len(horzs) != 2 || len(verts) != 2 {
		panic(fmt.Errorf("rect: %s", path.String()))
	}

	var left, right, top, bottom transform.Point
	if path[horzs[0]].Y > path[horzs[1]].Y {
		top, bottom = path[horzs[0]], path[horzs[1]]
	} else {
		top, bottom = path[horzs[1]], path[horzs[0]]
	}
	if path[verts[0]].X > path[verts[1]].X {
		left, right = path[verts[0]], path[verts[1]]
	} else {
		left, right = path[verts[1]], path[verts[0]]
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

	return bboxRuling{PdfRectangle: bbox, _kind: bboxKind(bbox)}
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

func (v bboxRuling) lo() float64 {
	switch v._kind {
	case rulingVer:
		return v.Llx
	case rulingHor:
		return v.Lly
	default:
		panic(v)
	}
}

func (v bboxRuling) hi() float64 {
	switch v._kind {
	case rulingVer:
		return v.Urx
	case rulingHor:
		return v.Ury
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

func makeStrokeRulings(strokes []subpath) rulingList {
	var vecs rulingList
	for _, path := range strokes {
		vecs = append(vecs, path._strokeRulings()...)
	}
	vecs.sort()
	return vecs
}

func (path subpath) strokeRulings() rulingList {
	vecs := path._strokeRulings()
	vecs.sort()
	return vecs
}

func (path subpath) _strokeRulings() rulingList {
	if len(path) < 2 {
		return nil
	}
	var vecs rulingList
	p1 := path[0]
	for _, p2 := range path[1:] {
		v := makeEdgeRuling(p1, p2)
		vecs = append(vecs, v)
		p1 = p2
	}
	return vecs
}

func makeFillRulings(fills []subpath) rulingList {
	var vecs rulingList
	for _, path := range fills {
		if !path.isRectPath() {
			continue
		}
		v := path[:4].makeBboxRuling()
		if v.kind() == rulingNil {
			// common.Log.Info("Bad ruling: %s", asString(v))
			continue
		}
		vecs = append(vecs, v)
	}
	vecs.sort()
	return vecs
}

// func (path subpath) strokeRulings() rulingList {
// 	vecs := path._strokeRulings()
// 	vecs.sort()
// 	return vecs
// }

func (path subpath) isRectPath() bool {
	if len(path) < 4 || len(path) > 5 {
		common.Log.Notice("Not rect: %s", path.String())
		return false
	}
	if len(path) == 5 {
		p1 := path[0]
		p2 := path[4]
		if p1.X != p2.X || p1.Y != p2.Y {
			common.Log.Notice("Not rect: %s", path.String())
			return false
		}
	}
	return true
}
