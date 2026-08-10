package patch

import (
	"bytes"
	"fmt"
	"regexp"
)

// Ad URL markers — the Rust `&'static str` constants that drive the ad
// webview. Replacing them in place (same length) with a blank URL kills all
// ad-network traffic and the consent flow, with zero structural changes.
//
// The replacement must be exactly as long as the original (43 bytes) because
// Rust stores `&'static str` as (ptr, len) — the length is fixed at compile
// time. `about:blank#` + 31 zeroes is a valid, harmless URL that loads a
// blank page, and the trailing fragment keeps the string the same length.
const (
	adLink   = "https://modrinth.com/wrapper/app-ads-cookie"
	blankURL = "about:blank#0000000000000000000000000000000"
)

// Regex-based markers for the decompressed main JS bundle. Patterns are
// built from literal text via regexp.QuoteMeta with a wildcard for the
// minified function name, so they survive minifier renames.
func bt() string { return "\x60" } // backtick

var jsMarkers = buildMarkers()

type jsMarker struct {
	name     string
	re       *regexp.Regexp
	replace  string
	required bool
}

func buildMarkers() []jsMarker {
	var m []jsMarker
	add := func(name, literal, replace string, required bool) {
		m = append(m, jsMarker{
			name:     name,
			re:       regexp.MustCompile(literal),
			replace:  replace,
			required: required,
		})
	}
	btk := bt()
	// showAd computed:  k=X(()=>p.value&&!O.value&&c.value!==void 0)
	add("showAd computed",
		`X\(\(\)=>\w+\.value&&!\w+\.value&&\w+\.value!==void 0\)`,
		`X(()=>!1)`, true)
	// adConsentAvailable computed:  A=X(()=>c.value!==void 0&&!O.value)
	add("adConsentAvailable computed",
		`X\(\(\)=>\w+\.value!==void 0&&!\w+\.value\)`,
		`X(()=>!1)`, true)
	// watcher: B([k,A],async([e,t])=>{if(e){await Y2(!0);return}await Z2(!0),t&&await Y2()},{immediate:!0})
	// -> B([k,A],()=>{},{immediate:!0})  (k/A names preserved via backrefs)
	add("ads watcher",
		`B\(\[(\w+),(\w+)\],async\(\[(\w+),(\w+)\]\)=>\{if\((\w+)\)\{await (\w+)\(!0\);return\}await (\w+)\(!0\),(\w+)&&await (\w+)\(\)\},\{immediate:!0\}\)`,
		`B([$1,$2],()=>{},{immediate:!0})`, true)
	// helper: async function Y2(e=!1){return await vZ(`plugin:ads|init_ads_window`,{overrideShown:e,dpr:window.devicePixelRatio})}
	add("init_ads_window helper",
		regexp.QuoteMeta("async function ")+`(\w+)`+regexp.QuoteMeta("(e=!1){return await ")+`(\w+)`+regexp.QuoteMeta("("+btk+"plugin:ads|init_ads_window"+btk+",{overrideShown:e,dpr:window.devicePixelRatio})}"),
		`async function $1(e=!1){}`, true)
	// helper: async function X2(){return await vZ(`plugin:ads|show_ads_window`,{dpr:window.devicePixelRatio})}
	add("show_ads_window helper",
		regexp.QuoteMeta("async function ")+`(\w+)`+regexp.QuoteMeta("(){return await ")+`(\w+)`+regexp.QuoteMeta("("+btk+"plugin:ads|show_ads_window"+btk+",{dpr:window.devicePixelRatio})}"),
		`async function $1(){}`, true)
	// helper: async function Z2(e){return await vZ(`plugin:ads|hide_ads_window`,{reset:e})}
	add("hide_ads_window helper",
		regexp.QuoteMeta("async function ")+`(\w+)`+regexp.QuoteMeta("(e){return await ")+`(\w+)`+regexp.QuoteMeta("("+btk+"plugin:ads|hide_ads_window"+btk+",{reset:e})}"),
		`async function $1(e){}`, true)
	// optional promos
	add("Modrinth Plus link", regexp.QuoteMeta("https://modrinth.plus?app"), "about:blank", false)
	add("hosting medal light", regexp.QuoteMeta("https://cdn-raw.modrinth.com/modrinth-hosting-medal-light.webp"), "about:blank", false)
	add("hosting medal dark", regexp.QuoteMeta("https://cdn-raw.modrinth.com/modrinth-hosting-medal-dark.webp"), "about:blank", false)
	return m
}

// ApplyPatches patches one binary: the native ad-URL strings, then the
// embedded main JS bundle. It returns a human-readable summary.
func ApplyPatches(b *Binary) (string, error) {
	var notes []string

	// 1. Native layer: rewrite the ad webview URL.
	//    macOS universal: once per slice (2 total). Windows: once.
	if n, err := b.ReplaceAll([]byte(adLink), []byte(blankURL)); err != nil {
		return "", fmt.Errorf("ad URL string: %w", err)
	} else {
		notes = append(notes, fmt.Sprintf("ad-webview URL rewritten in %d place(s)", n))
	}

	// 2. Embedded frontend layer (per Mach-O slice / PE).
	if err := b.ForEachSlice(func(off, size int) error {
		am, err := parseAssetTable(b, off, size)
		if err != nil {
			return err
		}
		mainKey := am.MainChunkKey()
		if mainKey == "" {
			return fmt.Errorf("main chunk key not found in /index.html")
		}
		js, ok := am.Asset(mainKey)
		if !ok {
			return fmt.Errorf("main chunk %q not found", mainKey)
		}

		patched, changed, err := patchJS(js)
		if err != nil {
			return err
		}
		if changed == 0 {
			return fmt.Errorf("main chunk %q: no ad markers matched (already patched or unsupported version)", mainKey)
		}
		compressed, err := b.ReplaceAsset(am, mainKey, patched)
		if err != nil {
			return err
		}
		notes = append(notes, fmt.Sprintf("slice@%#x: %s rewritten (%d marker(s), %d compressed bytes)", off, mainKey, changed, compressed))
		return nil
	}); err != nil {
		return "", err
	}

	return joinNotes(notes), nil
}

// patchJS applies the jsMarkers to the decompressed JS bundle. It returns the
// patched bytes and how many markers matched. Required markers must match
// exactly once; optional markers are applied when present (0 or 1 times).
func patchJS(js []byte) ([]byte, int, error) {
	out := append([]byte(nil), js...)
	changed := 0
	for _, m := range jsMarkers {
		matches := m.re.FindAll(out, -1)
		switch {
		case len(matches) == 0:
			if m.required {
				return nil, 0, fmt.Errorf("required marker %q not found (unsupported version or already patched)", m.name)
			}
		case len(matches) > 1:
			return nil, 0, fmt.Errorf("marker %q matched %d times (expected 1)", m.name, len(matches))
		default:
			out = m.re.ReplaceAll(out, []byte(m.replace))
			changed++
		}
	}
	if changed == 0 {
		return nil, 0, nil
	}
	return out, changed, nil
}

// IsPatched reports whether the binary already looks patched (idempotency and
// watcher use). It checks both the native URL and the JS gate marker.
func IsPatched(b *Binary) bool {
	if b.Count([]byte(blankURL)) == 0 {
		return false
	}
	// count native AD_LINK occurrences: must be 0
	if b.Count([]byte(adLink)) != 0 {
		return false
	}
	return true
}

func joinNotes(notes []string) string {
	out := ""
	for i, n := range notes {
		if i > 0 {
			out += "; "
		}
		out += n
	}
	return out
}

var _ = bytes.Contains // keep bytes import if unused later
