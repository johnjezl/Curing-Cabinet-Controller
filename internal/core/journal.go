package core

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// A journal line is: 8 hex CRC32, a space, JSON, a newline. It is plain text so
// that a record can be read with a text editor long after this code is gone.
// A torn line fails its CRC; readers skip it and count it. Earlier lines are
// never rewritten, so a torn write cannot reach earlier data.

const (
	// segment rollover size. 4 MiB of process log is about 18 days at 60 s.
	segmentBytes = 4 << 20
	// pending is what is kept in RAM while storage is failing. At one log line
	// per minute plus events this is about a day.
	maxPending = 1500
)

func encodeLine(v any) ([]byte, error) {
	js, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(js)+10)
	out = fmt.Appendf(out, "%08x ", crc32.ChecksumIEEE(js))
	out = append(out, js...)
	return append(out, '\n'), nil
}

// decodeLines calls fn for every line with a good CRC and returns the number of
// bad lines.
func decodeLines(data []byte, fn func(js []byte)) (bad int) {
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		var line []byte
		if i < 0 {
			line, data = data, nil
		} else {
			line, data = data[:i], data[i+1:]
		}
		if len(line) == 0 {
			continue
		}
		if len(line) < 10 || line[8] != ' ' {
			bad++
			continue
		}
		want, err := strconv.ParseUint(string(line[:8]), 16, 32)
		if err != nil || uint32(want) != crc32.ChecksumIEEE(line[9:]) {
			bad++
			continue
		}
		fn(line[9:])
	}
	return bad
}

// journal is an append-only segmented file set with bounded RAM buffering while
// storage is failing.
type journal struct {
	fs      FS
	prefix  string
	f       File
	segNo   int
	size    int
	torn    bool     // the last write may have left a partial line
	pending [][]byte // lines not yet durable
	dropped uint64   // lines dropped because pending overflowed
	lastErr error
}

func segName(prefix string, n int) string { return fmt.Sprintf("%s-%06d.jnl", prefix, n) }

func segments(fs FS, prefix string) ([]string, []int, error) {
	names, err := fs.List()
	if err != nil {
		return nil, nil, err
	}
	var out []string
	var nums []int
	for _, n := range names {
		if strings.HasPrefix(n, prefix+"-") && strings.HasSuffix(n, ".jnl") {
			v, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(n, prefix+"-"), ".jnl"))
			if err == nil {
				out = append(out, n)
				nums = append(nums, v)
			}
		}
	}
	sort.Sort(byNum{out, nums})
	return out, nums, nil
}

type byNum struct {
	s []string
	n []int
}

func (b byNum) Len() int           { return len(b.s) }
func (b byNum) Less(i, j int) bool { return b.n[i] < b.n[j] }
func (b byNum) Swap(i, j int)      { b.s[i], b.s[j] = b.s[j], b.s[i]; b.n[i], b.n[j] = b.n[j], b.n[i] }

func openJournal(fs FS, prefix string) *journal {
	j := &journal{fs: fs, prefix: prefix}
	_, nums, err := segments(fs, prefix)
	if err == nil && len(nums) > 0 {
		j.segNo = nums[len(nums)-1]
		if b, err := fs.ReadFile(segName(prefix, j.segNo)); err == nil {
			j.size = len(b)
			// Anything after the last newline is a torn tail. Terminate it rather
			// than truncate it: the file is evidence and is only ever appended.
			if len(b) > 0 && b[len(b)-1] != '\n' {
				j.torn = true
			}
		}
	} else {
		j.segNo = 1
	}
	return j
}

func (j *journal) ensureOpen() error {
	if j.f != nil && j.size < segmentBytes {
		return nil
	}
	if j.f != nil {
		j.f.Close()
		j.f = nil
		j.segNo++
		j.size = 0
		j.torn = false
	}
	f, err := j.fs.OpenAppend(segName(j.prefix, j.segNo))
	if err != nil {
		return err
	}
	j.f = f
	return nil
}

