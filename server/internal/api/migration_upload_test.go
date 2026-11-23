package api_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/stretchr/testify/require"
)

// nativeVisitsDesign declares the table and field the synthetic native files
// below hold records for.
const nativeVisitsDesign = `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog><BaseTable id="129" name="Visits"><FieldCatalog><Field id="1" name="Amount" dataType="Number"/></FieldCatalog></BaseTable></BaseTableCatalog></File></FMPReport>`

// syntheticNativeFile builds an original HBAM7 file whose single leaf block
// holds one Visits record with Amount = value, followed by padPages empty
// pages that are not part of the leaf chain.
func syntheticNativeFile(value string, padPages int) []byte {
	file := make([]byte, (3+padPages)*4096)
	copy(file[15:20], "HBAM7")
	binary.BigEndian.PutUint16(file[4096+12:], 2)
	payload := []byte{0x28, 0x80, 1, 0x20, 5, 0x20, 1, 0x06, 1, byte(len(value))}
	for _, b := range []byte(value) {
		payload = append(payload, b^0x5a)
	}
	copy(file[2*4096+20:], payload)
	return file
}

type uploadPart struct {
	name, filename string
	body           []byte
}

func multipartBody(t *testing.T, parts []uploadPart) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		var (
			w   interface{ Write([]byte) (int, error) }
			err error
		)
		if p.filename != "" {
			w, err = mw.CreateFormFile(p.name, p.filename)
		} else {
			w, err = mw.CreateFormField(p.name)
		}
		require.NoError(t, err)
		_, err = w.Write(p.body)
		require.NoError(t, err)
	}
	require.NoError(t, mw.Close())
	return &buf, mw.FormDataContentType()
}

func (h *apiHarness) doMultipart(target, token string, parts []uploadPart, out interface{}) *httptest.ResponseRecorder {
	h.t.Helper()
	body, contentType := multipartBody(h.t, parts)
	req := httptest.NewRequest(http.MethodPost, target, body)
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	if out != nil && rec.Body.Len() > 0 {
		require.NoError(h.t, json.Unmarshal(rec.Body.Bytes(), out), rec.Body.String())
	}
	return rec
}

func requestPart(t *testing.T, fields map[string]interface{}) uploadPart {
	t.Helper()
	raw, err := json.Marshal(fields)
	require.NoError(t, err)
	return uploadPart{name: "request", body: raw}
}

type migrationCounts struct {
	Report struct {
		Counts struct {
			Detected int `json:"records_detected"`
			Ready    int `json:"records_ready"`
			Rejected int `json:"rejected_count"`
		} `json:"counts"`
	} `json:"report"`
}

// A native copy sent as a multipart part is converted exactly like the same
// copy sent as base64 inside JSON, and applies its records (#126).
func TestAPI_MigrationMultipartNativeMatchesJSON(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	database := uniqueDB("f4b_native_upload")
	h.createDatabase(database, "qualifier", "synthetic-test-password")
	token := h.login(database, "qualifier", "synthetic-test-password")
	file := syntheticNativeFile("12", 0)

	var viaJSON, viaMultipart migrationCounts
	rec := h.do(http.MethodPost, "/api/v1/solutions/migrate/preflight", token, map[string]interface{}{
		"design_xml": nativeVisitsDesign, "fmp12_base64": base64.StdEncoding.EncodeToString(file),
	}, &viaJSON)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = h.doMultipart("/api/v1/solutions/migrate/preflight", token, []uploadPart{
		{name: "native", filename: "Synthetic.fmp12", body: file},
		requestPart(t, map[string]interface{}{"design_xml": nativeVisitsDesign}),
	}, &viaMultipart)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, viaJSON.Report.Counts.Detected)
	require.Equal(t, 1, viaJSON.Report.Counts.Ready)
	require.Equal(t, viaJSON.Report.Counts, viaMultipart.Report.Counts)

	// The design may travel as its own part as well.
	var result struct {
		RecordsInserted int `json:"records_inserted"`
	}
	rec = h.doMultipart("/api/v1/solutions/migrate/apply", token, []uploadPart{
		requestPart(t, map[string]interface{}{}),
		{name: "design", filename: "Synthetic.xml", body: []byte(nativeVisitsDesign)},
		{name: "native", filename: "Synthetic.fmp12", body: file},
	}, &result)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, 1, result.RecordsInserted)
	var rows []map[string]interface{}
	rec = h.do(http.MethodGet, "/api/v1/data/visits/", token, nil, &rows)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, rows, 1)
	require.Equal(t, "12", fmt.Sprint(rows[0]["amount"]))
}

