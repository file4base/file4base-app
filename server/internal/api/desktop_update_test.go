package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/file4base/file4base-app/server/internal/api"
)

type desktopUpdateResponse struct {
	Platform        string `json:"platform"`
	ClientVersion   string `json:"client_version"`
	ServerVersion   string `json:"server_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	ReleaseURL      string `json:"release_url"`
	CheckedAt       string `json:"checked_at"`
	AssetName       string `json:"asset_name"`
	DownloadURL     string `json:"download_url"`
	SHA256          string `json:"sha256"`
}

// The desktop update channel answers without a session: a client too old to
// sign in must still be able to find its own update.
func TestDesktopUpdateNeedsNoSession(t *testing.T) {
	h := newAPIHarness(t, api.Options{Version: "0.19.2"})

	var res desktopUpdateResponse
	rec := h.do(http.MethodGet, "/api/v1/updates/desktop?platform=macos&client_version=999.0.0", "", nil, &res)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "macos", res.Platform)
	assert.Equal(t, "999.0.0", res.ClientVersion)
	assert.Equal(t, "0.19.2", res.ServerVersion)
	assert.NotEmpty(t, res.ReleaseURL)
	assert.NotEmpty(t, res.CheckedAt)
	// A client newer than anything published is up to date, so no asset is
	// resolved for it (and no release lookup is spent on one).
	assert.False(t, res.UpdateAvailable)
	assert.Empty(t, res.DownloadURL)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}

func TestDesktopUpdateRejectsUnknownPlatform(t *testing.T) {
	h := newAPIHarness(t, api.Options{Version: "0.19.2"})

	for _, target := range []string{
		"/api/v1/updates/desktop",
		"/api/v1/updates/desktop?platform=android",
		"/api/v1/updates/desktop?platform=web",
	} {
		var res struct {
			Error string `json:"error"`
		}
		rec := h.do(http.MethodGet, target, "", nil, &res)
		assert.Equal(t, http.StatusBadRequest, rec.Code, target)
		assert.Equal(t, "invalid_platform", res.Error, target)
	}
}

// An outdated client always gets somewhere to go: a resolved asset when the
// release carries one for its platform, the release page otherwise (a version
// with no published assets, notably the simulated one used to rehearse the
// update banner, must not leave the client with a dead URL).
func TestDesktopUpdateOutdatedClientGetsSomewhereToGo(t *testing.T) {
	h := newAPIHarness(t, api.Options{Version: "0.19.2"})

	var res desktopUpdateResponse
	rec := h.do(http.MethodGet, "/api/v1/updates/desktop?platform=linux&client_version=0.0.1", "", nil, &res)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "linux", res.Platform)
	assert.True(t, res.UpdateAvailable, "0.0.1 is older than any release")
	assert.NotEmpty(t, res.ReleaseURL, "the release page is the floor")
	// Whether a download URL comes back depends on what the published release
	// carries, so only the invariant is asserted: a URL always names its asset.
	if res.DownloadURL != "" {
		assert.NotEmpty(t, res.AssetName)
	}
}
