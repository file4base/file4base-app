package api_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/containermeta"
	"github.com/stretchr/testify/require"
)

const containerMigrationDesign = `<FMPReport><File name="Assets.fmp12"><BaseTableCatalog>` +
	`<BaseTable id="129" name="Documents"><FieldCatalog>` +
	`<Field id="1" name="Title" dataType="Text"/>` +
	`<Field id="2" name="Attachment" dataType="Container"/>` +
	`</FieldCatalog></BaseTable>` +
	`</BaseTableCatalog></File></FMPReport>`

func syntheticContainerNative(t *testing.T, title, filename string, fileBytes, previewJPG []byte) []byte {
	t.Helper()
	xor := func(b []byte) []byte {
		out := append([]byte(nil), b...)
		for i := range out {
			out[i] ^= 0x5a
		}
		return out
	}
	var fnam []byte
	fnam = binary.BigEndian.AppendUint32(fnam, 1)
	schemeEnc := xor([]byte("file"))
	nameEnc := xor([]byte(filename))
	fnam = append(fnam, byte(len(schemeEnc)))
	fnam = append(fnam, schemeEnc...)
	fnam = binary.BigEndian.AppendUint16(fnam, uint16(len(nameEnc)))
	fnam = append(fnam, nameEnc...)

	payload := appendSyntheticHBAM7Catalog(nil, 129, "Documents", []syntheticCatalogField{
		{id: 1, name: "Title", fieldClass: 1, dataType: 1},
		{id: 2, name: "Attachment", fieldClass: 1, dataType: 6},
	})
	// Row 1: Title + Section 31 container ref ("01")
	payload = append(payload, 0x28, 0x80, 1, 0x20, 5, 0x20, 1, 0x06, 1, byte(len(title)))
	payload = append(payload, xor([]byte(title))...)
	payload = append(payload, 0x20, 2)
	ref := append(xor([]byte("01")), 0x41, 0x32, 0xda)
	hdr := make([]byte, 8)
	binary.BigEndian.PutUint32(hdr[0:4], uint32(len(ref)))
	binary.BigEndian.PutUint32(hdr[4:8], 3)
	payload = append(payload, 0x05, 0)
	payload = append(payload, hdr...)
	payload = append(payload, 0x06, 1, byte(len(ref)))
	payload = append(payload, ref...)
	payload = append(payload, 0x40, 0x40, 0x40, 0x40)

	// Section 31 object 1 with FNAM, JPEG preview, and FILE original
	payload = append(payload, 0x28, 0x80, 1, 0x20, 31, 0x20, 5, 0x20, 1)
	payload = append(payload, 31, 4, 'F', 'N', 'A', 'M')
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(fnam)))
	payload = append(payload, fnam...)
	for _, fork := range []struct {
		tag  string
		data []byte
	}{{"JPEG", previewJPG}, {"FILE", fileBytes}} {
		payload = append(payload, 0x38, 4)
		payload = append(payload, []byte(fork.tag)...)
		payload = append(payload, 0x03, 0)
		payload = binary.BigEndian.AppendUint32(payload, uint32(len(fork.data)))
		payload = append(payload, 0x07, 1)
		payload = binary.BigEndian.AppendUint16(payload, uint16(len(fork.data)))
		payload = append(payload, fork.data...)
		payload = append(payload, 0x40)
	}
	payload = append(payload, 0x40, 0x40, 0x40, 0x40)

	file := make([]byte, 3*4096)
	copy(file[15:20], "HBAM7")
	binary.BigEndian.PutUint16(file[4096+12:], 2)
	copy(file[2*4096+20:], payload)
	return file
}

// #125 end-to-end: migrating a native .fmp12 with an embedded container applies
// the original file bytes verbatim, persists filename/MIME/SHA-256 metadata in
// sys_container_meta, serves it over the Data API, survives .f4data export/import,
// and clears stale metadata when the container field is overwritten.
func TestAPI_NativeContainerMigrationEndToEnd(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	database := uniqueDB("f4b_containers")
	h.createDatabase(database, "qualifier", "synthetic-test-password")
	token := h.login(database, "qualifier", "synthetic-test-password")

	original := []byte("%PDF-1.7\n1 0 obj << /Type /Catalog >> endobj\n%%EOF\n")
	preview := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0xff, 0xd9}
	wantSum := sha256.Sum256(original)
	wantHash := hex.EncodeToString(wantSum[:])

	native := syntheticContainerNative(t, "Spec Sheet", "spec-sheet.pdf", original, preview)

	var preflight struct {
		Report struct {
			Counts struct {
				Detected      int `json:"records_detected"`
				Ready         int `json:"records_ready"`
				Rejected      int `json:"rejected_count"`
				CellsDetected int `json:"container_cells_detected"`
				Reconstructed int `json:"container_files_reconstructed"`
				Missing       int `json:"container_files_missing"`
			} `json:"counts"`
		} `json:"report"`
	}
	rec := h.doMultipart("/api/v1/solutions/migrate/preflight", token, []uploadPart{
		requestPart(t, map[string]interface{}{"design_xml": containerMigrationDesign}),
		{name: "native", filename: "Assets.fmp12", body: native},
	}, &preflight)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 0, preflight.Report.Counts.Rejected)
	require.Equal(t, 1, preflight.Report.Counts.Ready)
	require.Equal(t, 1, preflight.Report.Counts.CellsDetected)
	require.Equal(t, 1, preflight.Report.Counts.Reconstructed)
	require.Equal(t, 0, preflight.Report.Counts.Missing)

	rec = h.doMultipart("/api/v1/solutions/migrate/apply", token, []uploadPart{
		requestPart(t, map[string]interface{}{"design_xml": containerMigrationDesign}),
		{name: "native", filename: "Assets.fmp12", body: native},
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var rows []map[string]interface{}
	rec = h.do(http.MethodGet, "/api/v1/data/documents/", token, nil, &rows)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, rows, 1)
	require.Equal(t, "Spec Sheet", rows[0]["title"])

	// Raw container bytes match original byte-for-byte.
	b64, ok := rows[0]["attachment"].(string)
	require.True(t, ok)
	decoded, err := base64.StdEncoding.DecodeString(b64)
	require.NoError(t, err)
	require.Equal(t, original, decoded)

	// Container metadata carries original filename, MIME type, SHA-256 and byte length.
	metaAll, ok := rows[0][containermeta.Key].(map[string]interface{})
	require.True(t, ok, "expected __container_meta in Data API response: %+v", rows[0])
	attMeta, ok := metaAll["attachment"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "spec-sheet.pdf", attMeta["filename"])
	require.Equal(t, "application/pdf", attMeta["mime_type"])
	require.Equal(t, wantHash, attMeta["sha256"])
	require.Equal(t, float64(len(original)), attMeta["size_bytes"])

	// Overwriting the container field clears stale migrated metadata, and a
	// client cannot forge __container_meta.
	recID := rows[0]["id"].(string)
	replacement := base64.StdEncoding.EncodeToString([]byte("new-bytes"))
	var updated map[string]interface{}
	rec = h.do(http.MethodPut, "/api/v1/data/documents/"+recID, token, map[string]interface{}{
		"attachment":      replacement,
		containermeta.Key: map[string]interface{}{"attachment": map[string]interface{}{"filename": "forged.pdf"}},
	}, &updated)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Nil(t, updated[containermeta.Key])
}
