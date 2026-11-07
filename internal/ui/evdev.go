package ui

// Multitouch protocol B decoder. Pure, so it is tested with raw event sequences.

const (
	evSyn = 0x00
	evKey = 0x01
	evAbs = 0x03

	synReport  = 0
	synDropped = 3

	absMTSlot       = 0x2f
	absMTPositionX  = 0x35
	absMTPositionY  = 0x36
	absMTTrackingID = 0x39

	maxSlots = 10
)

// RawEvent is one kernel input_event.
type RawEvent struct {
	Type, Code uint16
	Value      int32
}

type slot struct {
	active bool
	x, y   int32
}

// Decoder turns kernel events into one-finger gestures. Three or more contacts
// are a palm or a wet cloth, and cancel the gesture until the surface is clear.
type Decoder struct {
	slots        [maxSlots]slot
	cur          int
	primary      int // slot being followed, -1 if none
	cancelled    bool
	lastX, lastY int
	syncing      bool
	// Scale from device coordinates to screen pixels.
	MinX, MaxX, MinY, MaxY int32
	W, H                   int
}

// NewDecoder builds a decoder for a device's coordinate range.
func NewDecoder(minX, maxX, minY, maxY int32, w, h int) *Decoder {
	if maxX <= minX {
		maxX = minX + int32(w) - 1
	}
	if maxY <= minY {
		maxY = minY + int32(h) - 1
	}
	return &Decoder{primary: -1, MinX: minX, MaxX: maxX, MinY: minY, MaxY: maxY, W: w, H: h}
}

func (d *Decoder) scale(x, y int32) (int, int) {
	sx := int(int64(x-d.MinX) * int64(d.W-1) / int64(d.MaxX-d.MinX))
	sy := int(int64(y-d.MinY) * int64(d.H-1) / int64(d.MaxY-d.MinY))
	if sx < 0 {
		sx = 0
	}
	if sy < 0 {
		sy = 0
	}
	if sx >= d.W {
		sx = d.W - 1
	}
	if sy >= d.H {
		sy = d.H - 1
	}
	return sx, sy
}

// Feed consumes one kernel event and returns any gesture events it completes.
func (d *Decoder) Feed(e RawEvent) []TouchEvent {
	switch e.Type {
	case evAbs:
		if d.syncing {
			return nil
		}
		switch e.Code {
		case absMTSlot:
			if e.Value >= 0 && e.Value < maxSlots {
				d.cur = int(e.Value)
			}
		case absMTTrackingID:
			d.slots[d.cur].active = e.Value >= 0
		case absMTPositionX:
			d.slots[d.cur].x = e.Value
		case absMTPositionY:
			d.slots[d.cur].y = e.Value
		}
	case evSyn:
		switch e.Code {
		case synDropped:
			// Events were lost. Abandon the gesture and resync on the next report.
			d.syncing = true
			wasDown := d.primary >= 0
			d.primary = -1
			d.slots = [maxSlots]slot{}
			d.cancelled = true
			if wasDown {
				return []TouchEvent{{Kind: TouchCancel}}
			}
		case synReport:
			if d.syncing {
				d.syncing = false
				return nil
			}
			return d.report()
		}
	}
	return nil
}

func (d *Decoder) report() []TouchEvent {
	n, first := 0, -1
	for i, s := range d.slots {
		if s.active {
			n++
			if first < 0 {
				first = i
			}
		}
	}
	if n == 0 {
		wasDown := d.primary >= 0
		d.primary = -1
		d.cancelled = false
		if wasDown {
			return []TouchEvent{{Kind: TouchUp, X: d.lastX, Y: d.lastY}}
		}
		return nil
	}
	if d.cancelled {
		return nil
	}
	if n >= 3 {
		d.cancelled = true
		wasDown := d.primary >= 0
		d.primary = -1
		if wasDown {
			return []TouchEvent{{Kind: TouchCancel}}
		}
		return nil
	}
	if d.primary < 0 {
		d.primary = first
		d.lastX, d.lastY = d.scale(d.slots[first].x, d.slots[first].y)
		return []TouchEvent{{Kind: TouchDown, X: d.lastX, Y: d.lastY}}
	}
	if !d.slots[d.primary].active {
		// The followed finger lifted while another stayed: that ends this gesture.
		d.cancelled = true
		d.primary = -1
		return []TouchEvent{{Kind: TouchUp, X: d.lastX, Y: d.lastY}}
	}
	x, y := d.scale(d.slots[d.primary].x, d.slots[d.primary].y)
	if x != d.lastX || y != d.lastY {
		d.lastX, d.lastY = x, y
		return []TouchEvent{{Kind: TouchMove, X: x, Y: y}}
	}
	return nil
}
