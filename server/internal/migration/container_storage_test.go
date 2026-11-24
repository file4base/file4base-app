package migration

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The fixture in testdata/containers was created with FileMaker Pro 26.0.2:
// two records whose containers are embedded, in open external storage and in
// secure external storage. See testdata/containers/README.md.
const containerFixtureDir = "testdata/containers"

// SHA-256 of readme.txt, the file FileMaker wrote to open external storage.
const openStoragePayloadSHA256 = "a071de099929052cb11945537cb0aff2095ffe85529f712f5461a74bc6862540"

func gunzipFixture(t *testing.T, name string) []byte {
	t.Helper()
	f, err := os.Open(filepath.Join(containerFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func convertContainerFixture(t *testing.T, configure func(*MigrationRequest)) *ConvertedSolution {
	t.Helper()
	req := MigrationRequest{
		DesignXML:       string(gunzipFixture(t, "f4b-containers.xml.gz")),
		NativeDateOrder: "dmy",
		NativeFileName:  "f4b-containers.fmp12",
	}
	if configure != nil {
		configure(&req)
	}
	converted, err := AnalyzeAndConvertNative(req, gunzipFixture(t, "f4b-containers.fmp12.gz"))
	if err != nil {
		t.Fatal(err)
	}
	return converted
}

func rejectedDetails(c *ConvertedSolution) []string {
	var out []string
	for _, item := range c.Report.Rejected {
		out = append(out, item.Detail)
	}
	sort.Strings(out)
	return out
}

func countContaining(details []string, part string) int {
	n := 0
	for _, d := range details {
		if strings.Contains(d, part) {
			n++
		}
	}
	return n
}

// Row 2 holds a secure-storage container, so the import stays refused as a
// whole (no rows), as for any container that cannot be migrated. The counts
// and rejections show what happened to each cell.
func assertNoRows(t *testing.T, c *ConvertedSolution) {
	t.Helper()
	for table, rows := range c.TableRows {
		if len(rows) > 0 {
			t.Fatalf("%s returned %d rows although a container was refused", table, len(rows))
		}
	}
}

func TestContainerFixtureOpenStorageIsReadAndSecureStorageRefused(t *testing.T) {
	var asked []string
	dirResolver := NewDirectoryExternalContainerResolver(filepath.Join(containerFixtureDir, "f4b-containers"))
	var files [][]byte
	c := convertContainerFixture(t, func(r *MigrationRequest) {
		r.externalContainerResolver = func(ref string) ([]byte, bool, error) {
			asked = append(asked, ref)
			b, ok, err := dirResolver(ref)
			if ok {
				files = append(files, b)
			}
			return b, ok, err
		}
	})
	if len(asked) != 2 || asked[0] != "Table/external_file/readme.txt" || asked[1] != asked[0] {
		t.Fatalf("resolver asked for %q, want Table/external_file/readme.txt for each record", asked)
	}
	for _, b := range files {
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != openStoragePayloadSHA256 {
			t.Fatalf("open storage file sha256 = %s", got)
		}
	}
	counts := c.Report.Counts
	if counts.ContainerCellsDetected != 9 || counts.ContainerFilesReconstructed != 8 || counts.ContainerFilesMissing != 1 {
		t.Fatalf("counts detected=%d reconstructed=%d missing=%d, want 9/8/1",
			counts.ContainerCellsDetected, counts.ContainerFilesReconstructed, counts.ContainerFilesMissing)
	}
	rejected := rejectedDetails(c)
	if countContaining(rejected, "table.external_secure (row 2)") != 1 || countContaining(rejected, "secure external storage") != 1 {
		t.Fatalf("rejected = %q, want one secure-storage refusal for external_secure", rejected)
	}
	assertNoRows(t, c)
}

func TestContainerFixtureReportsOpenStorageWithoutFiles(t *testing.T) {
	c := convertContainerFixture(t, nil)
	counts := c.Report.Counts
	if counts.ContainerFilesReconstructed != 6 || counts.ContainerFilesMissing != 3 {
		t.Fatalf("counts reconstructed=%d missing=%d, want 6/3: the descriptor must not count as the file",
			counts.ContainerFilesReconstructed, counts.ContainerFilesMissing)
	}
	if n := countContaining(rejectedDetails(c), `"Table/external_file/readme.txt"`); n != 2 {
		t.Fatalf("missing open-storage reports = %d, want 2: %q", n, rejectedDetails(c))
	}
	assertNoRows(t, c)
}

func TestContainerFixtureRejectsChangedExternalFile(t *testing.T) {
	c := convertContainerFixture(t, func(r *MigrationRequest) {
		r.externalContainerResolver = func(ref string) ([]byte, bool, error) {
			return bytes.Repeat([]byte("x"), 46), true, nil
		}
	})
	if n := countContaining(rejectedDetails(c), "does not match the size and MD5"); n != 2 {
		t.Fatalf("size/MD5 mismatches = %d, want 2: %q", n, rejectedDetails(c))
	}
	if got := c.Report.Counts.ContainerFilesReconstructed; got != 6 {
		t.Fatalf("reconstructed = %d, want 6", got)
	}
}

func TestParseExternalStorageFile(t *testing.T) {
	raw := append([]byte{46, 0, 0, 0, 0, 0, 0, 0}, bytes.Repeat([]byte{0xab}, 16)...)
	raw = append(raw, []byte("readme.txt\x00")...)
	got, ok := parseExternalStorageFile(raw)
	if !ok || got.size != 46 || got.name != "readme.txt" || len(got.md5) != 16 {
		t.Fatalf("parse = %+v, %v", got, ok)
	}
	for _, bad := range [][]byte{nil, raw[:24], append(raw[:24:24], 0)} {
		if _, ok := parseExternalStorageFile(bad); ok {
			t.Errorf("parse(%q) accepted an incomplete descriptor", bad)
		}
	}
}
