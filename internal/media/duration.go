// Package media reads how long a video or audio file plays for, from its header, without
// decoding it. It knows Matroska (MKV, WebM), MP4 and QuickTime (MP4, M4V, MOV, M4A), FLAC
// and MP3; anything else, such as a ROM, a book or a PDF, has no duration.
package media

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"path"
	"strings"
	"time"
)

var errNotFound = errors.New("no duration in the header")

// IsTimed reports whether name is a kind of file whose duration can be read.
func IsTimed(name string) bool {
	return reader(name) != nil
}

func reader(name string) func(io.ReaderAt, int64) (time.Duration, error) {
	switch strings.ToLower(path.Ext(name)) {
	case ".mkv", ".webm", ".mka", ".mk3d":
		return matroska
	case ".mp4", ".m4v", ".mov", ".m4a", ".m4b", ".3gp":
		return mp4
	case ".flac":
		return flac
	case ".mp3":
		return mp3
	}
	return nil
}

// Duration is how long the file named name, of size bytes, plays for. It reports false for
// a kind of file it doesn't know, or a header it can't read.
func Duration(r io.ReaderAt, size int64, name string) (time.Duration, bool) {
	read := reader(name)
	if read == nil {
		return 0, false
	}
	d, err := read(r, size)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}

func readAt(r io.ReaderAt, off int64, n int) ([]byte, error) {
	b := make([]byte, n)
	m, err := r.ReadAt(b, off)
	if m == n {
		return b, nil
	}
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	return nil, err
}

// --- Matroska: Segment > Info > TimestampScale and Duration ---

const (
	ebmlSegment   = 0x18538067
	ebmlInfo      = 0x1549A966
	ebmlCluster   = 0x1F43B675
	ebmlTimescale = 0x2AD7B1
	ebmlDuration  = 0x4489
	// maxMatroskaScan bounds how far into the file the Info element is looked for; it comes
	// before the first cluster.
	maxMatroskaScan = 4 << 20
)

// vint reads an EBML variable-length integer at off: its value with the length marker kept
// (an element ID) or cleared (a size), and its length.
func vint(r io.ReaderAt, off int64, keepMarker bool) (uint64, int, error) {
	b, err := readAt(r, off, 1)
	if err != nil {
		return 0, 0, err
	}
	n := 1
	for mask := byte(0x80); n <= 8 && b[0]&mask == 0; mask >>= 1 {
		n++
	}
	if n > 8 {
		return 0, 0, errNotFound
	}
	all, err := readAt(r, off, n)
	if err != nil {
		return 0, 0, err
	}
	v := uint64(all[0])
	if !keepMarker {
		v &= uint64(0xFF >> n)
	}
	for _, c := range all[1:] {
		v = v<<8 | uint64(c)
	}
	return v, n, nil
}

// element reads the header of the element at off: its ID, the offset of its data, and its
// size, or -1 for an unknown size.
func element(r io.ReaderAt, off int64) (id uint64, data, size int64, err error) {
	id, idLen, err := vint(r, off, true)
	if err != nil {
		return 0, 0, 0, err
	}
	sz, szLen, err := vint(r, off+int64(idLen), false)
	if err != nil {
		return 0, 0, 0, err
	}
	size = int64(sz)
	if sz == (uint64(1)<<(7*szLen))-1 {
		size = -1
	}
	return id, off + int64(idLen+szLen), size, nil
}

func matroska(r io.ReaderAt, size int64) (time.Duration, error) {
	// the EBML header, then the Segment
	_, data, hsize, err := element(r, 0)
	if err != nil || hsize < 0 {
		return 0, errNotFound
	}
	id, off, segSize, err := element(r, data+hsize)
	if err != nil || id != ebmlSegment {
		return 0, errNotFound
	}
	end := size
	if segSize >= 0 {
		end = min(end, off+segSize)
	}
	for off < end && off < maxMatroskaScan {
		id, data, sz, err := element(r, off)
		if err != nil || sz < 0 || id == ebmlCluster {
			return 0, errNotFound
		}
		if id == ebmlInfo {
			return matroskaInfo(r, data, data+sz)
		}
		off = data + sz
	}
	return 0, errNotFound
}

func matroskaInfo(r io.ReaderAt, off, end int64) (time.Duration, error) {
	scale := uint64(1_000_000) // nanoseconds a tick, the default
	var ticks float64
	for off < end {
		id, data, sz, err := element(r, off)
		if err != nil || sz < 0 || sz > 8 {
			off = data + max(sz, 0)
			if err != nil {
				return 0, err
			}
			continue
		}
		b, err := readAt(r, data, int(sz))
		if err != nil {
			return 0, err
		}
		switch id {
		case ebmlTimescale:
			scale = 0
			for _, c := range b {
				scale = scale<<8 | uint64(c)
			}
		case ebmlDuration:
			switch sz {
			case 4:
				ticks = float64(math.Float32frombits(binary.BigEndian.Uint32(b)))
			case 8:
				ticks = math.Float64frombits(binary.BigEndian.Uint64(b))
			}
		}
		off = data + sz
	}
	if ticks <= 0 || scale == 0 {
		return 0, errNotFound
	}
	return time.Duration(ticks * float64(scale)), nil
}

// --- MP4 and QuickTime: moov > mvhd's timescale and duration ---

// box reads the header of the box at off: its type, the offset of its data, and the offset
// it ends at.
func box(r io.ReaderAt, off, limit int64) (typ string, data, end int64, err error) {
	h, err := readAt(r, off, 8)
	if err != nil {
		return "", 0, 0, err
	}
	size := int64(binary.BigEndian.Uint32(h))
	typ, data = string(h[4:8]), off+8
	switch size {
	case 0:
		size = limit - off
	case 1:
		l, err := readAt(r, off+8, 8)
		if err != nil {
			return "", 0, 0, err
		}
		size, data = int64(binary.BigEndian.Uint64(l)), off+16
	}
	if size < data-off {
		return "", 0, 0, errNotFound
	}
	return typ, data, off + size, nil
}