// append queues a line and tries to make everything durable.
func (j *journal) append(line []byte) error {
	if len(j.pending) >= maxPending {
		copy(j.pending, j.pending[1:])
		j.pending = j.pending[:len(j.pending)-1]
		j.dropped++
	}
	j.pending = append(j.pending, line)
	return j.flush()
}

// flush writes what is pending. Lines stay pending until a sync has succeeded,
// so a failed write can repeat a line in the file but cannot lose one. Readers
// remove repeats by sequence number.
func (j *journal) flush() error {
	if len(j.pending) == 0 {
		return nil
	}
	if err := j.ensureOpen(); err != nil {
		j.lastErr = err
		return err
	}
	var buf bytes.Buffer
	if j.torn {
		buf.WriteByte('\n')
	}
	for _, l := range j.pending {
		buf.Write(l)
	}
	n, err := j.f.Write(buf.Bytes())
	j.size += n
	if err != nil {
		j.torn = true
		j.lastErr = err
		j.f.Close()
		j.f = nil
		return err
	}
	j.torn = false
	if err := j.f.Sync(); err != nil {
		j.lastErr = err
		j.f.Close()
		j.f = nil
		return err
	}
	j.pending = j.pending[:0]
	j.lastErr = nil
	return nil
}

func (j *journal) close() {
	if j.f != nil {
		j.f.Close()
		j.f = nil
	}
}

// readJournal reads every segment of a journal in order.
func readJournal(fs FS, prefix string, fn func(js []byte)) (bad int, err error) {
	return readJournalTail(fs, prefix, 0, fn)
}

// readJournalTail reads the newest maxSegs segments; 0 means all of them.
func readJournalTail(fs FS, prefix string, maxSegs int, fn func(js []byte)) (bad int, err error) {
	names, _, err := segments(fs, prefix)
	if err != nil {
		return 0, err
	}
	if maxSegs > 0 && len(names) > maxSegs {
		names = names[len(names)-maxSegs:]
	}
	for _, n := range names {
		b, err := fs.ReadFile(n)
		if err != nil {
			return bad, err
		}
		bad += decodeLines(b, fn)
	}
	return bad, nil
}

// A state slot is: magic, 8-byte sequence, 4-byte length, 4-byte CRC, JSON. Two
// slots are written in turn. A torn write damages only the slot being written,
// and the other still holds the previous good state. No rename is involved, so
// nothing depends on the file system's rename ordering.
var slotMagic = []byte("CCS1")

const slotHeader = 4 + 8 + 4 + 4

func encodeSlot(seq uint64, js []byte) []byte {
	b := make([]byte, slotHeader+len(js))
	copy(b, slotMagic)
	binary.LittleEndian.PutUint64(b[4:], seq)
	binary.LittleEndian.PutUint32(b[12:], uint32(len(js)))
	binary.LittleEndian.PutUint32(b[16:], crc32.ChecksumIEEE(js))
	copy(b[slotHeader:], js)
	return b
}

func decodeSlot(b []byte) (seq uint64, js []byte, ok bool) {
	if len(b) < slotHeader || !bytes.Equal(b[:4], slotMagic) {
		return 0, nil, false
	}
	seq = binary.LittleEndian.Uint64(b[4:])
	n := int(binary.LittleEndian.Uint32(b[12:]))
	if n < 0 || slotHeader+n > len(b) {
		return 0, nil, false
	}
	js = b[slotHeader : slotHeader+n]
	if crc32.ChecksumIEEE(js) != binary.LittleEndian.Uint32(b[16:]) {
		return 0, nil, false
	}
	return seq, js, true
}

// Store is what the control step writes through. Implementations must not
// block the caller for long; see AsyncStore for the real-hardware wrapper.
type Store interface {
	SaveState(State) error
	AppendLog(LogRecord) error
	AppendEvent(Event) error
	// Status reports the latest result and dropped line count.
	Status() StoreStatus
	Close() error
}

// StoreStatus describes storage health.
type StoreStatus struct {
	OK      bool
	Err     string
	FreeMB  float64
	Dropped uint64
}

