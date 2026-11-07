package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

type glyph struct {
	mask    *image.Alpha
	box     image.Rectangle // relative to the dot
	advance int
}

type cachedFace struct {
	mu      sync.Mutex
	face    font.Face
	glyphs  map[rune]*glyph
	metrics font.Metrics
}

func (f *cachedFace) glyph(r rune) *glyph {
	f.mu.Lock()
	defer f.mu.Unlock()
	if g := f.glyphs[r]; g != nil {
		return g
	}
	dr, m, mp, adv, ok := f.face.Glyph(fixed.P(0, 0), r)
	if !ok {
		dr, m, mp, adv, _ = f.face.Glyph(fixed.P(0, 0), '?')
	}
	a := image.NewAlpha(image.Rect(0, 0, dr.Dx(), dr.Dy()))
	if m != nil {
		draw.Draw(a, a.Bounds(), m, mp, draw.Src)
	}
	g := &glyph{mask: a, box: dr, advance: adv.Round()}
	f.glyphs[r] = g
	return g
}

func (f *cachedFace) measure(s string) int {
	w := 0
	for _, r := range s {
		w += f.glyph(r).advance
	}
	return w
}

// inkBounds returns the union of glyph ink relative to the dot.
func (f *cachedFace) inkBounds(s string) image.Rectangle {
	x := 0
	var u image.Rectangle
	for _, r := range s {
		g := f.glyph(r)
		if !g.box.Empty() {
			u = u.Union(g.box.Add(image.Pt(x, 0)))
		}
		x += g.advance
	}
	return u
}

// HAlign and VAlign position text inside its box.
type HAlign int
type VAlign int

const (
	Left HAlign = iota
	Center
	Right
)
const (
	Middle VAlign = iota
	Top
	Bottom
)

// Style describes one run of text.
type Style struct {
	Font    FontID
	Size    int
	Color   color.RGBA
	H       HAlign
	V       VAlign
	MinSize int // smallest shrink allowed; 0 means 80 % of Size
}

// TextRec is one drawn run, recorded when auditing.
type TextRec struct {
	S        string
	Box      image.Rectangle
	Ink      image.Rectangle
	Size     int
	Contrast float64
}

// Audit collects layout faults while drawing. Nil in production.
type Audit struct {
	Texts    []TextRec
	Problems []string
	Digits   map[string]int // label -> digit height in pixels
}

func (a *Audit) problem(format string, args ...any) {
	a.Problems = append(a.Problems, fmt.Sprintf(format, args...))
}

// Canvas is the drawing surface: an 800x480 RGBA image plus an optional audit.
type Canvas struct {
	Img   *image.RGBA
	Audit *Audit
}

// NewCanvas allocates a screen-sized canvas.
func NewCanvas() *Canvas {
	return &Canvas{Img: image.NewRGBA(image.Rect(0, 0, ScreenW, ScreenH))}
}

func rect(x0, y0, x1, y1 int) image.Rectangle { return image.Rect(x0, y0, x1, y1) }

// Fill paints a solid rectangle.
func (c *Canvas) Fill(r image.Rectangle, col color.RGBA) {
	draw.Draw(c.Img, r, image.NewUniform(col), image.Point{}, draw.Src)
}

// Round paints a solid rounded rectangle with anti-aliased corners.
func (c *Canvas) Round(r image.Rectangle, rad int, col color.RGBA) {
	r = r.Intersect(c.Img.Bounds())
	if r.Empty() {
		return
	}
	if rad*2 > r.Dx() {
		rad = r.Dx() / 2
	}
	if rad*2 > r.Dy() {
		rad = r.Dy() / 2
	}
	if rad <= 0 {
		c.Fill(r, col)
		return
	}
	c.Fill(rect(r.Min.X+rad, r.Min.Y, r.Max.X-rad, r.Max.Y), col)
	c.Fill(rect(r.Min.X, r.Min.Y+rad, r.Min.X+rad, r.Max.Y-rad), col)
	c.Fill(rect(r.Max.X-rad, r.Min.Y+rad, r.Max.X, r.Max.Y-rad), col)
	fr := float64(rad)
	for dy := 0; dy < rad; dy++ {
		for dx := 0; dx < rad; dx++ {
			d := math.Hypot(fr-float64(dx)-0.5, fr-float64(dy)-0.5)
			a := fr - d + 0.5
			if a <= 0 {
				continue
			}
			if a > 1 {
				a = 1
			}
			for _, p := range []image.Point{
				{r.Min.X + dx, r.Min.Y + dy}, {r.Max.X - 1 - dx, r.Min.Y + dy},
				{r.Min.X + dx, r.Max.Y - 1 - dy}, {r.Max.X - 1 - dx, r.Max.Y - 1 - dy},
			} {
				c.blend(p.X, p.Y, col, a)
			}
		}
	}
}

