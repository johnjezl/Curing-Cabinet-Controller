# ADR 0301: Panel rendering stack

Status: accepted

## Options
1. X or Wayland with a toolkit. Rich, but adds a display server, CGO and a large
   surface that can fail next to food.
2. DRM/KMS directly. Page flips avoid tearing, but it takes the display from fbdev
   tools and needs a master lock.
3. fbdev: mmap /dev/fb0 and draw with Go's image packages and x/image fonts.

## Decision
Option 3. It is pure Go and static, and it matches the constraint of no display
server. Frames are composed on an in-memory RGBA image, then rows that changed are
copied to the device. A full repaint every 5 s heals any stray write.

The pixel layout is read from the driver, not assumed. The rig reports XRGB:
transp offset 0, length 0. Writing alpha to that offset overwrote blue, which
turned the whole panel blue. Only the rig's own framebuffer showed this, and a test
now covers it.

Fonts are Go Regular and Go Bold, embedded through x/image. Licence is BSD-style.
Glyphs are cached per size.

## Consequences
fbdev has no vsync, so a frame can tear during a copy. Changes are small and at
most 20 per second. Row diffing keeps the copy short. Tearing has not been
observed because no one has looked at the glass during a redraw.