// findBox is the data and end of the first box of type typ between off and end.
func findBox(r io.ReaderAt, off, end int64, typ string) (int64, int64, error) {
	for off < end {
		t, data, next, err := box(r, off, end)
		if err != nil {
			return 0, 0, err
		}
		if t == typ {
			return data, next, nil
		}
		off = next
	}
	return 0, 0, errNotFound
}

func mp4(r io.ReaderAt, size int64) (time.Duration, error) {
	moov, moovEnd, err := findBox(r, 0, size, "moov")
	if err != nil {
		return 0, err
	}
	mvhd, _, err := findBox(r, moov, moovEnd, "mvhd")
	if err != nil {
		return 0, err
	}
	v, err := readAt(r, mvhd, 1)
	if err != nil {
		return 0, err
	}
	var scale, dur uint64
	if v[0] == 1 {
		b, err := readAt(r, mvhd+4+16, 12)
		if err != nil {
			return 0, err
		}
		scale, dur = uint64(binary.BigEndian.Uint32(b)), binary.BigEndian.Uint64(b[4:])
	} else {
		b, err := readAt(r, mvhd+4+8, 8)
		if err != nil {
			return 0, err
		}
		scale, dur = uint64(binary.BigEndian.Uint32(b)), uint64(binary.BigEndian.Uint32(b[4:]))
	}
	if scale == 0 || dur == 0 || dur == math.MaxUint32 || dur == math.MaxUint64 {
		return 0, errNotFound
	}
	return time.Duration(float64(dur) / float64(scale) * float64(time.Second)), nil
}

// --- FLAC: STREAMINFO's sample rate and total samples ---

func flac(r io.ReaderAt, _ int64) (time.Duration, error) {
	b, err := readAt(r, 0, 26)
	if err != nil || string(b[:4]) != "fLaC" || b[4]&0x7F != 0 {
		return 0, errNotFound
	}
	// STREAMINFO from byte 8: block sizes (4), frame sizes (6), then 20 bits of sample rate,
	// 3 of channels, 5 of bits a sample and 36 of total samples
	v := binary.BigEndian.Uint64(b[18:26])
	rate := v >> 44
	total := v & (1<<36 - 1)
	if rate == 0 || total == 0 {
		return 0, errNotFound
	}
	return time.Duration(float64(total) / float64(rate) * float64(time.Second)), nil
}

// --- MP3: a Xing, Info or VBRI frame count, else the first frame's bitrate ---

var (
	mp3Bitrates = [2][16]int{ // kbit/s, layer III; MPEG-1, then MPEG-2 and 2.5
		{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
	}
	mp3Rates = [3][3]int{{44100, 48000, 32000}, {22050, 24000, 16000}, {11025, 12000, 8000}}
)

// maxMP3Scan bounds how far past the tags the first frame is looked for.
const maxMP3Scan = 64 << 10

func mp3(r io.ReaderAt, size int64) (time.Duration, error) {
	off := int64(0)
	if h, err := readAt(r, 0, 10); err == nil && string(h[:3]) == "ID3" {
		off = 10 + int64(h[6]&0x7F)<<21 | int64(h[7]&0x7F)<<14 | int64(h[8]&0x7F)<<7 | int64(h[9]&0x7F)
	}
	buf, err := readAt(r, off, int(min(maxMP3Scan, size-off)))
	if err != nil {
		return 0, err
	}
	for i := 0; i+4 <= len(buf); i++ {
		if buf[i] != 0xFF || buf[i+1]&0xE0 != 0xE0 {
			continue
		}
		ver := (buf[i+1] >> 3) & 3 // 3 MPEG-1, 2 MPEG-2, 0 MPEG-2.5
		layer := (buf[i+1] >> 1) & 3
		bi, ri := buf[i+2]>>4, (buf[i+2]>>2)&3
		if ver == 1 || layer != 1 || bi == 0 || bi == 15 || ri == 3 {
			continue
		}
		v := map[byte]int{3: 0, 2: 1, 0: 2}[ver]
		rate := mp3Rates[v][ri]
		kbps := mp3Bitrates[min(v, 1)][bi]
		samples := 1152
		if v > 0 {
			samples = 576
		}
		mono := buf[i+3]>>6 == 3
		side := map[[2]bool]int{{false, false}: 32, {false, true}: 17, {true, false}: 17, {true, true}: 9}[[2]bool{v > 0, mono}]
		if frames, ok := mp3Frames(buf[i:], 4+side); ok {
			return time.Duration(float64(frames) * float64(samples) / float64(rate) * float64(time.Second)), nil
		}
		// constant bitrate: the rest of the file at the first frame's rate
		bytes := size - off - int64(i)
		return time.Duration(float64(bytes) * 8 / float64(kbps*1000) * float64(time.Second)), nil
	}
	return 0, errNotFound
}

// mp3Frames reads the frame count of a Xing or Info header at at, or a VBRI one at 36.
func mp3Frames(f []byte, at int) (uint32, bool) {
	if len(f) >= at+12 {
		tag := string(f[at : at+4])
		if (tag == "Xing" || tag == "Info") && f[at+7]&1 != 0 {
			return binary.BigEndian.Uint32(f[at+8:]), true
		}
	}
	if len(f) >= 36+18 && string(f[36:40]) == "VBRI" {
		return binary.BigEndian.Uint32(f[36+14:]), true
	}
	return 0, false
}
