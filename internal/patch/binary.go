// Package patch locates and rewrites byte regions inside the Modrinth App
// native binaries (Mach-O universal on macOS, PE on Windows).
//
// Strategy: all rewrites are same-length or smaller, done in place, so no
// offsets, pointers, or section tables need updating. The only structural
// mutation is the embedded-assets blob, which is padded back to its original
// size so every later pointer stays valid.
package patch

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// ErrNotFound is returned when a required marker is absent (wrong version,
// already patched, or not a Modrinth App binary).
var ErrNotFound = errors.New("required byte pattern not found")

// Binary is a loaded copy of the app binary being patched.
type Binary struct {
	data []byte
}

// Open reads the whole binary into memory.
func Open(path string) (*Binary, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return &Binary{data: b}, nil
}

// Data returns the underlying byte slice.
func (b *Binary) Data() []byte { return b.data }

// Len returns the size in bytes.
func (b *Binary) Len() int { return len(b.data) }

// Write persists the (mutated) binary to path.
func (b *Binary) Write(path string) error {
	return os.WriteFile(path, b.data, 0o755)
}

// findAll returns every occurrence of pattern in the data.
func findAll(data, pattern []byte) []int {
	var out []int
	from := 0
	for {
		i := indexOf(data, pattern, from)
		if i < 0 {
			return out
		}
		out = append(out, i)
		from = i + len(pattern)
	}
}

// ReplaceAll replaces every occurrence of old with rep (same length) and
// returns how many were replaced. It errors if lengths differ or nothing was
// found.
func (b *Binary) ReplaceAll(old, rep []byte) (int, error) {
	if len(old) != len(rep) {
		return 0, fmt.Errorf("replace length mismatch: %d != %d", len(old), len(rep))
	}
	idxs := findAll(b.data, old)
	if len(idxs) == 0 {
		return 0, fmt.Errorf("%w: %q", ErrNotFound, old)
	}
	for _, i := range idxs {
		copy(b.data[i:i+len(rep)], rep)
	}
	return len(idxs), nil
}

// Count returns how many times pattern occurs in the data.
func (b *Binary) Count(pattern []byte) int {
	return len(findAll(b.data, pattern))
}

// indexOf is a small helper.
func indexOf(data, pat []byte, from int) int {
	if len(pat) == 0 {
		return -1
	}
	for i := from; i+len(pat) <= len(data); i++ {
		if data[i] == pat[0] && equalBytes(data[i:i+len(pat)], pat) {
			return i
		}
	}
	return -1
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Mach-O universal (fat) support
// ---------------------------------------------------------------------------

const (
	fatMagic   = 0xcafebabe // big-endian
	fatMagicBE = 0xbebafeca // swapped
	fatMagic64 = 0xcafebabf
)

// machoSlice describes one architecture inside a fat binary.
type machoSlice struct {
	offset uint32
	size   uint32
}

// parseFat returns the slices of a universal binary, or nil if not a fat file.
func parseFat(data []byte) ([]machoSlice, error) {
	if len(data) < 8 {
		return nil, nil
	}
	magic := binary.BigEndian.Uint32(data[0:4])
	var n uint32
	switch magic {
	case fatMagic, fatMagicBE, fatMagic64:
		n = binary.BigEndian.Uint32(data[4:8])
	default:
		return nil, nil
	}
	if n == 0 || n > 64 {
		return nil, fmt.Errorf("implausible fat header arch count %d", n)
	}
	entrySize := 20 // fat_arch
	if magic == fatMagic64 {
		entrySize = 32 // fat_arch_64
	}
	if len(data) < 8+int(n)*entrySize {
		return nil, fmt.Errorf("truncated fat header")
	}
	var out []machoSlice
	for i := 0; i < int(n); i++ {
		e := 8 + i*entrySize
		off := binary.BigEndian.Uint32(data[e+8 : e+12])
		size := binary.BigEndian.Uint32(data[e+12 : e+16])
		if uint64(off)+uint64(size) > uint64(len(data)) {
			return nil, fmt.Errorf("fat slice %d out of bounds (off %d size %d file %d)", i, off, size, len(data))
		}
		out = append(out, machoSlice{offset: off, size: size})
	}
	return out, nil
}

// ForEachSlice runs fn once per Mach-O slice (for fat binaries) or once over
// the whole file (thin). The returned errors abort.
func (b *Binary) ForEachSlice(fn func(offset, size int) error) error {
	slices, err := parseFat(b.data)
	if err != nil {
		return err
	}
	if slices == nil {
		return fn(0, len(b.data))
	}
	for _, s := range slices {
		if err := fn(int(s.offset), int(s.size)); err != nil {
			return err
		}
	}
	return nil
}

// SliceBounds returns (offset,size) of each Mach-O slice in a fat binary, or
// a single (0,len) for thin binaries.
func (b *Binary) SliceBounds() ([]struct{ Off, Size int }, error) {
	slices, err := parseFat(b.data)
	if err != nil {
		return nil, err
	}
	if slices == nil {
		return []struct{ Off, Size int }{{0, len(b.data)}}, nil
	}
	out := make([]struct{ Off, Size int }, 0, len(slices))
	for _, s := range slices {
		out = append(out, struct{ Off, Size int }{int(s.offset), int(s.size)})
	}
	return out, nil
}