// FileStore is the synchronous store over an FS.
type FileStore struct {
	fs      FS
	logJ    *journal
	evJ     *journal
	slotSeq uint64
	nextOdd bool
	logSeq  uint64
	evSeq   uint64
	mu      sync.Mutex
	stErr   error
	logErr  error
	evErr   error
	writes  int
	dropped uint64
	freeMB  float64
}

const (
	slotA     = "state.a"
	slotB     = "state.b"
	prefixLog = "process"
	prefixEv  = "events"
)

// Recovered is everything read back at boot.
type Recovered struct {
	State      State
	HaveState  bool
	SlotDamage int // state slots that exist but failed validation
	Events     []Event
	BadEvents  int
	LogTail    []LogRecord // newest last, bounded
	BadLog     int
	HaveLog    bool
	LastLog    LogRecord
	ReadErr    string
}

// maxTail bounds how much of the process log is kept in RAM at boot, for trend
// display. 30 days at 60 s.
const maxTail = 43200

// OpenFileStore opens the store and reads back what survived.
func OpenFileStore(fs FS) (*FileStore, Recovered) {
	s := &FileStore{fs: fs, freeMB: -1}
	var r Recovered
	var bestSeq uint64
	for i, name := range []string{slotA, slotB} {
		b, err := fs.ReadFile(name)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				r.ReadErr = err.Error()
			}
			continue
		}
		seq, js, ok := decodeSlot(b)
		if !ok {
			r.SlotDamage++
			continue
		}
		var st State
		if err := json.Unmarshal(js, &st); err != nil {
			r.SlotDamage++
			continue
		}
		if !r.HaveState || seq > bestSeq {
			bestSeq, r.State, r.HaveState = seq, st, true
			s.nextOdd = i == 0 // write the other slot next
		}
	}
	s.slotSeq = bestSeq
	var lastEv uint64
	bad, err := readJournal(fs, prefixEv, func(js []byte) {
		var e Event
		if json.Unmarshal(js, &e) == nil {
			if e.Seq > lastEv { // a repeat from a failed write has an old Seq
				lastEv = e.Seq
				r.Events = append(r.Events, e)
			}
		}
	})
	r.BadEvents = bad
	if err != nil {
		r.ReadErr = err.Error()
	}
	s.evSeq = lastEv
	var lastLog uint64
	bad, err = readJournalTail(fs, prefixLog, 4, func(js []byte) {
		var l LogRecord
		if json.Unmarshal(js, &l) == nil {
			if l.Seq > lastLog {
				lastLog = l.Seq
				r.LogTail = append(r.LogTail, l)
				if len(r.LogTail) > 2*maxTail {
					r.LogTail = append(r.LogTail[:0], r.LogTail[len(r.LogTail)-maxTail:]...)
				}
			}
		}
	})
	r.BadLog = bad
	if err != nil {
		r.ReadErr = err.Error()
	}
	if len(r.LogTail) > maxTail {
		r.LogTail = r.LogTail[len(r.LogTail)-maxTail:]
	}
	if n := len(r.LogTail); n > 0 {
		r.HaveLog, r.LastLog = true, r.LogTail[n-1]
	}
	s.logSeq = lastLog
	s.logJ = openJournal(fs, prefixLog)
	s.evJ = openJournal(fs, prefixEv)
	return s, r
}

// SaveState implements Store.
func (s *FileStore) SaveState(st State) error {
	js, err := json.Marshal(st)
	if err != nil {
		return err
	}
	s.slotSeq++
	name := slotA
	if s.nextOdd {
		name = slotB
	}
	err = s.fs.ReplaceSync(name, encodeSlot(s.slotSeq, js))
	s.nextOdd = !s.nextOdd // even on failure: never hammer one slot twice
	s.mu.Lock()
	s.stErr = err
	s.writes++
	s.mu.Unlock()
	return err
}