// A native copy above the 64 MiB JSON ceiling can be applied as a multipart
// part, while the same request as JSON is refused with a pointer to it.
func TestAPI_MigrationNativeAboveJSONLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates a native file above 64 MiB")
	}
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	database := uniqueDB("f4b_native_large")
	h.createDatabase(database, "qualifier", "synthetic-test-password")
	token := h.login(database, "qualifier", "synthetic-test-password")
	file := syntheticNativeFile("7", (64<<20)/4096)
	require.Greater(t, len(file), 64<<20)

	// JSON: the body is cut at the ceiling and reported as too large, not as
	// malformed JSON.
	oversized := `{"design_xml":"` + strings.Repeat(" ", 64<<20) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/solutions/migrate/preflight", strings.NewReader(oversized))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "multipart/form-data")

	var result struct {
		RecordsInserted int `json:"records_inserted"`
	}
	rec = h.doMultipart("/api/v1/solutions/migrate/apply", token, []uploadPart{
		requestPart(t, map[string]interface{}{"design_xml": nativeVisitsDesign}),
		{name: "native", filename: "Large.fmp12", body: file},
	}, &result)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, 1, result.RecordsInserted)

	// Without a session the JSON ceiling still applies to a multipart upload,
	// so public database creation never admits anonymous gigabyte uploads.
	rec = h.doMultipart("/api/v1/solutions/migrate/preflight", "", []uploadPart{
		requestPart(t, map[string]interface{}{"design_xml": nativeVisitsDesign}),
		{name: "native", filename: "Large.fmp12", body: file},
	}, nil)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "without a session")
}

func TestAPI_MigrationMultipartRefusesInvalidUploads(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true, MaxMigrationUploadBytes: 1 << 20})
	database := uniqueDB("f4b_native_invalid")
	h.createDatabase(database, "qualifier", "synthetic-test-password")
	token := h.login(database, "qualifier", "synthetic-test-password")
	file := syntheticNativeFile("12", 0)
	base := map[string]interface{}{"design_xml": nativeVisitsDesign}

	cases := []struct {
		name   string
		parts  []uploadPart
		status int
		detail string
	}{
		{"above the configured limit", []uploadPart{requestPart(t, base), {name: "native", filename: "Big.fmp12", body: syntheticNativeFile("12", 256)}},
			http.StatusRequestEntityTooLarge, "MIGRATION_MAX_UPLOAD_BYTES"},
		{"missing request part", []uploadPart{{name: "native", filename: "Synthetic.fmp12", body: file}},
			http.StatusBadRequest, "(the JSON migration request) is required"},
		{"unknown part", []uploadPart{requestPart(t, base), {name: "attachment", body: []byte("x")}},
			http.StatusBadRequest, "unexpected part"},
		{"duplicate native part", []uploadPart{requestPart(t, base), {name: "native", filename: "a.fmp12", body: file}, {name: "native", filename: "b.fmp12", body: file}},
			http.StatusBadRequest, "more than once"},
		{"empty native part", []uploadPart{requestPart(t, base), {name: "native", filename: "Empty.fmp12"}},
			http.StatusBadRequest, "is empty"},
		{"native part and base64", []uploadPart{
			requestPart(t, map[string]interface{}{"design_xml": nativeVisitsDesign, "fmp12_base64": base64.StdEncoding.EncodeToString(file)}),
			{name: "native", filename: "Synthetic.fmp12", body: file}},
			http.StatusBadRequest, "not both"},
		{"design part and design_xml", []uploadPart{requestPart(t, base), {name: "design", body: []byte(nativeVisitsDesign)}},
			http.StatusBadRequest, "not both"},
		{"invalid request JSON", []uploadPart{{name: "request", body: []byte("{")}},
			http.StatusBadRequest, "invalid JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.doMultipart("/api/v1/solutions/migrate/preflight", token, tc.parts, nil)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), tc.detail)
		})
	}
}

// An unreadable native part blocks apply before any write, exactly like the
// same copy sent as base64.
func TestAPI_MigrationMultipartNativeRejectionDoesNotApplySchema(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	database := uniqueDB("f4b_native_upload_reject")
	h.createDatabase(database, "qualifier", "synthetic-test-password")
	token := h.login(database, "qualifier", "synthetic-test-password")
	var before, after []struct {
		Name string `json:"name"`
	}
	rec := h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, &before)
	require.Equal(t, http.StatusOK, rec.Code)
	rec = h.doMultipart("/api/v1/solutions/migrate/apply", token, []uploadPart{
		requestPart(t, map[string]interface{}{"design_xml": nativeVisitsDesign}),
		{name: "native", filename: "Synthetic.fmp12", body: syntheticNativeFile("12-A", 0)},
	}, nil)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "Native Data Import Blocked")
	rec = h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, &after)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, before, after)
}

// Without a session and without public database creation, the upload is
// refused before its body is read.
func TestAPI_MigrationMultipartRequiresAuthentication(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: false})
	for _, path := range api.MigrationRoutes {
		rec := h.doMultipart(path, "", []uploadPart{
			requestPart(t, map[string]interface{}{"design_xml": nativeVisitsDesign}),
			{name: "native", filename: "Synthetic.fmp12", body: syntheticNativeFile("12", 0)},
		}, nil)
		require.Equal(t, http.StatusUnauthorized, rec.Code, path)
	}
}
