package ui

import (
	"fmt"
	"math"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
)

// finite guards a value from the core: NaN and Inf must never reach the screen.
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func fmtFloat(v float64, dec int) string {
	if !finite(v) {
		return "--"
	}
	return fmt.Sprintf("%.*f", dec, v)
}

// fmtDur renders a duration as an operator reads it: largest two units.
func fmtDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int64(d.Seconds() + 0.5)
	switch {
	case s < 10*60:
		return fmt.Sprintf("%d:%02d min", s/60, s%60)
	case s < 60*60:
		return fmt.Sprintf("%d min", s/60)
	case s < 24*3600:
		return fmt.Sprintf("%d h %d min", s/3600, (s%3600)/60)
	default:
		return fmt.Sprintf("%d d %d h", s/86400, (s%86400)/3600)
	}
}

// fmtTimer is a countdown: always minutes and seconds under an hour, so a
// protection timer can be seen moving.
func fmtTimer(sec float64) string {
	if !finite(sec) || sec < 0 {
		sec = 0
	}
	n := int64(sec + 0.5)
	if n >= 3600 {
		return fmt.Sprintf("%d h %d min", n/3600, (n%3600)/60)
	}
	return fmt.Sprintf("%d:%02d", n/60, n%60)
}

func fmtAge(sec float64) string {
	if !finite(sec) || sec < 0 {
		return "unknown time"
	}
	return fmtDur(time.Duration(sec * float64(time.Second)))
}

// readingGood is true only for a value that may be shown as live.
func readingGood(r api.Reading) bool {
	return r.Quality == api.QualityOK && finite(r.Value)
}

func qualityWord(q api.Quality) string {
	switch q {
	case api.QualityOK:
		return "OK"
	case api.QualityStale:
		return "STALE"
	case api.QualityFault:
		return "FAULT"
	case api.QualitySuspect:
		return "SUSPECT"
	case api.QualityOff:
		return "NOT FITTED"
	}
	return "UNKNOWN"
}

func sevWord(s api.Severity) string {
	switch s {
	case api.SevCritical:
		return "CRITICAL"
	case api.SevWarning:
		return "WARNING"
	}
	return "INFO"
}

// fmtWeight shows grams as whole numbers: HX711 noise makes more digits a lie.
func fmtWeight(v float64) string { return fmtFloat(v, 0) }
