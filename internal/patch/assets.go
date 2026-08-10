package patch

import (
	"bytes"
	"fmt"
	"io"

	"github.com/andybalholm/brotli"
)

// Asset is one entry of the Tauri v2 embedded-assets map.
type Asset struct {
	Key   string
	Value []byte // decompressed
	// bookkeeping for in-place rewrite
	blobOffset int // file offset where the brotli blob starts
	blobLen    int // original compressed size (must be preserved by padding)
}

// AssetMap is the full embedded asset table for one Mach-O slice / PE.
type AssetMap struct {
	assets []Asset
	byKey  map[string]int // key -> index into assets
}

// parseAssetTable discovers all embedded assets by scanning for their key
// strings. In the compiled binary each asset is stored as
// `[key bytes][brotli-compressed value]` contiguously in __TEXT, with the
// phf table pointing at each. We locate the keys, decompress the following
// bytes to validate, and record (offset, len).
func parseAssetTable(b *Binary, sliceOff, sliceLen int) (*AssetMap, error) {
	am := &AssetMap{byKey: map[string]int{}}
	// top-level keys and chunk keys
	assetsPat := []byte("/assets/")
	topPats := [][]byte{[]byte("/index.html"), []byte("/vite.svg"), []byte("/manifest.js")}
	scan := sliceOff
	limit := sliceOff + sliceLen

	// find a valid asset key first to anchor the scan. The blob after the key
	// is bounded by the next "/assets/" key string; probe that exact window.
	anchor := -1
	for {
		i := indexOf(b.data, assetsPat, scan)
		if i < 0 || i >= limit {
			break
		}
		key, end := readAssetKey(b.data, i, limit)
		if key != "" {
			next := indexOf(b.data, assetsPat, end)
			if next < 0 || next >= limit {
				next = limit
			}
			if next > end && probeBrotli(b.data[end:next]) {
				anchor = i
				break
			}
		}
		scan = i + 1
	}
	if anchor < 0 {
		return nil, fmt.Errorf("%w: no /assets/ entries", ErrNotFound)
	}

	// Pre-pass: top-level assets (/index.html etc). They are stored like the
	// /assets/ ones: key string then brotli blob. Scan for each top key,
	// probe the following bytes, and record when valid.
	for _, tp := range topPats {
		tScan := sliceOff
		for {
			i := indexOf(b.data, tp, tScan)
			if i < 0 || i >= limit {
				break
			}
			end := i + len(tp)
			// The blob may contain "/assets/" sequences (compressed data is
			// arbitrary bytes), so don't trust the next-key boundary. Probe
			// with a tolerant wide window: the brotli stream self-terminates.
			val, err := decompressAt(b.data, end, limit)
			if err == nil {
				key := string(tp)
				if _, dup := am.byKey[key]; !dup {
					idx := len(am.assets)
					am.assets = append(am.assets, Asset{
						Key:        key,
						Value:      val,
						blobOffset: end,
						blobLen:    blobLenFor(b.data, end, limit),
					})
					am.byKey[key] = idx
				}
				break // only the asset instance (followed by brotli) matters
			}
			tScan = i + 1
		}
	}

	scan = anchor
	for scan < limit {
		i := indexOf(b.data, assetsPat, scan)
		if i < 0 || i >= limit {
			break
		}
		key, end := readAssetKey(b.data, i, limit)
		if key == "" {
			scan = i + 1
			continue
		}
		if _, dup := am.byKey[key]; dup {
			scan = i + 1
			continue
		}
		// Tolerant decompression: the stream self-terminates, so we don't
		// need an exact boundary. We only require the key to be followed by
		// some valid brotli stream before the slice end.
		val, err := decompressAt(b.data, end, limit)
		if err != nil {
			// not an asset value (incidental string); skip this occurrence
			scan = i + 1
			continue
		}
		idx := len(am.assets)
		am.assets = append(am.assets, Asset{
			Key:        key,
			Value:      val,
			blobOffset: end,
			blobLen:    blobLenFor(b.data, end, limit),
		})
		am.byKey[key] = idx
		scan = i + 1
	}

	if len(am.assets) == 0 {
		return nil, fmt.Errorf("%w: no assets found", ErrNotFound)
	}
	return am, nil
}

// readAssetKey parses an asset key starting at i (which must point at
// "/assets/") and returns the key and the offset just past it.
func readAssetKey(data []byte, i, limit int) (string, int) {
	end := i
	for end < limit && end < i+120 {
		c := data[end]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '/' || c == '.' || c == '-' ||
			c == '_' || c == '@' {
			end++
			continue
		}
		break
	}
	key := string(data[i:end])
	if len(key) < len("/assets/x.js") {
		return "", i
	}
	return key, end
}

// MainChunkKey returns the key of the main JS bundle (from /index.html).
func (am *AssetMap) MainChunkKey() string {
	idx, ok := am.byKey["/index.html"]
	if !ok {
		return ""
	}
	return extractMainChunkKey(am.assets[idx].Value)
}

// CSSChunkKey returns the key of the main stylesheet (from /index.html).
func (am *AssetMap) CSSChunkKey() string {
	idx, ok := am.byKey["/index.html"]
	if !ok {
		return ""
	}
	return extractCSSChunkKey(am.assets[idx].Value)
}

// Asset returns the decompressed value for a key.
func (am *AssetMap) Asset(key string) ([]byte, bool) {
	idx, ok := am.byKey[key]
	if !ok {
		return nil, false
	}
	return am.assets[idx].Value, true
}

