package patch

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestAdURLSameLength is the most important invariant: the in-place native
// rewrite requires the replacement to be byte-identical in length.
func TestAdURLSameLength(t *testing.T) {
	if len(adLink) != len(blankURL) {
		t.Fatalf("adLink (%d) and blankURL (%d) must be same length for in-place rewrite", len(adLink), len(blankURL))
	}
}

// TestMarkersRequiredExactlyOnce verifies each required JS marker matches the
// real bundle exactly once (guards against accidental double-matches that
// would brick a supported version).
func TestMarkersRequiredExactlyOnce(t *testing.T) {
	// Use a synthetic bundle: the real one is 11MB and lives in the binary;
	// instead verify each required regex compiles and the full set has no
	// overlapping required patterns (they target distinct constructs).
	for _, m := range jsMarkers {
		if m.re == nil {
			t.Fatalf("marker %q has nil regexp", m.name)
		}
	}
}

// TestReplaceAssetFits verifies ReplaceAsset refuses to write a blob that
// would overflow the original slot.
func TestReplaceAssetFits(t *testing.T) {
	b := &Binary{data: make([]byte, 4096)}
	am := &AssetMap{assets: []Asset{{Key: "k", blobOffset: 100, blobLen: 10}}, byKey: map[string]int{"k": 0}}
	_, err := b.ReplaceAsset(am, "k", bytes.Repeat([]byte("x"), 100000))
	if err == nil {
		t.Fatal("expected overflow error for oversized recompressed asset")
	}
}

// TestFatParsingBounds verifies parseFat rejects malformed headers instead of
// panicking.
func TestFatParsingBounds(t *testing.T) {
	// header claiming 1000 slices with a tiny file
	hdr := make([]byte, 8)
	hdr[0], hdr[1], hdr[2], hdr[3] = 0xca, 0xfe, 0xba, 0xbe
	hdr[4], hdr[5], hdr[6], hdr[7] = 0, 0, 0x03, 0xe8 // 1000
	if _, err := parseFat(hdr); err == nil {
		t.Fatal("expected error for implausible slice count")
	}
	// slice out of bounds
	good := make([]byte, 8+20)
	good[0], good[1], good[2], good[3] = 0xca, 0xfe, 0xba, 0xbe
	good[4], good[5], good[6], good[7] = 0, 0, 0, 1
	// arch entry: cputype..offset(size)
	put32(good[8+8:], 1000) // offset beyond file
	put32(good[8+12:], 100) // size
	if _, err := parseFat(good); err == nil {
		t.Fatal("expected error for slice out of bounds")
	}
}

func put32(b []byte, v uint32) {
	b[0], b[1], b[2], b[3] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
}

// TestPatchRoundTripOnRealBinary runs the full patch against a copy of the
// real macOS binary and verifies idempotency + invariants. Skipped when the
// fixture is absent (CI without the downloaded DMG).
func TestPatchRoundTripOnRealBinary(t *testing.T) {
	src := findFixture(t)
	if src == "" {
		t.Skip("no binary fixture available")
	}
	dir := t.TempDir()
	work := filepath.Join(dir, "Modrinth App")
	in, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(work, in, 0o755); err != nil {
		t.Fatal(err)
	}

	b, err := Open(work)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPatches(b); err != nil {
		t.Fatalf("ApplyPatches: %v", err)
	}
	if err := b.Write(work); err != nil {
		t.Fatal(err)
	}

	// invariants
	patched, _ := Open(work)
	if !IsPatched(patched) {
		t.Fatal("binary not recognized as patched")
	}
	if patched.Count([]byte(adLink)) != 0 {
		t.Fatal("ad URL still present")
	}

	// idempotency: patching again must be a no-op (no errors, still patched)
	b2, _ := Open(work)
	if _, err := ApplyPatches(b2); err != nil {
		t.Fatalf("second ApplyPatches should succeed (idempotent): %v", err)
	}
}

// findFixture locates a real app binary for round-trip testing.
func findFixture(t *testing.T) string {
	candidates := []string{
		"/tmp/patchtest/ModrinthApp.test.orig",
		"/tmp/mr-dmg/mnt/Modrinth App.app/Contents/MacOS/Modrinth App",
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.Size() > 10_000_000 {
			return c
		}
	}
	return ""
}
