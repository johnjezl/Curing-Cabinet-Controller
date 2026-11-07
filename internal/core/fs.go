package core

import (
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
)

// File is an append handle. Sync returning nil means the data survives power
// loss. That promise is the whole basis of the persistence design.
type File interface {
	Write(p []byte) (int, error)
	Sync() error
	Close() error
}

// FS is the storage the core is allowed to use. OSFS is the real one; MemFS
// models power loss so that torn writes can be tested at every byte.
type FS interface {
	// OpenAppend opens name for appending, creating it if needed.
	OpenAppend(name string) (File, error)
	// ReadFile returns the whole file; a missing file wraps os.ErrNotExist.
	ReadFile(name string) ([]byte, error)
	// ReplaceSync overwrites name in place and syncs it. It is NOT atomic: a
	// power cut can leave anything there. Callers rely on two slots, not on this.
	ReplaceSync(name string, data []byte) error
	// List returns file names, sorted.
	List() ([]string, error)
	Remove(name string) error
	// FreeMB reports free space. An error is not fatal.
	FreeMB() (float64, error)
}

// OSFS stores files under Dir.
type OSFS struct{ Dir string }

// NewOSFS creates dir if needed.
func NewOSFS(dir string) (*OSFS, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &OSFS{Dir: dir}, nil
}

func (o *OSFS) path(name string) string { return filepath.Join(o.Dir, name) }

