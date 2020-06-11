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

const rulingTol = 1.0 / 200.0
const rulingSignificant = 50.0

type rulingList []vector
type vector struct {
	p1, p2 transform.Point
	kind   rulingKind
}
type rulingKind int

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

func (v vector) String() string {
	switch v.kind {
	case rulingVer:
		return fmt.Sprintf("%10s x=%6.2f dy=%6.2f =%6.2f - %6.2f",
			v.kind, v.xMean(), v.p2.Y-v.p1.Y, v.p1.Y, v.p2.Y)
	case rulingHor:
		return fmt.Sprintf("%10s y=%6.2f dx=%6.2f =%6.2f - %6.2f",
			v.kind, v.yMean(), v.p2.X-v.p1.X, v.p1.X, v.p2.X)
	default:
		return fmt.Sprintf("%10s p1=%6.2f p2=%6.2f", v.kind, v.p1, v.p2)
	}
}

func (v vector) xMean() float64 {
	return 0.5 * (v.p1.X + v.p2.X)
}

func (v vector) xDelta() float64 {
	return math.Abs(v.p2.X - v.p2.X)
}

func (v vector) yMean() float64 {
	return 0.5 * (v.p1.Y + v.p2.Y)
}

func (v vector) yDelta() float64 {
	return math.Abs(v.p2.Y - v.p2.Y)
}

func (v vector) asRuling() vector {
	dx := math.Abs(v.p1.X - v.p2.X)
	dy := math.Abs(v.p1.Y - v.p2.Y)
	if dx >= rulingSignificant && dy <= rulingTol {
		y := v.yMean()
		v.p1.Y = y
		v.p2.Y = y
		v.kind = rulingHor
	} else if dy >= rulingSignificant && dx <= rulingTol {
		x := v.xMean()
		v.p1.X = x
		v.p2.X = x
		v.kind = rulingVer
	} else {
		v.kind = rulingNil
	}
	return v
}

func pathRulings(paths []subpath) rulingList {
	var vecs rulingList
	for _, path := range paths {
		vecs = append(vecs, path._rulings()...)
	}
	vecs.sort()
	return vecs
}

func (path subpath) rulings() rulingList {
	vecs := path.rulings()
	vecs.sort()
	return vecs
}

func (path subpath) _rulings() rulingList {
	if len(path) < 2 {
		return nil
	}
	var vecs rulingList
	p1 := path[0]
	for _, p2 := range path[1:] {
		v := vector{p1: p1, p2: p2}
		vecs = append(vecs, v.asRuling())
		p1 = p2
	}
	return vecs
}

func (vecs rulingList) sort() {
	sort.Slice(vecs, func(i, j int) bool {
		vi, vj := vecs[i], vecs[j]
		if vi.kind != vj.kind {
			return vi.kind > vj.kind
		}
		switch vi.kind {
		case rulingHor:
			mi, mj := vi.yMean(), vj.yMean()
			if mi != mj {
				return mi > mj
			}
			mi, mj = vi.p1.X, vj.p1.X
			if mi != mj {
				return mi > mj
			}
		case rulingVer:
			mi, mj := vi.xMean(), vj.xMean()
			if mi != mj {
				return mi < mj
			}
			mi, mj = vi.p1.Y, vj.p1.Y
			if mi != mj {
				return mi > mj
			}
		}
		mi, mj := vi.p1.Y, vj.p1.Y
		if mi != mj {
			return mi > mj
		}
		return vi.p1.X < vj.p1.X
	})
}
