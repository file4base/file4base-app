package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeDesktopPlatform(t *testing.T) {
	cases := map[string]string{
		"macos":   "macos",
		"macOS":   "macos",
		"darwin":  "macos",
		" mac ":   "macos",
		"osx":     "macos",
		"windows": "windows",
		"Win32":   "windows",
		"linux":   "linux",
		"LINUX":   "linux",
	}
	for raw, want := range cases {
		got, ok := normalizeDesktopPlatform(raw)
		assert.True(t, ok, raw)
		assert.Equal(t, want, got, raw)
	}

	for _, raw := range []string{"", "android", "ios", "web", "freebsd"} {
		_, ok := normalizeDesktopPlatform(raw)
		assert.False(t, ok, raw)
	}
}

func TestResolveDesktopAssetPrefersNativeInstaller(t *testing.T) {
	assets := map[string]desktopAsset{
		"File4Base-macOS.dmg":             {Name: "File4Base-macOS.dmg", URL: "https://example.test/dmg"},
		"File4Base-macOS.zip":             {Name: "File4Base-macOS.zip", URL: "https://example.test/zip"},
		"File4Base-Windows-x64.zip":       {Name: "File4Base-Windows-x64.zip", URL: "https://example.test/winzip"},
		"File4Base-Linux-x64.tar.gz":      {Name: "File4Base-Linux-x64.tar.gz", URL: "https://example.test/targz"},
		"File4Base-Linux-x64.deb":         {Name: "File4Base-Linux-x64.deb", URL: "https://example.test/deb"},
		"File4Base-Windows-x64-Setup.exe": {Name: "File4Base-Windows-x64-Setup.exe", URL: "https://example.test/setup"},
	}

	macAsset, ok := resolveDesktopAsset(assets, "macos")
	require.True(t, ok)
	assert.Equal(t, "File4Base-macOS.dmg", macAsset.Name)

	winAsset, ok := resolveDesktopAsset(assets, "windows")
	require.True(t, ok)
	assert.Equal(t, "File4Base-Windows-x64-Setup.exe", winAsset.Name)

	linuxAsset, ok := resolveDesktopAsset(assets, "linux")
	require.True(t, ok)
	assert.Equal(t, "File4Base-Linux-x64.deb", linuxAsset.Name)
}

func TestResolveDesktopAssetFallsBackToBundle(t *testing.T) {
	// A release that carries only the plain bundles still updates a client.
	assets := map[string]desktopAsset{
		"File4Base-macOS.zip":       {Name: "File4Base-macOS.zip", URL: "https://example.test/zip"},
		"File4Base-Windows-x64.zip": {Name: "File4Base-Windows-x64.zip", URL: "https://example.test/winzip"},
	}

	macAsset, ok := resolveDesktopAsset(assets, "macos")
	require.True(t, ok)
	assert.Equal(t, "File4Base-macOS.zip", macAsset.Name)

	_, ok = resolveDesktopAsset(assets, "linux")
	assert.False(t, ok, "no Linux asset in this release")
}

func TestResolveDesktopAssetIgnoresAssetWithoutURL(t *testing.T) {
	assets := map[string]desktopAsset{
		"File4Base-macOS.dmg": {Name: "File4Base-macOS.dmg"},
		"File4Base-macOS.zip": {Name: "File4Base-macOS.zip", URL: "https://example.test/zip"},
	}
	asset, ok := resolveDesktopAsset(assets, "macos")
	require.True(t, ok)
	assert.Equal(t, "File4Base-macOS.zip", asset.Name)
}

func TestFetchChecksumsParsesManifest(t *testing.T) {
	const manifest = "" +
		"8f5d1a1ca1b2d3e4f5061728394a5b6c7d8e9f001122334455667788990011aa  File4Base-macOS.dmg\n" +
		"AA5D1A1CA1B2D3E4F5061728394A5B6C7D8E9F001122334455667788990011BB *File4Base-Windows-x64-Setup.exe\n" +
		"\n" +
		"# a comment line is ignored\n" +
		"tooshort  File4Base-Linux-x64.deb\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "file4base-server/9.9.9", r.Header.Get("User-Agent"))
		_, _ = w.Write([]byte(manifest))
	}))
	defer srv.Close()

	sums := fetchChecksums(context.Background(), srv.Client(), srv.URL, "9.9.9")
	assert.Equal(t, "8f5d1a1ca1b2d3e4f5061728394a5b6c7d8e9f001122334455667788990011aa", sums["File4Base-macOS.dmg"])
	// Binary-mode '*' stripped and the digest lower-cased.
	assert.Equal(t, "aa5d1a1ca1b2d3e4f5061728394a5b6c7d8e9f001122334455667788990011bb", sums["File4Base-Windows-x64-Setup.exe"])
	// A malformed digest is dropped rather than served as a checksum.
	assert.NotContains(t, sums, "File4Base-Linux-x64.deb")
}

func TestFetchChecksumsToleratesFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	assert.Empty(t, fetchChecksums(context.Background(), srv.Client(), srv.URL, "9.9.9"))
}

func TestFetchReleaseAssetsServesCachedVersion(t *testing.T) {
	// A cache entry within its TTL answers without any outbound request, which
	// is what keeps a fleet of polling clients off GitHub.
	h := &ServerAdminHandler{
		desktopAssets: map[string]desktopAsset{
			"File4Base-macOS.dmg": {Name: "File4Base-macOS.dmg", URL: "https://example.test/dmg", SHA256: "abc"},
		},
		desktopAssetsVersion:   "0.20.0",
		desktopAssetsCheckedAt: time.Now().UTC(),
	}

	assets := h.fetchReleaseAssets(context.Background(), "v0.20.0")
	require.Len(t, assets, 1)
	assert.Equal(t, "https://example.test/dmg", assets["File4Base-macOS.dmg"].URL)

	assert.Nil(t, h.fetchReleaseAssets(context.Background(), ""), "an empty version resolves no assets")
}