// syncDir makes a newly created directory entry durable.
func (o *OSFS) syncDir() error {
	d, err := os.Open(o.Dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// OpenAppend implements FS.
func (o *OSFS) OpenAppend(name string) (File, error) {
	_, statErr := os.Stat(o.path(name))
	f, err := os.OpenFile(o.path(name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	if errors.Is(statErr, os.ErrNotExist) {
		if err := o.syncDir(); err != nil {
			f.Close()
			return nil, err
		}
	}
	return f, nil
}

// ReadFile implements FS.
func (o *OSFS) ReadFile(name string) ([]byte, error) { return os.ReadFile(o.path(name)) }

// ReplaceSync implements FS.
func (o *OSFS) ReplaceSync(name string, data []byte) error {
	_, statErr := os.Stat(o.path(name))
	f, err := os.OpenFile(o.path(name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if errors.Is(statErr, os.ErrNotExist) {
		return o.syncDir()
	}
	return nil
}

// List implements FS.
func (o *OSFS) List() ([]string, error) {
	es, err := os.ReadDir(o.Dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range es {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// Remove implements FS.
func (o *OSFS) Remove(name string) error { return os.Remove(o.path(name)) }

// FreeMB implements FS.
func (o *OSFS) FreeMB() (float64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(o.Dir, &st); err != nil {
		return 0, err
	}
	return float64(st.Bavail) * float64(st.Bsize) / (1024 * 1024), nil
}

// ErrPowerCut is returned by MemFS after an armed cut has fired.
var ErrPowerCut = errors.New("simulated power cut")

// ErrInjected is the storage failure MemFS returns when told to fail.
var ErrInjected = errors.New("injected storage failure")

// MemFS is an in-memory FS with a power-loss model:
//
//	synced bytes always survive;
//	unsynced appended bytes survive as a random prefix, and the last part of
//	that prefix may be garbage (a torn sector);
//	an unsynced in-place replace leaves the old content, nothing, or a torn mix.
//
// A cut can be armed to fire after a given number of mutating operations, so a
// test can cut at every point in a run.
type MemFS struct {
	mu       sync.Mutex
	files    map[string]*memFile
	ops      int
	cutAfter int // fires when ops reaches this; 0 means not armed
	cut      bool

	// FailWrite and FailSync make those operations return ErrInjected.
	FailWrite bool
	FailSync  bool
	// CapacityBytes, if positive, makes writes past this total return ENOSPC.
	CapacityBytes int
}

type memFile struct {
	durable  []byte // what survives a cut for certain
	volatile []byte // current content as seen by reads
	replaced bool   // volatile came from a replace that is not yet synced
}

// NewMemFS returns an empty MemFS.
func NewMemFS() *MemFS { return &MemFS{files: map[string]*memFile{}} }

// Ops returns the number of mutating operations so far.
func (m *MemFS) Ops() int { m.mu.Lock(); defer m.mu.Unlock(); return m.ops }

// ArmCut makes the FS die after n more mutating operations (n>=1).
func (m *MemFS) ArmCut(n int) { m.mu.Lock(); defer m.mu.Unlock(); m.cutAfter = m.ops + n }

// Cut reports whether an armed cut has fired.
func (m *MemFS) Cut() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.cut }

// PowerCut applies the loss model and brings the FS back as it would be found
// at the next boot. Use rng so that a seed reproduces the exact damage.
func (m *MemFS) PowerCut(rng *rand.Rand) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.files {
		if f.replaced {
			switch rng.IntN(3) {
			case 0: // old content remains
				f.volatile = append([]byte(nil), f.durable...)
			case 1: // truncated
				f.volatile = nil
			default: // torn: a prefix of the new data, the tail is garbage
				n := 0
				if len(f.volatile) > 0 {
					n = rng.IntN(len(f.volatile))
				}
				f.volatile = append([]byte(nil), f.volatile[:n]...)
				garble(f.volatile, rng)
			}
		} else if len(f.volatile) > len(f.durable) {
			extra := len(f.volatile) - len(f.durable)
			keep := rng.IntN(extra + 1)
			nv := append([]byte(nil), f.volatile[:len(f.durable)+keep]...)
			garbleFrom(nv, len(f.durable), rng)
			f.volatile = nv
		}
		f.durable = append([]byte(nil), f.volatile...)
		f.replaced = false
	}
	m.cut = false
	m.cutAfter = 0
}

func garble(b []byte, rng *rand.Rand) { garbleFrom(b, 0, rng) }

func garbleFrom(b []byte, from int, rng *rand.Rand) {
	if len(b) <= from || rng.IntN(2) == 0 {
		return
	}
	start := from + rng.IntN(len(b)-from)
	for i := start; i < len(b); i++ {
		b[i] = byte(rng.IntN(256))
	}
}

// tick counts one mutating operation; caller holds the lock.
func (m *MemFS) tick() error {
	if m.cut {
		return ErrPowerCut
	}
	m.ops++
	if m.cutAfter > 0 && m.ops >= m.cutAfter {
		m.cut = true
		return ErrPowerCut
	}
	return nil
}

func (m *MemFS) total() int {
	t := 0
	for _, f := range m.files {
		t += len(f.volatile)
	}
	return t
}

type memHandle struct {
	m    *MemFS
	name string
}

// OpenAppend implements FS.
func (m *MemFS) OpenAppend(name string) (File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cut {
		return nil, ErrPowerCut
	}
	if m.FailWrite {
		return nil, ErrInjected
	}
	if _, ok := m.files[name]; !ok {
		m.files[name] = &memFile{}
	}
	return &memHandle{m: m, name: name}, nil
}

func (h *memHandle) Write(p []byte) (int, error) {
	m := h.m
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[h.name]
	if !ok {
		return 0, os.ErrNotExist
	}
	if err := m.tick(); err != nil {
		// A cut in the middle of a write leaves part of it behind.
		f.volatile = append(f.volatile, p[:len(p)/2]...)
		return len(p) / 2, err
	}
	if m.FailWrite {
		return 0, ErrInjected
	}
	if m.CapacityBytes > 0 && m.total()+len(p) > m.CapacityBytes {
		n := m.CapacityBytes - m.total()
		if n < 0 {
			n = 0
		}
		f.volatile = append(f.volatile, p[:n]...)
		return n, syscall.ENOSPC
	}
	f.volatile = append(f.volatile, p...)
	return len(p), nil
}

func (h *memHandle) Sync() error {
	m := h.m
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[h.name]
	if !ok {
		return os.ErrNotExist
	}
	if err := m.tick(); err != nil {
		return err
	}
	if m.FailSync {
		return ErrInjected
	}
	f.durable = append(f.durable[:0], f.volatile...)
	f.replaced = false
	return nil
}

func (h *memHandle) Close() error { return nil }

// ReadFile implements FS.
func (m *MemFS) ReadFile(name string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[name]
	if !ok {
		return nil, fmt.Errorf("%s: %w", name, os.ErrNotExist)
	}
	return append([]byte(nil), f.volatile...), nil
}

// ReplaceSync implements FS. It is two steps, so a cut can land between them.
func (m *MemFS) ReplaceSync(name string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.tick(); err != nil {
		return err
	}
	if m.FailWrite {
		return ErrInjected
	}
	f, ok := m.files[name]
	if !ok {
		f = &memFile{}
		m.files[name] = f
	}
	if m.CapacityBytes > 0 && m.total()-len(f.volatile)+len(data) > m.CapacityBytes {
		return syscall.ENOSPC
	}
	f.volatile = append([]byte(nil), data...)
	f.replaced = true
	if err := m.tick(); err != nil {
		return err
	}
	if m.FailSync {
		return ErrInjected
	}
	f.durable = append(f.durable[:0], f.volatile...)
	f.replaced = false
	return nil
}

// List implements FS.
func (m *MemFS) List() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.files))
	for n := range m.files {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// Remove implements FS.
func (m *MemFS) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.tick(); err != nil {
		return err
	}
	if m.FailWrite {
		return ErrInjected
	}
	delete(m.files, name)
	return nil
}

// FreeMB implements FS.
func (m *MemFS) FreeMB() (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CapacityBytes > 0 {
		return float64(m.CapacityBytes-m.total()) / (1024 * 1024), nil
	}
	return 1024, nil
}

// WriteRaw puts bytes into a file, durable. Tests use it to plant corruption.
func (m *MemFS) WriteRaw(name string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[name] = &memFile{durable: append([]byte(nil), data...), volatile: append([]byte(nil), data...)}
}

var _ io.Writer = (File)(nil)

// DurableFile returns what would certainly survive a power cut right now.
func (m *MemFS) DurableFile(name string) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[name]
	if !ok {
		return nil
	}
	return append([]byte(nil), f.durable...)
}

// FileNames lists files. Unlike List it cannot fail.
func (m *MemFS) FileNames() []string {
	names, _ := m.List()
	return names
}
