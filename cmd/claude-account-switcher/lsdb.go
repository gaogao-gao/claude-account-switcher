package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/journal"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// lsDB reads Chromium's Local Storage LevelDB and adds writes to it the way
// Chromium's own LevelDB does, by appending to the live log, which it replays
// on its next start.
//
// No LevelDB library may write this database: goleveldb, on opening it, drops
// log records whose sequence number lies below the one in the MANIFEST, which
// Chromium writes routinely, and a rewrite would lose them. Reading the
// tables through goleveldb is safe; the logs are replayed here, all of them.
type lsDB struct {
	dir     string
	vals    map[string][]byte
	log     string // the live log, where writes go
	logEnd  int    // where its last whole record ends
	nextSeq uint64
}

func openLSDB(dir string) (*lsDB, error) {
	m, err := readManifest(dir)
	if err != nil {
		return nil, err
	}
	db, err := leveldb.OpenFile(dir, &opt.Options{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	d := &lsDB{dir: dir, vals: map[string][]byte{}, nextSeq: m.lastSeq + 1}
	it := db.NewIterator(nil, nil)
	for it.Next() {
		d.vals[string(it.Key())] = append([]byte{}, it.Value()...)
	}
	it.Release()
	err = it.Error()
	db.Close()
	if err != nil {
		return nil, err
	}

	// Logs from the MANIFEST's on hold what the tables do not have yet.
	logs, _ := filepath.Glob(filepath.Join(dir, "*.log"))
	var live []uint64
	for _, p := range logs {
		n, err := strconv.ParseUint(strings.TrimSuffix(filepath.Base(p), ".log"), 10, 64)
		if err == nil && (n >= m.logNum || n == m.prevLogNum && n != 0) {
			live = append(live, n)
		}
	}
	sort.Slice(live, func(i, j int) bool { return live[i] < live[j] })
	for _, n := range live {
		p := filepath.Join(dir, fmt.Sprintf("%06d.log", n))
		if err := d.replay(p); err != nil {
			return nil, fmt.Errorf("replay %s: %w", filepath.Base(p), err)
		}
		d.log = p
	}
	if d.log == "" {
		return nil, errors.New("no live log")
	}
	return d, nil
}

func (d *lsDB) get(key []byte) ([]byte, bool) {
	v, ok := d.vals[string(key)]
	return v, ok
}

// replay applies the write batches of a log, skipping damaged ones as
// LevelDB does, and notes where its last whole record ends.
func (d *lsDB) replay(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	records, end := readLogRecords(data)
	d.logEnd = end
	for _, b := range records {
		if len(b) < 12 {
			continue
		}
		seq := binary.LittleEndian.Uint64(b)
		count := binary.LittleEndian.Uint32(b[8:])
		if e := seq + uint64(count); e > d.nextSeq {
			d.nextSeq = e
		}
		// Check the whole batch before applying any of it.
		type op struct {
			put      bool
			key, val []byte
		}
		var ops []op
		rest := b[12:]
		for i := uint32(0); i < count && rest != nil; i++ {
			if len(rest) == 0 {
				rest = nil
				break
			}
			kind := rest[0]
			key, r, ok := lengthPrefixed(rest[1:])
			if !ok || kind > 1 {
				rest = nil
				break
			}
			o := op{put: kind == 1, key: key}
			if o.put {
				if o.val, r, ok = lengthPrefixed(r); !ok {
					rest = nil
					break
				}
			}
			ops = append(ops, o)
			rest = r
		}
		if rest == nil || uint32(len(ops)) != count {
			continue
		}
		for _, o := range ops {
			if o.put {
				d.vals[string(o.key)] = append([]byte{}, o.val...)
			} else {
				delete(d.vals, string(o.key))
			}
		}
	}
	return nil
}

// readLogRecords splits a LevelDB log into its records, dropping chunks that
// fail their checksum and records left unfinished. end is the offset right
// after the last whole record, or after the block padding that follows it.
func readLogRecords(data []byte) (records [][]byte, end int) {
	var cur []byte
	inRecord := false
	off := 0
	for off < len(data) {
		left := logBlock - off%logBlock
		if left < logHeader {
			if allZero(data[off:min(off+left, len(data))]) && !inRecord && end == off {
				end = min(off+left, len(data))
			}
			off += left
			continue
		}
		if off+logHeader > len(data) {
			break
		}
		h := data[off : off+logHeader]
		n := int(binary.LittleEndian.Uint16(h[4:]))
		kind := h[6]
		if off+logHeader+n > len(data) {
			break // torn tail
		}
		payload := data[off+logHeader : off+logHeader+n]
		crc := crc32.Update(crc32.Checksum([]byte{kind}, crc32c), crc32c, payload)
		next := off + logHeader + n
		if kind == 0 && n == 0 && binary.LittleEndian.Uint32(h) == 0 {
			// Zeroed space LevelDB leaves; skip to the next block.
			off += left
			continue
		}
		if binary.LittleEndian.Uint32(h) != (crc>>15|crc<<17)+0xa282ead8 {
			cur, inRecord = nil, false
			off += left // a bad chunk spoils the rest of its block
			continue
		}
		switch kind {
		case 1:
			records = append(records, append([]byte{}, payload...))
			cur, inRecord = nil, false
			end = next
		case 2:
			cur, inRecord = append([]byte{}, payload...), true
		case 3:
			if inRecord {
				cur = append(cur, payload...)
			}
		case 4:
			if inRecord {
				records = append(records, append(cur, payload...))
				end = next
			}
			cur, inRecord = nil, false
		}
		off = next
	}
	return records, end
}

func allZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

func lengthPrefixed(b []byte) (field, rest []byte, ok bool) {
	n, k := binary.Uvarint(b)
	if k <= 0 || uint64(len(b)-k) < n {
		return nil, nil, false
	}
	return b[k : k+int(n)], b[k+int(n):], true
}

// put appends one write batch with kv to the live log.
func (d *lsDB) put(kv map[string][]byte) error {
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var batch bytes.Buffer
	var hdr [12]byte
	binary.LittleEndian.PutUint64(hdr[:], d.nextSeq)
	binary.LittleEndian.PutUint32(hdr[8:], uint32(len(keys)))
	batch.Write(hdr[:])
	for _, k := range keys {
		batch.WriteByte(1)
		batch.Write(binary.AppendUvarint(nil, uint64(len(k))))
		batch.WriteString(k)
		batch.Write(binary.AppendUvarint(nil, uint64(len(kv[k]))))
		batch.Write(kv[k])
	}

	f, err := os.OpenFile(d.log, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	size := info.Size()
	if size != int64(d.logEnd) {
		// Half a record at the end, as when the app was killed while
		// writing. LevelDB drops it on its next start; a record after it
		// would be dropped with it. Leave this switch's changes for the next.
		f.Close()
		return fmt.Errorf("%s ends in a partial record", filepath.Base(d.log))
	}
	data := appendLogRecord(nil, size, batch.Bytes())
	_, err = f.WriteAt(data, size)
	if err == nil {
		err = f.Sync()
	}
	if err != nil {
		// Leave no partial record behind.
		_ = f.Truncate(size)
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	d.logEnd += len(data)
	d.nextSeq += uint64(len(keys))
	for _, k := range keys {
		d.vals[k] = kv[k]
	}
	return nil
}

// LevelDB's log: 32 KiB blocks of chunks, each a masked CRC-32C, a length
// and a type ahead of its payload; a block tail too short for a header is
// zero filled.
const (
	logBlock  = 32 << 10
	logHeader = 7
)

var crc32c = crc32.MakeTable(crc32.Castagnoli)

// appendLogRecord adds payload as one record to a log that is size bytes long.
func appendLogRecord(out []byte, size int64, payload []byte) []byte {
	off := int(size % logBlock)
	first := true
	for {
		if left := logBlock - off; left < logHeader {
			out = append(out, make([]byte, left)...)
			off = 0
		}
		n := min(len(payload), logBlock-off-logHeader)
		last := n == len(payload)
		var kind byte
		switch {
		case first && last:
			kind = 1
		case first:
			kind = 2
		case last:
			kind = 4
		default:
			kind = 3
		}
		crc := crc32.Update(crc32.Checksum([]byte{kind}, crc32c), crc32c, payload[:n])
		var h [logHeader]byte
		binary.LittleEndian.PutUint32(h[:], (crc>>15|crc<<17)+0xa282ead8)
		binary.LittleEndian.PutUint16(h[4:], uint16(n))
		h[6] = kind
		out = append(out, h[:]...)
		out = append(out, payload[:n]...)
		payload = payload[n:]
		off = (off + logHeader + n) % logBlock
		first = false
		if last {
			return out
		}
	}
}

type manifest struct {
	logNum, prevLogNum, lastSeq uint64
}

// readManifest reads where the live logs start and the last sequence number
// from the MANIFEST that CURRENT names.
func readManifest(dir string) (manifest, error) {
	var m manifest
	cur, err := os.ReadFile(filepath.Join(dir, "CURRENT"))
	if err != nil {
		return m, err
	}
	f, err := os.Open(filepath.Join(dir, strings.TrimSpace(string(cur))))
	if err != nil {
		return m, err
	}
	defer f.Close()
	r := journal.NewReader(f, nil, false, true)
	for {
		rec, err := r.Next()
		if err != nil {
			break
		}
		edit, err := io.ReadAll(rec)
		if err != nil {
			break
		}
		readVersionEdit(edit, &m)
	}
	if m.logNum == 0 {
		return m, errors.New("MANIFEST names no log")
	}
	return m, nil
}

func readVersionEdit(b []byte, m *manifest) {
	uv := func() (uint64, bool) {
		v, k := binary.Uvarint(b)
		if k <= 0 {
			return 0, false
		}
		b = b[k:]
		return v, true
	}
	skip := func() bool { // a length-prefixed field
		_, rest, ok := lengthPrefixed(b)
		b = rest
		return ok
	}
	for len(b) > 0 {
		tag, ok := uv()
		if !ok {
			return
		}
		switch tag {
		case 1: // comparator
			ok = skip()
		case 2:
			m.logNum, ok = uv()
		case 9:
			m.prevLogNum, ok = uv()
		case 3: // next file number
			_, ok = uv()
		case 4:
			var seq uint64
			if seq, ok = uv(); ok && seq > m.lastSeq {
				m.lastSeq = seq
			}
		case 5: // compact pointer: level, key
			if _, ok = uv(); ok {
				ok = skip()
			}
		case 6: // deleted file: level, number
			if _, ok = uv(); ok {
				_, ok = uv()
			}
		case 7: // new file: level, number, size, smallest, largest
			for i := 0; i < 3 && ok; i++ {
				_, ok = uv()
			}
			ok = ok && skip() && skip()
		default:
			return
		}
		if !ok {
			return
		}
	}
}