func (c *Canvas) blend(x, y int, col color.RGBA, a float64) {
	if !(image.Pt(x, y).In(c.Img.Bounds())) {
		return
	}
	o := c.Img.PixOffset(x, y)
	p := c.Img.Pix[o : o+4 : o+4]
	ia := 1 - a
	p[0] = uint8(float64(col.R)*a + float64(p[0])*ia + 0.5)
	p[1] = uint8(float64(col.G)*a + float64(p[1])*ia + 0.5)
	p[2] = uint8(float64(col.B)*a + float64(p[2])*ia + 0.5)
	p[3] = 255
}

// Stroke outlines a rounded rectangle.
func (c *Canvas) Stroke(r image.Rectangle, rad, w int, col, inner color.RGBA) {
	c.Round(r, rad, col)
	c.Round(r.Inset(w), rad-w, inner)
}

// Hatch overlays 45 degree stripes. Stale and faulty values sit on this so they
// cannot be mistaken for a live reading even in peripheral vision.
func (c *Canvas) Hatch(r image.Rectangle, period, width int, col color.RGBA) {
	r = r.Intersect(c.Img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if (x+y)%period < width {
				c.Img.SetRGBA(x, y, col)
			}
		}
	}
}

// Line draws an anti-aliased line of width w.
func (c *Canvas) Line(x0, y0, x1, y1, w float64, col color.RGBA) {
	dx, dy := x1-x0, y1-y0
	l := math.Hypot(dx, dy)
	if l == 0 {
		return
	}
	nx, ny := -dy/l*w/2, dx/l*w/2
	c.Poly([][2]float64{{x0 + nx, y0 + ny}, {x1 + nx, y1 + ny}, {x1 - nx, y1 - ny}, {x0 - nx, y0 - ny}}, col)
}

// Poly fills a polygon with anti-aliasing.
func (c *Canvas) Poly(pts [][2]float64, col color.RGBA) {
	minX, minY, maxX, maxY := pts[0][0], pts[0][1], pts[0][0], pts[0][1]
	for _, p := range pts {
		minX, maxX = math.Min(minX, p[0]), math.Max(maxX, p[0])
		minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
	}
	r := image.Rect(int(minX)-1, int(minY)-1, int(maxX)+2, int(maxY)+2).Intersect(c.Img.Bounds())
	if r.Empty() {
		return
	}
	ra := vector.NewRasterizer(r.Dx(), r.Dy())
	ox, oy := float32(r.Min.X), float32(r.Min.Y)
	ra.MoveTo(float32(pts[0][0])-ox, float32(pts[0][1])-oy)
	for _, p := range pts[1:] {
		ra.LineTo(float32(p[0])-ox, float32(p[1])-oy)
	}
	ra.ClosePath()
	ra.Draw(c.Img, r, image.NewUniform(col), image.Point{})
}

// Text draws one line in box, shrinking to MinSize and then truncating. A
// truncation is an audit problem: fixtures must never reach it.
func (c *Canvas) Text(box image.Rectangle, s string, st Style) {
	if s == "" {
		return
	}
	min := st.MinSize
	if min == 0 {
		min = st.Size * 8 / 10
	}
	size := st.Size
	f := getFace(st.Font, size)
	for size > min && f.measure(s) > box.Dx() {
		size--
		f = getFace(st.Font, size)
	}
	if f.measure(s) > box.Dx() {
		if c.Audit != nil {
			c.Audit.problem("text truncated: %q in %v at %dpx", s, box, size)
		}
		rs := []rune(s)
		for len(rs) > 1 && f.measure(string(rs)+"…") > box.Dx() {
			rs = rs[:len(rs)-1]
		}
		s = string(rs) + "…"
	}
	c.drawRun(box, s, f, size, st)
}

func (c *Canvas) drawRun(box image.Rectangle, s string, f *cachedFace, size int, st Style) {
	w := f.measure(s)
	x := box.Min.X
	switch st.H {
	case Center:
		x += (box.Dx() - w) / 2
	case Right:
		x = box.Max.X - w
	}
	asc, desc := f.metrics.Ascent.Round(), f.metrics.Descent.Round()
	y := box.Min.Y + asc
	switch st.V {
	case Middle:
		y = box.Min.Y + (box.Dy()+asc-desc)/2
	case Bottom:
		y = box.Max.Y - desc
	}
	c.drawGlyphs(x, y, s, f, size, st.Font, st.Color, box)
}

func (c *Canvas) drawGlyphs(x, y int, s string, f *cachedFace, size int, id FontID, col color.RGBA, box image.Rectangle) {
	if c.Audit != nil {
		ink := f.inkBounds(s).Add(image.Pt(x, y))
		for _, r := range s {
			if r != ' ' && !hasGlyph(id, r) {
				c.Audit.problem("missing glyph %q in %q", r, s)
			}
		}
		if !ink.In(c.Img.Bounds()) {
			c.Audit.problem("text off screen: %q ink %v", s, ink)
		}
		if !ink.In(box.Inset(-2)) {
			c.Audit.problem("text outside its box: %q ink %v box %v", s, ink, box)
		}
		c.Audit.Texts = append(c.Audit.Texts, TextRec{S: s, Box: box, Ink: ink, Size: size, Contrast: c.worstContrast(ink, col)})
	}
	uni := image.NewUniform(col)
	for _, r := range s {
		g := f.glyph(r)
		if !g.box.Empty() {
			d := g.box.Add(image.Pt(x, y))
			draw.DrawMask(c.Img, d, uni, image.Point{}, g.mask, image.Point{}, draw.Over)
		}
		x += g.advance
	}
}