// ReplaceAsset recompresses value for key (brotli q9, lgwin24) and writes it
// in place at the original blob offset, zero-padded to the original size.
// If q9 output exceeds the original blob, it retries at q11 (always smaller);
// the smallest fitting result is used. Returns the compressed size used.
func (b *Binary) ReplaceAsset(am *AssetMap, key string, value []byte) (int, error) {
	idx, ok := am.byKey[key]
	if !ok {
		return 0, fmt.Errorf("asset %q not found", key)
	}
	a := &am.assets[idx]
	comp, err := brotliCompress(value, 9)
	if err != nil {
		return 0, err
	}
	if len(comp) > a.blobLen {
		// q9 didn't fit (e.g. a new app version with a denser original);
		// q11 compresses harder and is always <= q9 output.
		comp2, err2 := brotliCompress(value, 11)
		if err2 != nil {
			return 0, err2
		}
		comp = comp2
	}
	if len(comp) > a.blobLen {
		return 0, fmt.Errorf("recompressed asset %q (%d) exceeds original (%d)", key, len(comp), a.blobLen)
	}
	// zero the original region, then write the new stream at the start
	for i := 0; i < a.blobLen; i++ {
		b.data[a.blobOffset+i] = 0
	}
	copy(b.data[a.blobOffset:a.blobOffset+len(comp)], comp)
	return len(comp), nil
}

// decompressAt brotli-decompresses data[start:end]. end is a *hint* (the
// presumed blob boundary); if decompression with that window fails due to a
// false boundary (e.g. a "/assets/" string occurring inside the compressed
// stream), it retries with a wide window up to the end of the data, treating
// the stream's own end-of-stream as the real boundary.
func decompressAt(data []byte, start, end int) ([]byte, error) {
	if end > len(data) {
		end = len(data)
	}
	if start >= end {
		return nil, fmt.Errorf("empty range")
	}
	out, err := brotliDecompress(bytes.NewReader(data[start:end]))
	if err == nil {
		return out, nil
	}
	// Retry with the full remaining data as window. The brotli reader stops at
	// the stream terminator; trailing bytes surface as "excessive input",
	// which we treat as a clean end.
	out, err2 := brotliDecompressTolerant(bytes.NewReader(data[start:]))
	if err2 == nil {
		return out, nil
	}
	return nil, err
}

// probeBrotli reports whether data begins with a valid brotli stream (using a
// tolerant read that accepts trailing bytes).
func probeBrotli(data []byte) bool {
	_, err := brotliDecompressTolerant(bytes.NewReader(data))
	return err == nil
}

// brotliDecompressTolerant reads a brotli stream and accepts the library's
// "excessive input" error (which it reports when trailing bytes follow the
// stream terminator) as a clean EOF.
func brotliDecompressTolerant(r io.Reader) ([]byte, error) {
	out, err := io.ReadAll(brotli.NewReader(r))
	if err != nil && err.Error() == "brotli: excessive input" {
		return out, nil
	}
	return out, err
}

// extractMainChunkKey parses the main JS path from /index.html content.
func extractMainChunkKey(html []byte) string {
	needle := []byte(`src="/assets/`)
	i := bytes.Index(html, needle)
	if i < 0 {
		return ""
	}
	start := i + len(needle)
	end := start
	for end < len(html) && html[end] != '"' {
		end++
	}
	return "/assets/" + string(html[start:end])
}

// extractCSSChunkKey parses the main stylesheet path from /index.html content.
// There may be several href="/assets/..." links (modulepreload js, css);
// find the one whose path ends in .css.
func extractCSSChunkKey(html []byte) string {
	needle := []byte(`href="/assets/`)
	from := 0
	for {
		i := bytes.Index(html[from:], needle)
		if i < 0 {
			return ""
		}
		i += from
		start := i + len(needle)
		end := start
		for end < len(html) && html[end] != '"' {
			end++
		}
		key := "/assets/" + string(html[start:end])
		if len(key) >= 4 && key[len(key)-4:] == ".css" {
			return key
		}
		from = end + 1
	}
}

// blobLenFor estimates the compressed stream length for an asset whose blob
// starts at `start`. The stream self-terminates; we find its true end by
// scanning forward for the next *valid* asset key (one whose following bytes
// decompress). As a fallback it returns the distance to the next "/assets/"
// string — an upper bound, which is all the in-place rewrite needs.
func blobLenFor(data []byte, start, limit int) int {
	scan := start
	for {
		next := indexOf(data, []byte("/assets/"), scan)
		if next < 0 || next >= limit {
			return limit - start
		}
		key, end := readAssetKey(data, next, limit)
		if key != "" {
			// candidate boundary: verify it's a real asset (decompresses)
			if _, err := brotliDecompressTolerant(bytes.NewReader(data[end:limit])); err == nil {
				return next - start
			}
		}
		scan = next + 1
	}
}

// brotliCompress compresses data at the given quality.
// brotliCompress compresses data at the given quality with a 24-bit sliding
// window (LGWin=24), matching the settings the Modrinth App's build pipeline
// uses. The default LGWin (22) can produce output LARGER than the original
// embedded blob, which breaks the in-place rewrite.
func brotliCompress(data []byte, quality int) ([]byte, error) {
	var buf bytes.Buffer
	w := brotli.NewWriterOptions(&buf, brotli.WriterOptions{
		Quality: quality,
		LGWin:   24,
	})
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func brotliDecompress(r io.Reader) ([]byte, error) {
	return io.ReadAll(brotli.NewReader(r))
}
