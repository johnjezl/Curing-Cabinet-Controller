package ui

import (
	"fmt"
	"image"
	"math"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

var trendSpans = []struct {
	label string
	d     time.Duration
}{{"24 h", 24 * time.Hour}, {"7 d", 7 * 24 * time.Hour}, {"ALL", 0}}

// drawTrend draws small multiples on separate axes, one measure per plot. Gaps
// in valid data stay gaps: they are hatched, never joined by a line.
func (p *Panel) drawTrend(c *Canvas) {
	s := &p.snap
	colW := 98
	bh := (bodyBot - bodyTop - 2*4) / 3
	for i, sp := range trendSpans {
		y := bodyTop + i*(bh+4)
		st := btnNormal
		st.size = 26
		if p.trendSpan == sp.d {
			st.bg, st.fg = colOn, colBlack
		}
		d := sp.d
		p.button(c, rect(mR-colW, y, mR, y+bh), "trend.span."+sp.label, sp.label, st, func() { p.trendSpan = d })
	}
	plotR := mR - colW - 4
	end := s.Now
	start := end.Add(-p.trendSpan)
	if p.trendSpan == 0 {
		start = end
		if len(s.Trend) > 0 {
			start = s.Trend[0].T
		}
		if !start.Before(end) {
			start = end.Add(-time.Hour)
		}
	}
	type series struct {
		name, unit string
		dec        int
		get        func(api.Sample) float64
		set        float64
		hasSet     bool
		h          int
		now        api.Reading
	}
	list := []series{
		{"TEMP", "°C", 1, func(a api.Sample) float64 { return a.TempC }, s.Setpoint.TempC, true, 94, s.Temp},
		{"HUMIDITY", "%", 0, func(a api.Sample) float64 { return a.RHPct }, s.Setpoint.RHPct, true, 94, s.RH},
		{"AIR", "m/s", 2, func(a api.Sample) float64 { return a.AirMS }, s.Setpoint.AirMS, true, 56, s.Air},
		{"WEIGHT", "g", 0, func(a api.Sample) float64 { return a.WeightG }, 0, false, 56, s.Weight},
	}
	y := bodyTop
	for _, se := range list {
		r := rect(mX, y, plotR, y+se.h)
		p.plot(c, r, se.name, se.unit, se.dec, se.get, se.set, se.hasSet, start, end, se.now)
		y += se.h + 2
	}
	axis := rect(mX+62, y, plotR-6, bodyBot)
	span := end.Sub(start)
	c.Text(axis, "-"+fmtSpan(span), Style{Size: 16, Color: colDim, H: Left, MinSize: 12})
	c.Text(axis, "-"+fmtSpan(span/2), Style{Size: 16, Color: colDim, H: Center, MinSize: 12})
	endLabel := "now"
	if p.Stalled() {
		endLabel = "last data " + p.clockText() + ", NOT LIVE"
	}
	c.Text(axis, endLabel, Style{Size: 16, Color: colDim, H: Right, MinSize: 12})
}

func fmtSpan(d time.Duration) string {
	if d >= 48*time.Hour {
		return fmt.Sprintf("%.0f d", d.Hours()/24)
	}
	return fmt.Sprintf("%.0f h", d.Hours())
}

func (p *Panel) plot(c *Canvas, r image.Rectangle, name, unit string, dec int, get func(api.Sample) float64, set float64, hasSet bool, start, end time.Time, cur api.Reading) {
	c.Round(r, 4, colPanel)
	gutter := 62
	head := 22
	area := rect(r.Min.X+gutter, r.Min.Y+head, r.Max.X-6, r.Max.Y-3)
	var pts []api.Sample
	for _, a := range p.snap.Trend {
		if !a.T.Before(start) && !a.T.After(end) {
			pts = append(pts, a)
		}
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	n := 0
	for _, a := range pts {
		v := get(a)
		if a.Valid && finite(v) {
			lo, hi = math.Min(lo, v), math.Max(hi, v)
			n++
		}
	}
	title := name
	if n > 0 {
		title += fmt.Sprintf("  min %s  max %s %s", fmtFloat(lo, dec), fmtFloat(hi, dec), unit)
	}
	if hasSet && finite(set) {
		title += "  set " + fmtFloat(set, dec)
	}
	if p.Stalled() {
		title += "  NOT LIVE"
	}
	c.Fill(area, colBG)
	if cur.Quality != api.QualityOff {
		c.Text(rect(r.Min.X+8, r.Min.Y+1, r.Max.X-6, r.Min.Y+head), title, Style{Font: FontBold, Size: 17, Color: colText, MinSize: 12})
	}
	if cur.Quality == api.QualityOff {
		// No sensor fitted: any samples are placeholders, not a flat measurement.
		c.Text(rect(r.Min.X+8, r.Min.Y+1, r.Max.X-6, r.Min.Y+head), name, Style{Font: FontBold, Size: 17, Color: colText, MinSize: 12})
		c.Text(area, "NOT FITTED", Style{Font: FontBold, Size: 20, Color: colMuted, H: Center, MinSize: 13})
		return
	}
	if n == 0 {
		c.Hatch(area, 16, 6, colHatch)
		c.Text(area, "NO VALID DATA", Style{Font: FontBold, Size: 20, Color: colWarn, H: Center, MinSize: 13})
		return
	}
	if hasSet && finite(set) {
		lo, hi = math.Min(lo, set), math.Max(hi, set)
	}
	// A minimum span stops normal cycling from filling the plot and looking violent.
	band := func(up, down float64) (float64, float64) {
		return set + up, set - down
	}
	var limHi, limLo float64
	showLim := false
	lim := p.snap.Limits
	switch name {
	case "TEMP":
		limHi, limLo = band(lim.TempHighDelta, lim.TempLowDelta)
		showLim = lim.TempHighDelta > 0
	case "HUMIDITY":
		limHi, limLo = band(lim.RHHighDelta, lim.RHLowDelta)
		showLim = lim.RHHighDelta > 0
	}
	if showLim && finite(limHi) && finite(limLo) {
		lo, hi = math.Min(lo, limLo), math.Max(hi, limHi)
	}
	if hi-lo < math.Pow(10, float64(-dec)) {
		pad := math.Pow(10, float64(-dec))
		lo, hi = lo-pad, hi+pad
	}
	pad := (hi - lo) * 0.08
	lo, hi = lo-pad, hi+pad
	X := func(t time.Time) float64 {
		return float64(area.Min.X) + float64(t.Sub(start))/float64(end.Sub(start))*float64(area.Dx()-1)
	}
	Y := func(v float64) float64 {
		return float64(area.Max.Y-1) - (v-lo)/(hi-lo)*float64(area.Dy()-1)
	}
	c.Text(rect(r.Min.X+2, area.Min.Y-2, r.Min.X+gutter-4, area.Min.Y+16), fmtFloat(hi, dec), Style{Size: 15, Color: colDim, H: Right, MinSize: 11})
	c.Text(rect(r.Min.X+2, area.Max.Y-16, r.Min.X+gutter-4, area.Max.Y+2), fmtFloat(lo, dec), Style{Size: 15, Color: colDim, H: Right, MinSize: 11})
	// Recessive midline.
	c.Fill(rect(area.Min.X, area.Min.Y+area.Dy()/2, area.Max.X, area.Min.Y+area.Dy()/2+1), colPanel2)
	if hasSet && finite(set) {
		y := Y(set)
		for x := area.Min.X; x < area.Max.X; x += 12 {
			c.Fill(rect(x, int(y), x+6, int(y)+1), colDim)
		}
	}
	if showLim && finite(limHi) && finite(limLo) {
		for _, v := range []float64{limHi, limLo} {
			y := Y(v)
			for x := area.Min.X; x < area.Max.X; x += 6 {
				c.Fill(rect(x, int(y), x+2, int(y)+1), colWarn)
			}
		}
	}
	// Many samples per pixel: draw the range in each column, so one spike stays
	// visible and a cycling signal does not alias into a solid block of zigzags.
	if len(pts) > 2*area.Dx() {
		p.plotEnvelope(c, area, pts, get, X, Y)
		return
	}
	// A gap wider than this many typical sample periods is a real gap.
	maxGap := medianGap(pts) * 3
	var prev *api.Sample
	gapFrom := start
	for i := range pts {
		a := pts[i]
		v := get(a)
		good := a.Valid && finite(v)
		if !good {
			prev = nil
			continue
		}
		if prev == nil || a.T.Sub(prev.T) > maxGap {
			if a.T.Sub(gapFrom) > maxGap {
				g := rect(int(X(gapFrom)), area.Min.Y, int(X(a.T)), area.Max.Y)
				c.Hatch(g, 12, 4, colHatch)
			}
		} else {
			c.Line(X(prev.T), Y(get(*prev)), X(a.T), Y(v), 2, colTrend)
		}
		cp := a
		prev = &cp
		gapFrom = a.T
	}
	if end.Sub(gapFrom) > maxGap {
		c.Hatch(rect(int(X(gapFrom)), area.Min.Y, area.Max.X, area.Max.Y), 12, 4, colHatch)
	}
}

func (p *Panel) plotEnvelope(c *Canvas, area image.Rectangle, pts []api.Sample, get func(api.Sample) float64, X func(time.Time) float64, Y func(float64) float64) {
	type col struct {
		lo, hi float64
		n      int
	}
	cols := make([]col, area.Dx())
	for _, a := range pts {
		v := get(a)
		if !a.Valid || !finite(v) {
			continue
		}
		i := int(X(a.T)) - area.Min.X
		if i < 0 || i >= len(cols) {
			continue
		}
		e := &cols[i]
		if e.n == 0 || v < e.lo {
			e.lo = v
		}
		if e.n == 0 || v > e.hi {
			e.hi = v
		}
		e.n++
	}
	for i, e := range cols {
		x := area.Min.X + i
		if e.n == 0 {
			c.Fill(rect(x, area.Min.Y, x+1, area.Max.Y), colHatch)
			continue
		}
		y0, y1 := int(Y(e.hi)), int(Y(e.lo))+1
		c.Fill(rect(x, y0, x+1, y1+1), colTrend)
	}
}

func medianGap(pts []api.Sample) time.Duration {
	if len(pts) < 2 {
		return time.Hour
	}
	// A cheap robust estimate: the mean of the middle half would need a sort; the
	// core downsamples evenly, so the smallest repeated step is good enough.
	best := time.Duration(math.MaxInt64)
	for i := 1; i < len(pts); i++ {
		d := pts[i].T.Sub(pts[i-1].T)
		if d > 0 && d < best {
			best = d
		}
	}
	if best == time.Duration(math.MaxInt64) {
		return time.Hour
	}
	return best
}
