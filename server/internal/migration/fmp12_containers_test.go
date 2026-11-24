package migration

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/containermeta"
)

func encodeHBAM7FNAM(scheme, pathStr string) []byte {
	var out []byte
	out = binary.BigEndian.AppendUint32(out, 1)
	sEnc := nativeEncoded([]byte(scheme))
	pEnc := nativeEncoded([]byte(pathStr))
	out = append(out, byte(len(sEnc)))
	out = append(out, sEnc...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(pEnc)))
	out = append(out, pEnc...)
	return out
}

func appendLongKeyChunk(payload []byte, tag string, data []byte) []byte {
	// code 31: 1-byte longKey length, longKey bytes, 2-byte data length, data bytes
	payload = append(payload, 31, byte(len(tag)))
	payload = append(payload, []byte(tag)...)
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(data)))
	payload = append(payload, data...)
	return payload
}

func appendSection31Object(payload []byte, tableID int, objID byte, fnam []byte, forks map[string][]byte, overrideLen map[string]uint32) []byte {
	tHi := byte(0x80 | ((tableID - 128) >> 8))
	tLo := byte((tableID - 128) & 0xff)
	// Push [tableID].[31].[5].[objID]
	payload = append(payload, 0x28, tHi, tLo, 0x20, 31, 0x20, 5, 0x20, objID)
	if len(fnam) > 0 {
		payload = appendLongKeyChunk(payload, "FNAM", fnam)
	}
	for tag, content := range forks {
		// Push 4-byte fork tag
		payload = append(payload, 0x38, 4)
		payload = append(payload, []byte(tag)...)
		// key=0: 4-byte expected length (code 3 = 1-byte key + 4-byte data)
		declared := uint32(len(content))
		if v, ok := overrideLen[tag]; ok {
			declared = v
		}
		payload = append(payload, 0x03, 0)
		payload = binary.BigEndian.AppendUint32(payload, declared)
		// Split payload into <= 16-byte segments (code 7 = 1-byte key + 2-byte len)
		segIdx := byte(1)
		for off := 0; off < len(content); off += 16 {
			end := off + 16
			if end > len(content) {
				end = len(content)
			}
			chunk := content[off:end]
			payload = append(payload, 0x07, segIdx)
			payload = binary.BigEndian.AppendUint16(payload, uint16(len(chunk)))
			payload = append(payload, chunk...)
			segIdx++
		}
		payload = append(payload, 0x40) // pop tag
	}
	payload = append(payload, 0x40, 0x40, 0x40, 0x40)
	return payload
}

func appendSection5ContainerRef(payload []byte, tableID int, rowID, fieldID byte, objDigits string) []byte {
	tHi := byte(0x80 | ((tableID - 128) >> 8))
	tLo := byte((tableID - 128) & 0xff)
	ref := append(nativeEncoded([]byte(objDigits)), 0x41, 0x32, 0xda)
	hdr := make([]byte, 8)
	binary.BigEndian.PutUint32(hdr[0:4], uint32(len(ref)))
	binary.BigEndian.PutUint32(hdr[4:8], 3)
	payload = append(payload, 0x28, tHi, tLo, 0x20, 5, 0x20, rowID, 0x20, fieldID)
	// key=0: 8-byte header (code 5 = 1-byte key + 8-byte data)
	payload = append(payload, 0x05, 0)
	payload = append(payload, hdr...)
	// key=1: reference payload (code 6 = 1-byte key + 1-byte len)
	payload = append(payload, 0x06, 1, byte(len(ref)))
	payload = append(payload, ref...)
	payload = append(payload, 0x40, 0x40, 0x40, 0x40)
	return payload
}

const containerDesign = `<FMPReport><File name="Assets.fmp12"><BaseTableCatalog>` +
	`<BaseTable id="129" name="Documents"><FieldCatalog>` +
	`<Field id="1" name="Title" dataType="Text"/>` +
	`<Field id="2" name="Attachment" dataType="Container"/>` +
	`</FieldCatalog></BaseTable>` +
	`</BaseTableCatalog></File></FMPReport>`

