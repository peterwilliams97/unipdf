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
const rulingSignificant = 50.0

type edgeRuling struct {
	p1, p2 transform.Point
	_kind  rulingKind
}

func (v edgeRuling) kind() rulingKind { return v._kind }

func asString(v ruling) string {
	if v.kind() == rulingNil {
		return "NOT RULING"
	}
	return fmt.Sprintf("%10s x=%6.2f %6.2f - %6.2f (%6.2f)",
		v.kind(), v.primary(), v.lo(), v.hi(), v.hi()-v.lo())
	// switch v.kind() {
	// case rulingVer:
	// 	return fmt.Sprintf("%10s x=%6.2f dy=%6.2f =%6.2f - %6.2f",
	// 		v._kind, v.xMean(), v.p2.Y-v.p1.Y, v.p1.Y, v.p2.Y)
	// case rulingHor:
	// 	return fmt.Sprintf("%10s y=%6.2f dx=%6.2f =%6.2f - %6.2f",
	// 		v._kind, v.yMean(), v.p2.X-v.p1.X, v.p1.X, v.p2.X)
	// default:
	// 	return fmt.Sprintf("%10s p1=%6.2f p2=%6.2f", v._kind, v.p1, v.p2)
	// }
}

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

func makeEdgeRuling(p1, p2 transform.Point) edgeRuling {
	v := edgeRuling{p1: p1, p2: p2}
	dx := math.Abs(v.p1.X - v.p2.X)
	dy := math.Abs(v.p1.Y - v.p2.Y)
	if dx >= rulingSignificant && dy <= rulingTol {
		v._kind = rulingHor
	} else if dy >= rulingSignificant && dx <= rulingTol {
		v._kind = rulingVer
	} else {
		v._kind = rulingNil
	}
	return v
}

func strokeRulings(strokes []subpath) rulingList {
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
