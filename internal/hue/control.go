package hue

import "encoding/json"

// XYForResource converts a hue angle and saturation percent into an xy point
// inside the gamut advertised by a light resource. Missing gamut data uses gamut C.
func XYForResource(raw json.RawMessage, hueDeg, saturationPercent float64) Point {
	var parsed light
	_ = json.Unmarshal(raw, &parsed)
	gamut := parsed.gamut()
	if saturationPercent < 0 {
		saturationPercent = 0
	}
	if saturationPercent > 100 {
		saturationPercent = 100
	}
	return HSVToXY(hueDeg, saturationPercent/100, 1, gamut)
}