// #125: Embedded container with BOTH original FILE fork and JPEG/PICT preview
// thumbnails must choose the original FILE fork byte-for-byte, keep filename +
// MIME type, and verify its SHA-256 hash.
func TestNativeContainerPrefersOriginalFileOverPreviews(t *testing.T) {
	originalPDF := []byte("%PDF-1.7\n%Original binary document payload with \x00\xff bytes\n%%EOF\n")
	previewJPG := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0xff, 0xd9}
	previewPICT := []byte{0x00, 0x11, 0x02, 0xff, 0x0c, 0x00}
	wantHash := sha256.Sum256(originalPDF)

	payload := appendHBAM7TableCatalog(nil, 129, "Documents", []testHBAM7FieldSpec{
		{id: 1, name: "Title", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
		{id: 2, name: "Attachment", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeContainer},
	})
	payload = appendHBAM7SingleRow(payload, 129, 1, map[byte]string{1: "Invoice Q4"})
	payload = appendSection5ContainerRef(payload, 129, 1, 2, "01")
	payload = appendSection31Object(payload, 129, 1,
		encodeHBAM7FNAM("file", "invoice-2026.pdf"),
		map[string][]byte{
			"JPEG": previewJPG,
			"PICT": previewPICT,
			"FILE": originalPDF,
		}, nil)

	native := buildSyntheticHBAM7(payload)
	converted, err := AnalyzeAndConvertNative(MigrationRequest{DesignXML: containerDesign}, native)
	if err != nil {
		t.Fatal(err)
	}
	counts := converted.Report.Counts
	if counts.RejectedCount != 0 || counts.RecordsReady != 1 {
		t.Fatalf("unexpected counts: %+v, rejected: %+v", counts, converted.Report.Rejected)
	}
	if counts.ContainerCellsDetected != 1 || counts.ContainerFilesReconstructed != 1 || counts.ContainerFilesMissing != 0 {
		t.Fatalf("unexpected container counts: %+v", counts)
	}
	row := converted.TableRows["documents"][0]
	gotBytes, ok := row["attachment"].([]byte)
	if !ok || !bytes.Equal(gotBytes, originalPDF) {
		t.Fatalf("reconstructed bytes did not match original FILE fork: got %x want %x", gotBytes, originalPDF)
	}
	metaMap, _ := row[containermeta.Key].(map[string]containermeta.Entry)
	meta := metaMap["attachment"]
	if meta.Filename != "invoice-2026.pdf" || meta.MimeType != "application/pdf" || meta.SHA256 != hex.EncodeToString(wantHash[:]) || meta.SizeBytes != int64(len(originalPDF)) {
		t.Fatalf("unexpected container metadata: %+v", meta)
	}
}

// #125: Inline forks directly under [table].[5].[row].[fid] are reconstructed
// and truncated forks (declared length != assembled length) are refused.
func TestNativeContainerInlineForkAndTruncationGuard(t *testing.T) {
	pngBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 13, 'I', 'H', 'D', 'R'}
	wantHash := sha256.Sum256(pngBytes)

	payload := appendHBAM7TableCatalog(nil, 129, "Documents", []testHBAM7FieldSpec{
		{id: 1, name: "Title", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
		{id: 2, name: "Attachment", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeContainer},
	})
	payload = appendHBAM7SingleRow(payload, 129, 1, map[byte]string{1: "Logo"})
	// Inline container under [129].[5].[1].[2]
	payload = append(payload, 0x28, 0x80, 1, 0x20, 5, 0x20, 1, 0x20, 2)
	payload = appendLongKeyChunk(payload, "FNAM", encodeHBAM7FNAM("image", "logo.png"))
	payload = appendLongKeyChunk(payload, "PICT", []byte{1, 2, 3})
	payload = appendLongKeyChunk(payload, "PNGf", pngBytes)
	payload = append(payload, 0x40, 0x40, 0x40, 0x40)

	converted, err := AnalyzeAndConvertNative(MigrationRequest{DesignXML: containerDesign}, buildSyntheticHBAM7(payload))
	if err != nil {
		t.Fatal(err)
	}
	if converted.Report.Counts.RejectedCount != 0 || converted.Report.Counts.ContainerFilesReconstructed != 1 {
		t.Fatalf("inline container failed: %+v", converted.Report)
	}
	row := converted.TableRows["documents"][0]
	if !bytes.Equal(row["attachment"].([]byte), pngBytes) {
		t.Fatal("inline PNGf bytes did not match")
	}
	meta := row[containermeta.Key].(map[string]containermeta.Entry)["attachment"]
	if meta.Filename != "logo.png" || meta.MimeType != "image/png" || meta.SHA256 != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("unexpected inline metadata: %+v", meta)
	}

	// Now build a truncated Section 31 fork where declared length is 999 bytes.
	truncPayload := appendHBAM7TableCatalog(nil, 129, "Documents", []testHBAM7FieldSpec{
		{id: 1, name: "Title", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
		{id: 2, name: "Attachment", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeContainer},
	})
	truncPayload = appendHBAM7SingleRow(truncPayload, 129, 1, map[byte]string{1: "Truncated"})
	truncPayload = appendSection5ContainerRef(truncPayload, 129, 1, 2, "01")
	truncPayload = appendSection31Object(truncPayload, 129, 1,
		encodeHBAM7FNAM("image", "cut.jpg"),
		map[string][]byte{"JPEG": []byte{0xff, 0xd8, 0xff, 0xd9}},
		map[string]uint32{"JPEG": 999})
	truncConv, err := AnalyzeAndConvertNative(MigrationRequest{DesignXML: containerDesign}, buildSyntheticHBAM7(truncPayload))
	if err != nil {
		t.Fatal(err)
	}
	if truncConv.Report.Counts.RejectedCount == 0 || truncConv.Report.Counts.RecordsReady != 0 || truncConv.Report.Counts.ContainerFilesMissing != 1 {
		t.Fatalf("truncated container was not rejected: %+v", truncConv.Report.Counts)
	}
}

