package ui

import "image"

// NoticeDisplay is a display that can show a whole frame without going through
// the render loop. The freeze watchdog and the stop path use it.
type NoticeDisplay interface {
	PaintNotice(*image.RGBA) error
}

func noticeFrame(title, line1, line2 string) *image.RGBA {
	c := NewCanvas()
	c.Fill(c.Img.Bounds(), colCrit)
	c.Hatch(rect(0, 0, ScreenW, 28), 24, 12, colBlack)
	c.Hatch(rect(0, ScreenH-28, ScreenW, ScreenH), 24, 12, colBlack)
	c.Text(rect(24, 60, 776, 180), title, Style{Font: FontBold, Size: 76, Color: colText, H: Center, MinSize: 44})
	c.Wrap(rect(40, 190, 760, 330), line1, Style{Font: FontBold, Size: 36, Color: colText, H: Center, MinSize: 24}, 3)
	c.Wrap(rect(40, 335, 760, 445), line2, Style{Size: 30, Color: colText, H: Center, MinSize: 20}, 3)
	return c.Img
}

// simNoticeFrame is the notice a simulation leaves on the panel: purple, with the
// simulation band top and bottom, and no instruction to go and check a cabinet that
// was never being controlled (ADR 0406).
func simNoticeFrame(title, line1 string) *image.RGBA {
	c := NewCanvas()
	c.Fill(c.Img.Bounds(), colSim)
	for _, r := range []image.Rectangle{rect(0, 0, ScreenW, 44), rect(0, ScreenH-44, ScreenW, ScreenH)} {
		drawSimBandText(c, r, "SIMULATION · NOT A REAL CABINET", 32)
	}
	c.Text(rect(24, 70, 776, 180), title, Style{Font: FontBold, Size: 68, Color: colText, H: Center, MinSize: 40})
	c.Wrap(rect(40, 190, 760, 320), line1, Style{Font: FontBold, Size: 34, Color: colText, H: Center, MinSize: 22}, 3)
	c.Wrap(rect(40, 325, 760, 420), "No cabinet was being controlled by this simulation.", Style{Size: 30, Color: colText, H: Center, MinSize: 20}, 2)
	return c.Img
}

// FrozenFrame replaces the screen when the render loop has stopped drawing.
func FrozenFrame() *image.RGBA { return FrozenFrameFor(false) }

// FrozenFrameFor is FrozenFrame, marked as a simulation when sim is set.
func FrozenFrameFor(sim bool) *image.RGBA {
	if sim {
		return simNoticeFrame("SIMULATION FROZEN", "The simulated readings were removed because they were no longer live.")
	}
	return noticeFrame("PANEL FROZEN", "The last readings were removed because they were no longer live.",
		"Check the cabinet with a separate thermometer. Control may still be running. Restart the controller.")
}

// StoppedFrame replaces the screen when the panel stops, so an old frame does
// not stay up looking like a live reading.
func StoppedFrame() *image.RGBA { return StoppedFrameFor(false) }

// StoppedFrameFor is StoppedFrame, marked as a simulation when sim is set.
func StoppedFrameFor(sim bool) *image.RGBA {
	if sim {
		return simNoticeFrame("SIMULATION STOPPED", "This screen is not showing live readings.")
	}
	return noticeFrame("PANEL STOPPED", "This screen is not showing live readings.",
		"Check the cabinet with a separate thermometer. Restart the controller if it does not return within a minute.")
}