// Digits draws a number so that its digit height in pixels is close to height,
// shrinking only if it would not fit box.Dx(). It returns the drawn width.
func (c *Canvas) Digits(box image.Rectangle, s string, height int, col color.RGBA, h HAlign, label string) image.Rectangle {
	size := int(float64(height)/digitRatio() + 0.5)
	f := getFace(FontBold, size)
	for size > 12 && f.measure(s) > box.Dx() {
		size--
		f = getFace(FontBold, size)
	}
	ink := f.inkBounds("0")
	dh := ink.Dy()
	w := f.measure(s)
	x := box.Min.X
	switch h {
	case Center:
		x += (box.Dx() - w) / 2
	case Right:
		x = box.Max.X - w
	}
	y := box.Min.Y + (box.Dy()+dh)/2
	c.drawGlyphs(x, y, s, f, size, FontBold, col, box)
	if c.Audit != nil && label != "" {
		if c.Audit.Digits == nil {
			c.Audit.Digits = map[string]int{}
		}
		c.Audit.Digits[label] = dh
	}
	return image.Rect(x, y-dh, x+w, y)
}

var (
	digitRatioOnce sync.Once
	digitRatioVal  float64
)

// digitRatio is the height of a digit as a fraction of the font size.
func digitRatio() float64 {
	digitRatioOnce.Do(func() {
		f := getFace(FontBold, 200)
		digitRatioVal = float64(f.inkBounds("0").Dy()) / 200
	})
	return digitRatioVal
}

// Wrap breaks s into at most maxLines lines that fit box, shrinking the font
// down to MinSize first. Honest overflow is an audit problem.
func (c *Canvas) Wrap(box image.Rectangle, s string, st Style, maxLines int) int {
	if s == "" {
		return 0
	}
	min := st.MinSize
	if min == 0 {
		min = st.Size * 8 / 10
	}
	var lines []string
	var f *cachedFace
	var lh int
	size := st.Size
	for ; ; size-- {
		f = getFace(st.Font, size)
		lh = f.metrics.Height.Round()
		lines = wrapLines(f, s, box.Dx())
		fits := len(lines) <= maxLines && len(lines)*lh <= box.Dy()
		for _, l := range lines { // one long word can exceed the width on its own
			if f.measure(l) > box.Dx() {
				fits = false
			}
		}
		if fits || size <= min {
			break
		}
	}
	allowed := maxLines
	if n := box.Dy() / lh; n < allowed {
		allowed = n
	}
	if allowed < 1 {
		allowed = 1
	}
	if len(lines) > allowed {
		if c.Audit != nil {
			c.Audit.problem("wrap overflow: %q in %v (%d lines at %dpx)", s, box, len(lines), size)
		}
		lines = lines[:allowed]
		last := []rune(lines[allowed-1])
		for len(last) > 1 && f.measure(string(last)+"…") > box.Dx() {
			last = last[:len(last)-1]
		}
		lines[allowed-1] = string(last) + "…"
	}
	y := box.Min.Y
	total := len(lines) * lh
	switch st.V {
	case Middle:
		y += (box.Dy() - total) / 2
	case Bottom:
		y = box.Max.Y - total
	}
	line := st
	line.V = Top
	for _, l := range lines {
		c.drawRun(rect(box.Min.X, y, box.Max.X, y+lh), l, f, size, line)
		y += lh
	}
	return len(lines) * lh
}

func wrapLines(f *cachedFace, s string, width int) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		cur := ""
		for _, w := range words {
			try := w
			if cur != "" {
				try = cur + " " + w
			}
			if f.measure(try) <= width || cur == "" {
				cur = try
				continue
			}
			out = append(out, cur)
			cur = w
		}
		out = append(out, cur)
	}
	return out
}

// Luminance per WCAG 2.
func luminance(c color.RGBA) float64 {
	ch := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}

// Contrast is the WCAG contrast ratio between two colours.
func Contrast(a, b color.RGBA) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func (c *Canvas) worstContrast(ink image.Rectangle, fg color.RGBA) float64 {
	ink = ink.Intersect(c.Img.Bounds())
	worst := 21.0
	if ink.Empty() {
		return worst
	}
	stepX, stepY := ink.Dx()/12+1, ink.Dy()/6+1
	for y := ink.Min.Y; y < ink.Max.Y; y += stepY {
		for x := ink.Min.X; x < ink.Max.X; x += stepX {
			if v := Contrast(fg, c.Img.RGBAAt(x, y)); v < worst {
				worst = v
			}
		}
	}
	return worst
}
