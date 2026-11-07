package drivers

import (
	"errors"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// fakeChip hands out fakeLines and records requests.
type fakeChip struct {
	mu       sync.Mutex
	lines    map[int]*fakeLine
	failPins map[int]error
	closed   int
	onLine   func(*fakeLine)
}

func newFakeChip() *fakeChip { return &fakeChip{lines: map[int]*fakeLine{}, failPins: map[int]error{}} }

func (c *fakeChip) opener() chipOpener { return func() (gpioChip, error) { return c, nil } }

func (c *fakeChip) Request(consumer string, offset int, flags uint64, eventBuffer int) (gpioLine, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.failPins[offset]; err != nil {
		return nil, err
	}
	l := &fakeLine{pin: offset, consumer: consumer, flags: flags, buffer: eventBuffer}
	if flags&flagOutput == 0 {
		l.level = flags&flagPullUp != 0
	}
	if c.onLine != nil {
		c.onLine(l)
	}
	c.lines[offset] = l
	return l, nil
}

func (c *fakeChip) Close() error { c.closed++; return nil }

// fakeLine is a scripted GPIO line.
type fakeLine struct {
	mu       sync.Mutex
	pin      int
	consumer string
	flags    uint64
	buffer   int
	level    bool
	sets     []bool
	setErr   error
	getErr   error
	closed   bool
	events   []lineEvent
	eventErr error
	// hooks stand in for the device on the other end of the wire.
	onSet         func(high bool)
	onGet         func() bool
	onReconfigure func(flags uint64)
	flagHistory   []uint64
}

func (l *fakeLine) Set(high bool) error {
	l.mu.Lock()
	if l.setErr != nil {
		defer l.mu.Unlock()
		return l.setErr
	}
	l.level = high
	l.sets = append(l.sets, high)
	hook := l.onSet
	l.mu.Unlock()
	if hook != nil {
		hook(high)
	}
	return nil
}

func (l *fakeLine) Get() (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.getErr != nil {
		return false, l.getErr
	}
	if l.onGet != nil {
		return l.onGet(), nil
	}
	return l.level, nil
}

func (l *fakeLine) Reconfigure(flags uint64) error {
	l.mu.Lock()
	l.flags = flags
	l.flagHistory = append(l.flagHistory, flags)
	if flags&flagOutput != 0 {
		l.level = false
	} else {
		l.level = flags&flagPullUp != 0
	}
	hook := l.onReconfigure
	l.mu.Unlock()
	if hook != nil {
		hook(flags)
	}
	return nil
}

func (l *fakeLine) ReadEvent(timeout time.Duration) (lineEvent, error) {
	l.mu.Lock()
	if len(l.events) > 0 {
		e := l.events[0]
		l.events = l.events[1:]
		l.mu.Unlock()
		return e, nil
	}
	err := l.eventErr
	l.mu.Unlock()
	if err != nil {
		return lineEvent{}, err
	}
	time.Sleep(min(timeout, 2*time.Millisecond))
	return lineEvent{}, os.ErrDeadlineExceeded
}

func (l *fakeLine) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	return nil
}

func (l *fakeLine) push(e ...lineEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e...)
}

// fakeFS is an in-memory sysfs with error injection per path.
type fakeFS struct {
	mu       sync.Mutex
	files    map[string]string
	links    map[string]string
	readErr  map[string]error
	writeErr map[string]error
	writes   []string
	block    chan struct{} // when set, ReadFile waits on it
}

func newFakeFS() *fakeFS {
	return &fakeFS{files: map[string]string{}, links: map[string]string{}, readErr: map[string]error{}, writeErr: map[string]error{}}
}

func (f *fakeFS) ReadFile(p string) ([]byte, error) {
	f.mu.Lock()
	block := f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.readErr[p]; err != nil {
		return nil, err
	}
	v, ok := f.files[p]
	if !ok {
		return nil, &os.PathError{Op: "open", Path: p, Err: os.ErrNotExist}
	}
	return []byte(v), nil
}

func (f *fakeFS) WriteFile(p string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.writeErr[p]; err != nil {
		return err
	}
	if _, ok := f.files[p]; !ok {
		return &os.PathError{Op: "open", Path: p, Err: os.ErrNotExist}
	}
	f.files[p] = string(data)
	f.writes = append(f.writes, path.Base(p)+"="+string(data))
	return nil
}

func (f *fakeFS) Glob(pattern string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := map[string]bool{}
	depth := strings.Count(pattern, "/")
	for p := range f.files {
		parts := strings.Split(p, "/")
		for i := 1; i <= len(parts); i++ {
			prefix := strings.Join(parts[:i], "/")
			if strings.Count(prefix, "/") != depth {
				continue
			}
			if ok, _ := path.Match(pattern, prefix); ok {
				seen[prefix] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func (f *fakeFS) EvalSymlinks(p string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, ok := f.links[p]; ok {
		return v, nil
	}
	return p, nil
}

// fakeBus is a scripted I2C adapter.
type fakeBus struct {
	mu     sync.Mutex
	frame  []byte
	err    error
	addrs  []uint16
	sizes  []int
	block  chan struct{}
	closed bool
}

func (b *fakeBus) ReadFrom(addr uint16, p []byte) error {
	if b.block != nil {
		<-b.block
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.addrs = append(b.addrs, addr)
	b.sizes = append(b.sizes, len(p))
	if b.err != nil {
		return b.err
	}
	if len(b.frame) != len(p) {
		return errors.New("fake bus: short frame")
	}
	copy(p, b.frame)
	return nil
}

func (b *fakeBus) Close() error { b.closed = true; return nil }
