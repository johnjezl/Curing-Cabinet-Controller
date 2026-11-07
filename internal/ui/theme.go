package ui

import (
	"image/color"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// Physical scale of the 108 x 65 mm, 800 x 480 panel.
const (
	ScreenW  = 800
	ScreenH  = 480
	PxPerMM  = 800.0 / 108.0
	MinTapPx = 89 // 12 mm, for a gloved finger: the audit fails any smaller target.
)

func rgb(r, g, b uint8) color.RGBA { return color.RGBA{r, g, b, 255} }

// Palette. Colours are fixed; every text pair is checked by the audit in tests.
var (
	colBG       = rgb(0x08, 0x0B, 0x0F)
	colPanel    = rgb(0x14, 0x1A, 0x21)
	colPanel2   = rgb(0x24, 0x2D, 0x37)
	colLine     = rgb(0x48, 0x56, 0x64)
	colText     = rgb(0xF5, 0xF8, 0xFA)
	colDim      = rgb(0xC0, 0xCB, 0xD6)
	colMuted    = rgb(0x9A, 0xA8, 0xB5)
	colBlack    = rgb(0, 0, 0)
	colOK       = rgb(0x0B, 0x74, 0x3F)
	colWarn     = rgb(0xFF, 0xB0, 0x20)
	colCrit     = rgb(0xC4, 0x10, 0x2C)
	colCritText = rgb(0xFF, 0x6B, 0x78) // critical digits on a dark tile
	colInfo     = rgb(0x1A, 0x62, 0xC4)
	colSim      = rgb(0x8A, 0x1F, 0xD1)
	colSimLite  = rgb(0xE3, 0xC2, 0xFF)
	colOn       = rgb(0xDD, 0xEA, 0xF5)
	colHatch    = rgb(0x2C, 0x33, 0x3B)
	colTrend    = rgb(0x8F, 0xD3, 0xFF)
	colBtn      = rgb(0x2E, 0x3A, 0x47)
	colBtnDown  = rgb(0x6C, 0x80, 0x94)
)

// FontID selects one of the embedded faces.
type FontID int

const (
	FontRegular FontID = iota
	FontBold
)

var (
	fontOnce  sync.Once
	fontTable [2]*sfnt.Font

	faceMu    sync.Mutex
	faceCache = map[faceKey]*cachedFace{}
)

type faceKey struct {
	id   FontID
	size int
}

func loadFonts() {
	fontOnce.Do(func() {
		for i, data := range [][]byte{goregular.TTF, gobold.TTF} {
			f, err := opentype.Parse(data)
			if err != nil {
				panic(err) // embedded data: only possible with a broken build
			}
			fontTable[i] = f
		}
	})
}

// getFace returns a glyph-caching face. Faces are shared; the mutex protects
// only the table, and a face is only used by the one rendering goroutine in
// production (tests may render concurrently, so the glyph cache has its own lock).
func getFace(id FontID, size int) *cachedFace {
	loadFonts()
	faceMu.Lock()
	defer faceMu.Unlock()
	k := faceKey{id, size}
	if f := faceCache[k]; f != nil {
		return f
	}
	face, err := opentype.NewFace(fontTable[id], &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		panic(err)
	}
	f := &cachedFace{face: face, glyphs: map[rune]*glyph{}, metrics: face.Metrics()}
	faceCache[k] = f
	return f
}

// hasGlyph reports whether the face can actually draw r.
func hasGlyph(id FontID, r rune) bool {
	loadFonts()
	var b sfnt.Buffer
	i, err := fontTable[id].GlyphIndex(&b, r)
	return err == nil && i != 0
}
