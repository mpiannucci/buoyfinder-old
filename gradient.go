package buoyfinder

import "github.com/lucasb-eyer/go-colorful"

// This table contains the "keypoints" of the colorgradient you want to generate.
// The position of each keypoint has to live in the range [0,1]
type Gradient []struct {
	Col colorful.Color
	Pos float64
}

// This is the meat of the gradient computation. It returns a HCL-blend between
// the two colors around `t`.
// Note: It relies heavily on the fact that the gradient keypoints are sorted.
func (self Gradient) GetInterpolatedColorFor(t float64) colorful.Color {
	for i := 0; i < len(self)-1; i++ {
		c1 := self[i]
		c2 := self[i+1]
		if c1.Pos <= t && t <= c2.Pos {
			// We are in between c1 and c2. Go blend them!
			t := (t - c1.Pos) / (c2.Pos - c1.Pos)
			return c1.Col.BlendHcl(c2.Col, t).Clamped()
		}
	}

	// Nothing found? Means we're at (or past) the last gradient keypoint.
	return self[len(self)-1].Col
}

// This is a very nice thing Golang forces you to do!
// It is necessary so that we can write out the literal of the colortable below.
func MustParseHex(s string) colorful.Color {
	c, err := colorful.Hex(s)
	if err != nil {
		panic("MustParseHex: " + err.Error())
	}
	return c
}

func NewGradient() Gradient {
	// The "keypoints" of the gradient.
	keypoints := Gradient{
		{MustParseHex("#9e0142"), 0.025},
		{MustParseHex("#d53e4f"), 0.045},
		{MustParseHex("#f46d43"), 0.065},
		{MustParseHex("#fdae61"), 0.085},
		{MustParseHex("#fee090"), 0.105},
		{MustParseHex("#ffffbf"), 0.130},
		{MustParseHex("#e6f598"), 0.155},
		{MustParseHex("#abdda4"), 0.180},
		{MustParseHex("#66c2a5"), 0.230},
		{MustParseHex("#3288bd"), 0.350},
		{MustParseHex("#5e4fa2"), 0.580},
	}

	return keypoints
}