// #125: External container storage references resolve when backing files are
// supplied (via ExternalFiles or a local directory) and are explicitly reported
// as missing when absent.
func TestNativeContainerExternalStorageResolutionAndMissingReport(t *testing.T) {
	extContent := []byte("external-contract-bytes-2026")
	wantHash := sha256.Sum256(extContent)

	payload := appendHBAM7TableCatalog(nil, 129, "Documents", []testHBAM7FieldSpec{
		{id: 1, name: "Title", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
		{id: 2, name: "Attachment", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeContainer},
	})
	payload = appendHBAM7SingleRow(payload, 129, 1, map[byte]string{1: "Contract"})
	// External reference stored under [129].[5].[1].[2] via FNAM with no embedded payload fork
	payload = append(payload, 0x28, 0x80, 1, 0x20, 5, 0x20, 1, 0x20, 2)
	payload = appendLongKeyChunk(payload, "FNAM", encodeHBAM7FNAM("remote", "Files/Assets/contracts/signed.txt"))
	payload = append(payload, 0x40, 0x40, 0x40, 0x40)
	native := buildSyntheticHBAM7(payload)

	// 1. Without external files: explicitly reported missing.
	missingConv, err := AnalyzeAndConvertNative(MigrationRequest{DesignXML: containerDesign}, native)
	if err != nil {
		t.Fatal(err)
	}
	if missingConv.Report.Counts.ContainerCellsDetected != 1 || missingConv.Report.Counts.ContainerFilesReconstructed != 0 || missingConv.Report.Counts.ContainerFilesMissing != 1 {
		t.Fatalf("expected 1 missing external container, got counts: %+v", missingConv.Report.Counts)
	}
	if missingConv.Report.Counts.RejectedCount == 0 || missingConv.Report.Counts.RecordsReady != 0 {
		t.Fatalf("missing external container must reject import: %+v", missingConv.Report)
	}
	foundMissingMsg := false
	for _, rej := range missingConv.Report.Rejected {
		if strings.Contains(rej.Detail, "remote:Files/Assets/contracts/signed.txt") && strings.Contains(rej.Detail, "missing") {
			foundMissingMsg = true
		}
	}
	if !foundMissingMsg {
		t.Fatalf("missing external file path not reported: %+v", missingConv.Report.Rejected)
	}

	// 2. With ExternalFiles map (HTTP API path): reconstructed byte-for-byte.
	mapConv, err := AnalyzeAndConvertNative(MigrationRequest{
		DesignXML: containerDesign,
		ExternalFiles: map[string]string{
			"Files/Assets/contracts/signed.txt": base64.StdEncoding.EncodeToString(extContent),
		},
	}, native)
	if err != nil {
		t.Fatal(err)
	}
	if mapConv.Report.Counts.RejectedCount != 0 || mapConv.Report.Counts.ContainerFilesReconstructed != 1 || mapConv.Report.Counts.ContainerFilesMissing != 0 {
		t.Fatalf("external map resolution failed: %+v", mapConv.Report)
	}
	row := mapConv.TableRows["documents"][0]
	if !bytes.Equal(row["attachment"].([]byte), extContent) {
		t.Fatal("external container bytes mismatch")
	}
	meta := row[containermeta.Key].(map[string]containermeta.Entry)["attachment"]
	if meta.Filename != "signed.txt" || meta.SHA256 != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("unexpected external metadata: %+v", meta)
	}

	// 3. With local directory resolver (CLI path) + traversal safety check.
	dir := t.TempDir()
	full := filepath.Join(dir, "Files", "Assets", "contracts", "signed.txt")
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, extContent, 0600); err != nil {
		t.Fatal(err)
	}
	req := MigrationRequest{DesignXML: containerDesign}
	req.SetExternalContainerDir(dir)
	dirConv, err := AnalyzeAndConvertNative(req, native)
	if err != nil {
		t.Fatal(err)
	}
	if dirConv.Report.Counts.RejectedCount != 0 || dirConv.Report.Counts.ContainerFilesReconstructed != 1 {
		t.Fatalf("directory external resolver failed: %+v", dirConv.Report)
	}

	// Path traversal outside rootDir must never resolve.
	resolver := NewDirectoryExternalContainerResolver(dir)
	if _, found, err := resolver("filemac:/../../etc/passwd"); err != nil || found {
		t.Fatalf("path traversal should never resolve: found=%v err=%v", found, err)
	}
}