// AppendLog implements Store.
func (s *FileStore) AppendLog(l LogRecord) error {
	s.logSeq++
	l.Seq = s.logSeq
	line, err := encodeLine(l)
	if err != nil {
		return err
	}
	err = s.logJ.append(line)
	// A failed event write has nothing else to retry it in a quiet period.
	evErr := s.evJ.flush()
	s.mu.Lock()
	s.logErr = err
	if len(s.evJ.pending) == 0 {
		s.evErr = nil
	} else {
		s.evErr = evErr
	}
	s.writes++
	s.dropped = s.logJ.dropped + s.evJ.dropped
	s.mu.Unlock()
	return err
}

// AppendEvent implements Store.
func (s *FileStore) AppendEvent(e Event) error {
	s.evSeq++
	e.Seq = s.evSeq
	line, err := encodeLine(e)
	if err != nil {
		return err
	}
	err = s.evJ.append(line)
	s.mu.Lock()
	s.evErr = err
	if err == nil && s.logErr != nil && len(s.logJ.pending) == 0 {
		s.logErr = nil
	}
	s.writes++
	s.dropped = s.logJ.dropped + s.evJ.dropped
	s.mu.Unlock()
	return err
}

// Status implements Store.
func (s *FileStore) Status() StoreStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := StoreStatus{OK: s.stErr == nil && s.logErr == nil && s.evErr == nil}
	for _, e := range []error{s.stErr, s.logErr, s.evErr} {
		if e != nil {
			st.Err = e.Error()
			break
		}
	}
	if free, err := s.fs.FreeMB(); err == nil {
		st.FreeMB = free
	}
	st.Dropped = s.dropped
	return st
}

// Close implements Store.
func (s *FileStore) Close() error {
	s.logJ.close()
	s.evJ.close()
	return nil
}

// AsyncStore does the writing on its own goroutine, because an fsync on an SD
// card can stall for seconds and the control loop must not. The queue is
// bounded and drops oldest records when storage cannot keep up; drops are
// counted and raise the storage alarm.
type AsyncStore struct {
	inner   Store
	ch      chan func()
	done    chan struct{}
	once    sync.Once
	dropped atomic.Uint64
	stalled atomic.Bool
	depth   int
}

// NewAsyncStore wraps inner.
func NewAsyncStore(inner Store, depth int) *AsyncStore {
	a := &AsyncStore{inner: inner, ch: make(chan func(), depth), done: make(chan struct{}), depth: depth}
	go func() {
		defer close(a.done)
		for f := range a.ch {
			f()
		}
	}()
	return a
}

func (a *AsyncStore) enqueue(f func()) {
	select {
	case a.ch <- f:
		a.stalled.Store(false)
	default:
		a.dropped.Add(1)
		a.stalled.Store(true)
	}
}

// SaveState implements Store.
func (a *AsyncStore) SaveState(st State) error {
	st = st.clone()
	a.enqueue(func() { _ = a.inner.SaveState(st) })
	return nil
}

// AppendLog implements Store.
func (a *AsyncStore) AppendLog(l LogRecord) error {
	a.enqueue(func() { _ = a.inner.AppendLog(l) })
	return nil
}

// AppendEvent implements Store.
func (a *AsyncStore) AppendEvent(e Event) error {
	a.enqueue(func() { _ = a.inner.AppendEvent(e) })
	return nil
}

// Status implements Store.
func (a *AsyncStore) Status() StoreStatus {
	st := a.inner.Status()
	st.Dropped += a.dropped.Load()
	if a.stalled.Load() {
		st.OK = false
		if st.Err == "" {
			st.Err = "storage queue full: writes are stalled"
		}
	}
	return st
}

// Close implements Store. It waits for the queue to drain.
func (a *AsyncStore) Close() error {
	a.once.Do(func() { close(a.ch) })
	<-a.done
	return a.inner.Close()
}

// CountGoodLines counts valid journal lines in a byte slice.
func CountGoodLines(b []byte) (good, bad int) {
	bad = decodeLines(b, func([]byte) { good++ })
	return good, bad
}

// DecodeSlotOK says whether b is a valid state slot.
func DecodeSlotOK(b []byte) bool {
	_, _, ok := decodeSlot(b)
	return ok
}
