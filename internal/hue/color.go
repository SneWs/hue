package hue

import "math"

// Point is a CIE xy chromaticity coordinate.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Gamut is the triangle of colors a bulb can actually produce.
type Gamut struct {
	Red   Point
	Green Point
	Blue  Point
}

// GamutA, GamutB, and GamutC are the triangles Philips publishes for Hue generations.
func GamutA() Gamut {
	return Gamut{Red: Point{0.704, 0.296}, Green: Point{0.2151, 0.7106}, Blue: Point{0.138, 0.08}}
}

func GamutB() Gamut {
	return Gamut{Red: Point{0.675, 0.322}, Green: Point{0.409, 0.518}, Blue: Point{0.167, 0.04}}
}

func GamutC() Gamut {
	return Gamut{Red: Point{0.6915, 0.3083}, Green: Point{0.17, 0.7}, Blue: Point{0.1532, 0.0475}}
}

// GamutByType maps the bridge's gamut_type string. Unknown types use gamut C,
// which covers current color bulbs.
func GamutByType(kind string) Gamut {
	switch kind {
	case "A":
		return GamutA()
	case "B":
		return GamutB()
	default:
		return GamutC()
	}
}

// Philips Hue RGB-to-XYZ matrix. White (1,1,1) lands on D65, xy 0.3127, 0.3290.
var rgbToXYZ = [3][3]float64{
	{0.649926, 0.103455, 0.197109},
	{0.234327, 0.743075, 0.022598},
	{0.000000, 0.053077, 1.035763},
}

// HSVToXY converts hue degrees and saturation/value in 0..1 into a gamut-clamped xy point.
func HSVToXY(hue, sat, val float64, gamut Gamut) Point {
	r, g, b := hsvToRGB(hue, sat, val)
	r, g, b = gamma(r), gamma(g), gamma(b)
	x, y, z := apply(rgbToXYZ, r, g, b)
	sum := x + y + z
	if sum <= 0 {
		return Point{}
	}
	return clampToGamut(Point{x / sum, y / sum}, gamut)
}

// XYToHSV finds the hue and saturation which, after gamut clamping, land
// closest to p. A direct matrix inverse drifts once the bulb cannot produce
// the requested sRGB color, which is most saturated hues.
func XYToHSV(p Point, gamut Gamut) (hue, sat, val float64) {
	if p.X <= 0 && p.Y <= 0 {
		return 0, 0, 0
	}
	bestH, bestS := 0.0, 0.0
	best := math.MaxFloat64
	consider := func(h, s float64) {
		if h < 0 {
			h += 360
		} else if h >= 360 {
			h -= 360
		}
		s = clamp01(s)
		q := HSVToXY(h, s, 1, gamut)
		if d := dist2(p, q); d < best {
			best, bestH, bestS = d, h, s
		}
	}
	for h := 0.0; h < 360; h += 2 {
		for s := 0.0; s <= 1.001; s += 0.05 {
			consider(h, s)
		}
	}
	for h := bestH - 2; h <= bestH+2; h += 0.25 {
		for s := bestS - 0.05; s <= bestS+0.05; s += 0.01 {
			consider(h, s)
		}
	}
	return bestH, bestS, 1
}

func hsvToRGB(h, s, v float64) (float64, float64, float64) {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	s = clamp01(s)
	v = clamp01(v)
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return r + m, g + m, b + m
}

func gamma(u float64) float64 {
	if u > 0.04045 {
		return math.Pow((u+0.055)/1.055, 2.4)
	}
	return u / 12.92
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func apply(m [3][3]float64, r, g, b float64) (float64, float64, float64) {
	return m[0][0]*r + m[0][1]*g + m[0][2]*b,
		m[1][0]*r + m[1][1]*g + m[1][2]*b,
		m[2][0]*r + m[2][1]*g + m[2][2]*b
}

func clampToGamut(p Point, g Gamut) Point {
	if inTriangle(p, g.Red, g.Green, g.Blue) {
		return p
	}
	best := closestPointOnSegment(g.Red, g.Green, p)
	bestD := dist2(p, best)
	for _, edge := range [][2]Point{{g.Green, g.Blue}, {g.Blue, g.Red}} {
		c := closestPointOnSegment(edge[0], edge[1], p)
		if d := dist2(p, c); d < bestD {
			best, bestD = c, d
		}
	}
	return best
}

func inTriangle(p, a, b, c Point) bool {
	d1 := cross(p, a, b)
	d2 := cross(p, b, c)
	d3 := cross(p, c, a)
	hasNeg := d1 < 0 || d2 < 0 || d3 < 0
	hasPos := d1 > 0 || d2 > 0 || d3 > 0
	return !(hasNeg && hasPos)
}

func cross(p, a, b Point) float64 {
	return (b.X-a.X)*(p.Y-a.Y) - (b.Y-a.Y)*(p.X-a.X)
}

func closestPointOnSegment(a, b, p Point) Point {
	dx, dy := b.X-a.X, b.Y-a.Y
	denom := dx*dx + dy*dy
	if denom == 0 {
		return a
	}
	t := ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / denom
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return Point{a.X + t*dx, a.Y + t*dy}
}

func dist2(a, b Point) float64 {
	dx, dy := a.X-b.X, a.Y-b.Y
	return dx*dx + dy*dy
}
