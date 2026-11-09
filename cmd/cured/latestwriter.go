package main

import (
	"sync"
	"time"
)

// slowWrite is how long a save may take before the journal says so: an SD card can
// stall for seconds, and that is worth knowing.
const slowWrite = time.Second

// latestWriter saves one small file on its own goroutine. Put never waits for the
// disk, so the control loop can hand it a value and go on. Only the newest content
// matters: values put while a write is under way replace each other, and the newest
// is written next. Each write keeps atomicWrite's fsync and rename, so what is on disk
// is always whole; a power cut can lose the last change or so, never a torn file.
//
// The opening count and cured's system alarms were saved with fsync on the control
// loop, and on the rig a slow SD card held it past the 10 s watchdog (regression from
// 42d54c3; see ADR 0408).
type latestWriter struct {
	path  string
	logf  func(string, ...any)
	write func(path string, b []byte) error // writeFileSync when made

	mu      sync.Mutex
	pending []byte
	put     uint64 // values handed over
	written uint64 // the newest of them now on disk, or given up on
	kick    chan struct{}
}

func newLatestWriter(path string, logf func(string, ...any)) *latestWriter {
	w := &latestWriter{path: path, logf: logf, write: writeFileSync, kick: make(chan struct{}, 1)}
	go w.loop()
	return w
}

// Put hands b over to be written. It copies b and returns at once.
func (w *latestWriter) Put(b []byte) {
	w.mu.Lock()
	w.pending = append([]byte(nil), b...)
	w.put++
	w.mu.Unlock()
	select {
	case w.kick <- struct{}{}:
	default: // a write is already due; it will take the newest value
	}
}

func (w *latestWriter) loop() {
	for range w.kick {
		w.mu.Lock()
		b, n := w.pending, w.put
		if n == w.written {
			w.mu.Unlock()
			continue
		}
		w.mu.Unlock()
		start := time.Now()
		err := w.write(w.path, b)
		if d := time.Since(start); d >= slowWrite {
			w.logf("<4>cured: slow write: %s took %.1f s", w.path, d.Seconds())
		}
		if err != nil {
			w.logf("<4>cured: saving %s: %v", w.path, err)
		}
		w.mu.Lock()
		w.written = n
		again := w.put != n
		w.mu.Unlock()
		if again {
			select {
			case w.kick <- struct{}{}:
			default:
			}
		}
	}
}

// Flush waits up to d for everything put so far to be written, for an orderly stop
// and for tests. It reports whether it all was.
func (w *latestWriter) Flush(d time.Duration) bool {
	w.mu.Lock()
	want := w.put
	w.mu.Unlock()
	for end := time.Now().Add(d); ; time.Sleep(5 * time.Millisecond) {
		w.mu.Lock()
		done := w.written >= want
		w.mu.Unlock()
		if done {
			return true
		}
		if time.Now().After(end) {
			return false
		}
	}
}
