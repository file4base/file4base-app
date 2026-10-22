package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Desktop update channel.
//
// The container stack and the installed desktop client are two independent
// update axes. `/api/v1/server/containers` covers the stack and is reserved for
// server administrators, because finishing it means pulling images on the host.
// This endpoint covers the other axis: the natively compiled client that the
// user installed from a .dmg / Setup.exe / .deb, which no `docker compose pull`
// ever touches.
//
// It is deliberately open to any caller: a client too old to sign in must still
// be able to learn that a newer build exists, and no response field is more
// private than the public GitHub release it mirrors. Outbound traffic is bounded
// by the same ten-minute caches the stack check uses, so repeated calls do not
// amplify into GitHub.
const (
	releaseAssetsURLTemplate = "https://api.github.com/repos/file4base/file4base-app/releases/tags/v%s"
	checksumAssetName        = "SHA256SUMS.txt"
	desktopAssetCacheTTL     = 10 * time.Minute
)

// desktopAsset is one downloadable file of a published release.
type desktopAsset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// desktopAssetPreference lists the assets that can carry a platform's update,
// best first: a native installer, then the plain bundle as a fallback. The names
// match the artifacts published by .github/workflows/desktop_release.yml.
var desktopAssetPreference = map[string][]string{
	"macos":   {"File4Base-macOS.dmg", "File4Base-macOS.zip", "File4Base-macOS-universal.zip"},
	"windows": {"File4Base-Windows-x64-Setup.exe", "File4Base-Windows-x64.zip"},
	"linux":   {"File4Base-Linux-x64.deb", "File4Base-Linux-x64.tar.gz"},
}

// normalizeDesktopPlatform maps what a client may report (Platform.operatingSystem,
// a Go GOOS, or a human spelling) onto the three packaged desktop targets.
func normalizeDesktopPlatform(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "macos", "mac", "darwin", "osx", "mac os":
		return "macos", true
	case "windows", "win", "win32", "win64":
		return "windows", true
	case "linux":
		return "linux", true
	default:
		return "", false
	}
}

// fetchReleaseAssets lists the assets of the release tagged v<version> and
// resolves each one's SHA-256 from the release's own SHA256SUMS.txt. Results are
// cached per version so a fleet of clients polling at once costs one request.
//
// A version with no published release (notably the simulated one used to
// rehearse the update banner) yields an empty map and no error: the caller then
// serves the release page instead of a download URL.
func (h *ServerAdminHandler) fetchReleaseAssets(parentCtx context.Context, version string) map[string]desktopAsset {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" {
		return nil
	}

	h.mu.RLock()
	cachedVer := h.desktopAssetsVersion
	cachedAt := h.desktopAssetsCheckedAt
	cached := h.desktopAssets
	h.mu.RUnlock()
	if cachedVer == version && !cachedAt.IsZero() && time.Since(cachedAt) < desktopAssetCacheTTL {
		return cached
	}

	ctx, cancel := context.WithTimeout(parentCtx, 4*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 3500 * time.Millisecond}

	assets := make(map[string]desktopAsset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(releaseAssetsURLTemplate, version), nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "file4base-server/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// No such release yet (404 while a tag is being published, or a
		// simulated version): cache the empty answer so every client does not
		// retry it at once.
		h.storeDesktopAssets(version, assets)
		return assets
	}

	var rel struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil
	}

	checksumURL := ""
	for _, a := range rel.Assets {
		if a.Name == checksumAssetName {
			checksumURL = a.URL
			continue
		}
		assets[a.Name] = desktopAsset{Name: a.Name, URL: a.URL, Size: a.Size}
	}

	if checksumURL != "" {
		for name, sum := range fetchChecksums(ctx, client, checksumURL, version) {
			if asset, ok := assets[name]; ok {
				asset.SHA256 = sum
				assets[name] = asset
			}
		}
	}

	h.storeDesktopAssets(version, assets)
	return assets
}

func (h *ServerAdminHandler) storeDesktopAssets(version string, assets map[string]desktopAsset) {
	h.mu.Lock()
	h.desktopAssets = assets
	h.desktopAssetsVersion = version
	h.desktopAssetsCheckedAt = time.Now().UTC()
	h.mu.Unlock()
}

// fetchChecksums parses a `sha256sum`-style manifest into name -> hex digest.
func fetchChecksums(ctx context.Context, client *http.Client, url, version string) map[string]string {
	sums := make(map[string]string)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return sums
	}
	req.Header.Set("User-Agent", "file4base-server/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return sums
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return sums
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return sums
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 || len(fields[0]) != 64 {
			continue
		}
		// `sha256sum` marks binary mode with a leading '*' on the name.
		name := strings.TrimPrefix(fields[1], "*")
		sums[name] = strings.ToLower(fields[0])
	}
	return sums
}

// resolveDesktopAsset picks the best available asset for a platform.
func resolveDesktopAsset(assets map[string]desktopAsset, platform string) (desktopAsset, bool) {
	for _, name := range desktopAssetPreference[platform] {
		if asset, ok := assets[name]; ok && asset.URL != "" {
			return asset, true
		}
	}
	return desktopAsset{}, false
}

// GetDesktopUpdate reports whether a newer desktop client is published and where
// to get it.
//
// GET /api/v1/updates/desktop?platform=macos|windows|linux&client_version=0.19.2
//
// `update_available` is answered for the caller's own build when it sends
// `client_version`; without it the server's version stands in, which is what a
// client of unknown age gets compared against.
func (h *ServerAdminHandler) GetDesktopUpdate(w http.ResponseWriter, r *http.Request) {
	platform, ok := normalizeDesktopPlatform(r.URL.Query().Get("platform"))
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":  "invalid_platform",
			"detail": "platform must be one of: macos, windows, linux",
		})
		return
	}

	h.ensureVersionChecked(r.Context(), false)

	h.mu.RLock()
	serverVer := h.currentVersion
	latestVer := h.latestVersion
	relURL := h.releaseURL
	relNotes := h.releaseNotes
	checkedAt := h.lastVersionCheck
	h.mu.RUnlock()

	// The installed build the caller is asking on behalf of.
	baseVer := strings.TrimPrefix(strings.TrimSpace(r.URL.Query().Get("client_version")), "v")
	clientVer := baseVer
	if baseVer == "" {
		baseVer = serverVer
	}
	updateAvailable := compareSemVer(latestVer, baseVer) > 0

	resp := map[string]interface{}{
		"platform":         platform,
		"client_version":   clientVer,
		"server_version":   serverVer,
		"latest_version":   latestVer,
		"update_available": updateAvailable,
		"release_url":      relURL,
		"release_notes":    relNotes,
		"checked_at":       checkedAt.Format(time.RFC3339),
	}

	// Only resolve a download when there is something to download: an up to
	// date client needs no asset lookup, and an unpublished (or simulated)
	// version has none to find.
	if updateAvailable {
		if asset, found := resolveDesktopAsset(h.fetchReleaseAssets(r.Context(), latestVer), platform); found {
			resp["asset_name"] = asset.Name
			resp["download_url"] = asset.URL
			resp["size_bytes"] = asset.Size
			resp["sha256"] = asset.SHA256
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}
