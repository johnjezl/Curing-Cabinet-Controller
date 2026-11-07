//go:build demoinject

package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/johnjezl/curing-cabinet-controller/internal/ui"
)

func addInjectFlag(fs *flag.FlagSet) *string {
	return fs.String("inject", "", "FIFO path for scripted taps: lines of 'tap X Y' or 'hold X Y MS'. Off unless set.")
}

// startInject reads coordinates from a FIFO into the same input channel the
// touch reader feeds. Built only with -tags demoinject and active only when the
// -inject flag names a path.
func startInject(ctx context.Context, path *string, out chan<- ui.TouchEvent) {
	if path == nil || *path == "" {
		return
	}
	if err := syscall.Mkfifo(*path, 0o600); err != nil && !os.IsExist(err) {
		log.Fatalf("inject: %v", err)
	}
	log.Printf("uidemo: INJECTION ENABLED via %s", *path)
	go func() {
		for ctx.Err() == nil {
			f, err := os.OpenFile(*path, os.O_RDWR, 0)
			if err != nil {
				log.Printf("inject: %v", err)
				return
			}
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				if err := injectLine(sc.Text(), out); err != nil {
					log.Printf("inject: %v", err)
				}
			}
			f.Close()
		}
	}()
}

func addAutotapFlag(fs *flag.FlagSet) *string {
	return fs.String("autotap", "", "scripted taps, ';' separated: 'tap X Y', 'hold X Y MS' or 'sleep MS'. Off unless set.")
}

// startAutotap plays a tap script into the input channel, then logs its end. It
// exists for the rig, where a service has no stdin and no one touches the glass.
func startAutotap(ctx context.Context, script *string, out chan<- ui.TouchEvent) {
	if script == nil || *script == "" {
		return
	}
	log.Printf("uidemo: INJECTION ENABLED via -autotap")
	go func() {
		for _, step := range strings.Split(*script, ";") {
			if ctx.Err() != nil {
				return
			}
			w := strings.Fields(step)
			if len(w) == 2 && w[0] == "sleep" {
				ms, _ := strconv.Atoi(w[1])
				time.Sleep(time.Duration(ms) * time.Millisecond)
				continue
			}
			if err := injectLine(step, out); err != nil {
				log.Printf("inject: %v", err)
			}
			time.Sleep(450 * time.Millisecond) // past the bounce window, like a person
		}
		log.Printf("uidemo: autotap finished")
	}()
}

func injectLine(line string, out chan<- ui.TouchEvent) error {
	w := strings.Fields(line)
	if len(w) < 3 {
		return fmt.Errorf("bad line %q", line)
	}
	x, err1 := strconv.Atoi(w[1])
	y, err2 := strconv.Atoi(w[2])
	if err1 != nil || err2 != nil {
		return fmt.Errorf("bad coordinates %q", line)
	}
	d := 80 * time.Millisecond
	if w[0] == "hold" && len(w) > 3 {
		ms, err := strconv.Atoi(w[3])
		if err != nil {
			return err
		}
		d = time.Duration(ms) * time.Millisecond
	}
	log.Printf("inject %s x=%d y=%d", w[0], x, y)
	out <- ui.TouchEvent{Kind: ui.TouchDown, X: x, Y: y, At: time.Now()}
	time.Sleep(d)
	out <- ui.TouchEvent{Kind: ui.TouchUp, X: x, Y: y, At: time.Now()}
	return nil
}