func convertSingleSection31Container(t *testing.T, fnam []byte, forks map[string][]byte, declared map[string]uint32) *ConvertedSolution {
	t.Helper()
	payload := appendHBAM7TableCatalog(nil, 129, "Documents", []testHBAM7FieldSpec{
		{id: 1, name: "Title", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
		{id: 2, name: "Attachment", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeContainer},
	})
	payload = appendHBAM7SingleRow(payload, 129, 1, map[byte]string{1: "Logo"})
	payload = appendSection5ContainerRef(payload, 129, 1, 2, "01")
	payload = appendSection31Object(payload, 129, 1, fnam, forks, declared)
	converted, err := AnalyzeAndConvertNative(MigrationRequest{DesignXML: containerDesign}, buildSyntheticHBAM7(payload))
	if err != nil {
		t.Fatal(err)
	}
	return converted
}

// A production file held forks whose segments ran past the declared length:
// the declared bytes ended exactly on a segment boundary with the image's own
// end marker, and the extra segments were left over from a longer version.
func TestNativeContainerIgnoresStaleSegmentsPastDeclaredLength(t *testing.T) {
	image := append(append([]byte{0xff, 0xd8, 0xff, 0xe0}, bytes.Repeat([]byte{0x11}, 26)...), 0xff, 0xd9) // 32 bytes: two segments
	stale := bytes.Repeat([]byte{0x51, 0x45, 0x14, 0x00}, 5)                                               // 20 bytes: two more
	c := convertSingleSection31Container(t, encodeHBAM7FNAM("image", "logo.jpg"),
		map[string][]byte{"JPEG": append(append([]byte{}, image...), stale...)},
		map[string]uint32{"JPEG": uint32(len(image))})
	if c.Report.Counts.RejectedCount != 0 || c.Report.Counts.ContainerFilesReconstructed != 1 {
		t.Fatalf("stale tail refused: %+v %+v", c.Report.Counts, c.Report.Rejected)
	}
	if got := c.TableRows["documents"][0]["attachment"].([]byte); !bytes.Equal(got, image) {
		t.Fatalf("got %x, want the declared %d bytes", got, len(image))
	}
}

func TestNativeContainerRefusesDeclaredLengthInsideASegment(t *testing.T) {
	content := bytes.Repeat([]byte{0xab}, 48) // three 16-byte segments
	c := convertSingleSection31Container(t, encodeHBAM7FNAM("image", "logo.jpg"),
		map[string][]byte{"JPEG": content}, map[string]uint32{"JPEG": 40})
	if c.Report.Counts.ContainerFilesReconstructed != 0 || c.Report.Counts.ContainerFilesMissing != 1 {
		t.Fatalf("a length that is not a segment boundary was accepted: %+v", c.Report.Counts)
	}
}

func TestNativeContainerPrefersEPSOverItsPreviews(t *testing.T) {
	eps := []byte("%!PS-Adobe-3.0 EPSF-3.0\n%%BoundingBox: 0 0 10 10\nshowpage\n%%EOF\n")
	c := convertSingleSection31Container(t, nil, map[string][]byte{
		"EPS ": eps,
		"JPEG": {0xff, 0xd8, 0xff, 0xe0, 0xff, 0xd9},
		"PICT": {0x00, 0x11, 0x02, 0xff},
	}, nil)
	row := c.TableRows["documents"][0]
	if got, _ := row["attachment"].([]byte); !bytes.Equal(got, eps) {
		t.Fatalf("got %q, want the EPS original", got)
	}
	meta := row[containermeta.Key].(map[string]containermeta.Entry)["attachment"]
	if meta.MimeType != "application/postscript" || meta.Filename != "container.eps" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}
