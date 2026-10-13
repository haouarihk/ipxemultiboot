// Package iso9660 is a minimal read-only ISO9660 reader with Rock Ridge and
// Joliet name support. Files are exposed as io.ReaderAt views into the image so
// they can be served directly (with HTTP range support) without extraction.
package iso9660

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"
	"unicode/utf16"
)

const sectorSize = 2048

var ErrNotISO = errors.New("iso9660: not an ISO9660 image")

type extent struct {
	lba  uint32
	size uint32
}

// Entry is a file or directory inside the image.
type Entry struct {
	Name    string
	IsDir   bool
	Size    int64
	ModTime time.Time
	extents []extent
	more    bool // multi-extent: further records follow
}

// Image is an opened ISO9660 image.
type Image struct {
	r        io.ReaderAt
	closer   io.Closer
	volumeID string
	root     Entry
	joliet   bool
	rr       bool
	suspSkip int
}

// Open opens an ISO file from disk.
func Open(name string) (*Image, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	img, err := New(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	img.closer = f
	return img, nil
}

// New reads an image from r.
func New(r io.ReaderAt) (*Image, error) {
	img := &Image{r: r}
	var pvdRoot, jolietRoot []byte
	buf := make([]byte, sectorSize)
	for sector := int64(16); sector < 64; sector++ {
		if _, err := r.ReadAt(buf, sector*sectorSize); err != nil {
			return nil, ErrNotISO
		}
		if string(buf[1:6]) != "CD001" {
			return nil, ErrNotISO
		}
		switch buf[0] {
		case 1:
			pvdRoot = append([]byte(nil), buf[156:156+34]...)
			img.volumeID = strings.TrimSpace(string(buf[40:72]))
		case 2:
			esc := buf[88:91]
			if esc[0] == '%' && esc[1] == '/' && (esc[2] == '@' || esc[2] == 'C' || esc[2] == 'E') {
				jolietRoot = append([]byte(nil), buf[156:156+34]...)
			}
		}
		if buf[0] == 255 {
			break
		}
	}
	if pvdRoot == nil {
		return nil, ErrNotISO
	}

	img.root = parseRecord(pvdRoot)
	img.root.IsDir = true
	if img.detectRockRidge() {
		img.rr = true
	} else if jolietRoot != nil {
		img.joliet = true
		img.root = parseRecord(jolietRoot)
		img.root.IsDir = true
	}
	return img, nil
}

func (img *Image) Close() error {
	if img.closer != nil {
		return img.closer.Close()
	}
	return nil
}

func (img *Image) VolumeID() string { return img.volumeID }

// detectRockRidge checks the root "." record for a SUSP "SP" entry.
func (img *Image) detectRockRidge() bool {
	data := make([]byte, sectorSize)
	if _, err := img.r.ReadAt(data, int64(img.root.extents[0].lba)*sectorSize); err != nil {
		return false
	}
	l := int(data[0])
	if l < 34 {
		return false
	}
	nameLen := int(data[32])
	su := 33 + nameLen
	if su%2 == 1 {
		su++
	}
	sua := data[su:l]
	if len(sua) >= 7 && sua[0] == 'S' && sua[1] == 'P' && sua[4] == 0xBE && sua[5] == 0xEF {
		img.suspSkip = int(sua[6])
		return true
	}
	return false
}

func parseRecord(rec []byte) Entry {
	e := Entry{
		IsDir: rec[25]&0x02 != 0,
		Size:  int64(binary.LittleEndian.Uint32(rec[10:14])),
	}
	e.extents = []extent{{lba: binary.LittleEndian.Uint32(rec[2:6]), size: binary.LittleEndian.Uint32(rec[10:14])}}
	d := rec[18:25]
	if d[0] != 0 {
		e.ModTime = time.Date(1900+int(d[0]), time.Month(d[1]), int(d[2]), int(d[3]), int(d[4]), int(d[5]), 0,
			time.FixedZone("", int(int8(d[6]))*15*60))
	}
	return e
}

// readDir parses all records of a directory entry.
func (img *Image) readDir(dir Entry) ([]Entry, error) {
	if !dir.IsDir {
		return nil, fs.ErrInvalid
	}
	data := make([]byte, dir.extents[0].size)
	if _, err := img.r.ReadAt(data, int64(dir.extents[0].lba)*sectorSize); err != nil {
		return nil, err
	}
	var out []Entry
	for off := 0; off < len(data); {
		l := int(data[off])
		if l == 0 {
			// Records never span sectors; skip to the next one.
			off = (off/sectorSize + 1) * sectorSize
			continue
		}
		if off+l > len(data) || l < 34 {
			break
		}
		rec := data[off : off+l]
		off += l

		nameLen := int(rec[32])
		rawName := rec[33 : 33+nameLen]
		if nameLen == 1 && (rawName[0] == 0 || rawName[0] == 1) {
			continue // "." and ".."
		}
		e := parseRecord(rec)
		e.Name = img.decodeName(rawName)
		if img.rr {
			su := 33 + nameLen
			if su%2 == 1 {
				su++
			}
			if su+img.suspSkip <= len(rec) {
				if name, ok := img.rockRidgeName(rec[su+img.suspSkip:]); ok {
					e.Name = name
				}
			}
		}

		// Multi-extent files: consecutive records with the same name.
		more := rec[25]&0x80 != 0
		if n := len(out); n > 0 && out[n-1].more && out[n-1].Name == e.Name {
			out[n-1].extents = append(out[n-1].extents, e.extents...)
			out[n-1].Size += e.Size
			out[n-1].more = more
			continue
		}
		e.more = more
		out = append(out, e)
	}
	return out, nil
}

func (img *Image) decodeName(raw []byte) string {
	var name string
	if img.joliet {
		u := make([]uint16, len(raw)/2)
		for i := range u {
			u[i] = binary.BigEndian.Uint16(raw[2*i:])
		}
		name = string(utf16.Decode(u))
	} else {
		name = string(raw)
	}
	if i := strings.LastIndexByte(name, ';'); i >= 0 {
		name = name[:i]
	}
	return strings.TrimSuffix(name, ".")
}

// rockRidgeName extracts the NM (alternate name) entry, following CE continuations.
func (img *Image) rockRidgeName(sua []byte) (string, bool) {
	var name []byte
	found := false
	for depth := 0; depth < 8 && sua != nil; depth++ {
		var next []byte
		for len(sua) >= 4 {
			sig := string(sua[0:2])
			l := int(sua[2])
			if l < 4 || l > len(sua) {
				break
			}
			body := sua[4:l]
			switch sig {
			case "NM":
				if len(body) >= 1 {
					flags := body[0]
					if flags&0x06 == 0 { // not "." / ".."
						name = append(name, body[1:]...)
						found = true
					}
				}
			case "CE":
				if len(body) >= 24 {
					lba := binary.LittleEndian.Uint32(body[0:4])
					off := binary.LittleEndian.Uint32(body[8:12])
					n := binary.LittleEndian.Uint32(body[16:20])
					buf := make([]byte, n)
					if _, err := img.r.ReadAt(buf, int64(lba)*sectorSize+int64(off)); err == nil {
						next = buf
					}
				}
			case "ST":
				sua = nil
			}
			if sua == nil {
				break
			}
			sua = sua[l:]
		}
		sua = next
	}
	return string(name), found && len(name) > 0
}

// Stat looks up a path ("/" separated, leading slash optional). Matching is
// exact first, then case-insensitive.
func (img *Image) Stat(p string) (Entry, error) {
	p = strings.Trim(path.Clean("/"+p), "/")
	cur := img.root
	if p == "" {
		return cur, nil
	}
	for _, part := range strings.Split(p, "/") {
		entries, err := img.readDir(cur)
		if err != nil {
			return Entry{}, err
		}
		var match *Entry
		for i := range entries {
			if entries[i].Name == part {
				match = &entries[i]
				break
			}
		}
		if match == nil {
			for i := range entries {
				if strings.EqualFold(entries[i].Name, part) {
					match = &entries[i]
					break
				}
			}
		}
		if match == nil {
			return Entry{}, fmt.Errorf("%s: %w", p, fs.ErrNotExist)
		}
		cur = *match
	}
	return cur, nil
}

// ReadDir lists a directory.
func (img *Image) ReadDir(p string) ([]Entry, error) {
	e, err := img.Stat(p)
	if err != nil {
		return nil, err
	}
	return img.readDir(e)
}

// Exists reports whether a path exists.
func (img *Image) Exists(p string) bool {
	_, err := img.Stat(p)
	return err == nil
}

// Reader returns a reader over a file's contents.
func (img *Image) Reader(e Entry) *io.SectionReader {
	if len(e.extents) == 1 {
		return io.NewSectionReader(img.r, int64(e.extents[0].lba)*sectorSize, e.Size)
	}
	return io.NewSectionReader(&multiReaderAt{r: img.r, extents: e.extents}, 0, e.Size)
}

// ReadFile reads a whole (small) file.
func (img *Image) ReadFile(p string) ([]byte, error) {
	e, err := img.Stat(p)
	if err != nil {
		return nil, err
	}
	if e.IsDir {
		return nil, fs.ErrInvalid
	}
	var b bytes.Buffer
	_, err = io.Copy(&b, img.Reader(e))
	return b.Bytes(), err
}

type multiReaderAt struct {
	r       io.ReaderAt
	extents []extent
}

func (m *multiReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n := 0
	base := int64(0)
	for _, ex := range m.extents {
		size := int64(ex.size)
		if off < base+size && len(p) > 0 {
			rel := off - base
			chunk := p
			if int64(len(chunk)) > size-rel {
				chunk = chunk[:size-rel]
			}
			k, err := m.r.ReadAt(chunk, int64(ex.lba)*sectorSize+rel)
			n += k
			if err != nil {
				return n, err
			}
			p = p[k:]
			off += int64(k)
		}
		base += size
	}
	if len(p) > 0 {
		return n, io.EOF
	}
	return n, nil
}
