// Command fbshot converts a raw framebuffer dump (32 bpp XRGB8888, little
// endian) from stdin or a file into a PNG. It runs on the development machine.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
)

func main() {
	w := flag.Int("w", 800, "width in pixels")
	h := flag.Int("h", 480, "height in pixels")
	stride := flag.Int("stride", 3200, "bytes per line")
	in := flag.String("in", "-", "raw input file, - for stdin")
	out := flag.String("out", "", "output PNG")
	flag.Parse()
	if *out == "" {
		fatal(fmt.Errorf("-out is required"))
	}
	var r io.Reader = os.Stdin
	if *in != "-" {
		f, err := os.Open(*in)
		if err != nil {
			fatal(err)
		}
		defer f.Close()
		r = f
	}
	raw := make([]byte, *stride**h)
	if _, err := io.ReadFull(r, raw); err != nil {
		fatal(fmt.Errorf("framebuffer dump too short for %dx%d stride %d: %w", *w, *h, *stride, err))
	}
	img := image.NewRGBA(image.Rect(0, 0, *w, *h))
	for y := 0; y < *h; y++ {
		line := raw[y**stride:]
		for x := 0; x < *w; x++ {
			p := line[x*4:]
			img.SetRGBA(x, y, color.RGBA{R: p[2], G: p[1], B: p[0], A: 255})
		}
	}
	f, err := os.Create(*out)
	if err != nil {
		fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		fatal(err)
	}
	if err := f.Close(); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "fbshot:", err)
	os.Exit(1)
}
