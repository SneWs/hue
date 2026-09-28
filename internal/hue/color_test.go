package hue

import (
	"math"
	"testing"
)

func TestWhiteLandsOnD65(t *testing.T) {
	p := HSVToXY(0, 0, 1, GamutC())
	if math.Abs(p.X-0.3127) > 0.002 || math.Abs(p.Y-0.3290) > 0.002 {
		t.Fatalf("white xy = %.4f, %.4f, want D65", p.X, p.Y)
	}
	if !inTriangle(p, GamutC().Red, GamutC().Green, GamutC().Blue) {
		t.Fatal("white was clamped outside gamut C")
	}
}

func TestRedIsClampedIntoGamut(t *testing.T) {
	p := HSVToXY(0, 1, 1, GamutC())
	g := GamutC()
	if !inTriangle(p, g.Red, g.Green, g.Blue) {
		t.Fatalf("red xy %.4f, %.4f is outside gamut C", p.X, p.Y)
	}
	// Pure sRGB red sits outside gamut C, so the result should sit on the red corner.
	if dist2(p, g.Red) > 0.02 {
		t.Fatalf("clamped red %.4f, %.4f is far from gamut red %.4f, %.4f", p.X, p.Y, g.Red.X, g.Red.Y)
	}
}

func TestHueRoundTrip(t *testing.T) {
	for _, sat := range []float64{1, 0.45} {
		for _, hue := range []float64{0, 40, 120, 190, 275, 320} {
			p := HSVToXY(hue, sat, 1, GamutC())
			gotH, gotS, _ := XYToHSV(p, GamutC())
			back := HSVToXY(gotH, gotS, 1, GamutC())
			if dist2(p, back) > 1e-4 {
				t.Errorf("sat %.2f hue %.0f came back as %.1f/%.2f, xy moved from %.3f,%.3f to %.3f,%.3f", sat, hue, gotH, gotS, p.X, p.Y, back.X, back.Y)
			}
		}
	}
}
