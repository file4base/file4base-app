package migration

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/file4base/file4base-app/server/internal/containermeta"
)

// containerStats tracks container extraction counts across a native .fmp12 read (#125).
type containerStats struct {
	CellsDetected      int
	FilesReconstructed int
	FilesMissing       int
	Missing            []missingContainerFile
	ReconstructedByTbl map[string]int
}

type missingContainerFile struct {
	Table   string
	Column  string
	RowNum  int
	RefPath string
	Reason  string
}

type hbam7ContainerForks struct {
	fnamRaw      []byte
	hash16       []byte
	inlineForks  map[string][]byte
	forkSegments map[string]map[int][]byte
	expectedLen  map[string]uint32
}

func newHBAM7ContainerForks() *hbam7ContainerForks {
	return &hbam7ContainerForks{
		inlineForks:  map[string][]byte{},
		forkSegments: map[string]map[int][]byte{},
		expectedLen:  map[string]uint32{},
	}
}

func (f *hbam7ContainerForks) recordDirectory(data []byte) {
	if len(data) < 2 {
		return
	}
	count := int(binary.BigEndian.Uint16(data[0:2]))
	pos := 2
	for i := 0; i < count && pos+8 <= len(data); i++ {
		tag := string(data[pos : pos+4])
		size := binary.BigEndian.Uint32(data[pos+4 : pos+8])
		pos += 8
		if size > 0 {
			f.expectedLen[tag] = size
		}
	}
}

func (f *hbam7ContainerForks) addInlineFork(tag string, data []byte) {
	if tag == "FNAM" {
		f.fnamRaw = append([]byte(nil), data...)
		return
	}
	f.inlineForks[tag] = append([]byte(nil), data...)
}

func (f *hbam7ContainerForks) addForkSegment(tag string, key int, data []byte) {
	if key == 0 && len(data) == 4 {
		f.expectedLen[tag] = binary.BigEndian.Uint32(data)
		return
	}
	if key < 1 {
		return
	}
	if f.forkSegments[tag] == nil {
		f.forkSegments[tag] = map[int][]byte{}
	}
	f.forkSegments[tag][key] = append(f.forkSegments[tag][key], data...)
}

func (f *hbam7ContainerForks) forkData(tag string) ([]byte, uint32, bool) {
	if segs := f.forkSegments[tag]; len(segs) > 0 {
		keys := make([]int, 0, len(segs))
		total := 0
		for k, b := range segs {
			keys = append(keys, k)
			total += len(b)
		}
		sort.Ints(keys)
		out := make([]byte, 0, total)
		for _, k := range keys {
			out = append(out, segs[k]...)
		}
		return out, f.expectedLen[tag], true
	}
	if b, ok := f.inlineForks[tag]; ok {
		return b, f.expectedLen[tag], true
	}
	return nil, 0, false
}

func (f *hbam7ContainerForks) payloadTags() []string {
	seen := map[string]bool{}
	for tag := range f.inlineForks {
		if !isMetadataForkTag(tag) {
			seen[tag] = true
		}
	}
	for tag := range f.forkSegments {
		if !isMetadataForkTag(tag) {
			seen[tag] = true
		}
	}
	out := make([]string, 0, len(seen))
	for tag := range seen {
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func isMetadataForkTag(tag string) bool {
	switch tag {
	case "FNAM", "SIZE", "DPI_", "META", "TEXT", "SIZ2",
		// External storage descriptors: they locate a file, they are not one.
		"REMO", "ENCR", "file":
		return true
	}
	return false
}

type hbam7ContainerCell struct {
	refHeader  []byte
	refPayload []byte
	forks      *hbam7ContainerForks
	hasData    bool
}

type hbam7ContainerStore struct {
	// Section 31 objects: tableID -> objID -> forks
	objects map[int]map[int]*hbam7ContainerForks
}

func newHBAM7ContainerStore() *hbam7ContainerStore {
	return &hbam7ContainerStore{
		objects: map[int]map[int]*hbam7ContainerForks{},
	}
}

func (s *hbam7ContainerStore) observeSection31(c hbamChunk) bool {
	if len(c.path) < 4 {
		return false
	}
	tableID := hbamPathInt(c.path[0])
	section := hbamPathInt(c.path[1])
	sub := hbamPathInt(c.path[2])
	if tableID < 129 || section != 31 || sub != 5 {
		return false
	}
	objID := hbamPathInt(c.path[3])
	if objID < 1 {
		return true
	}
	if s.objects[tableID] == nil {
		s.objects[tableID] = map[int]*hbam7ContainerForks{}
	}
	obj := s.objects[tableID][objID]
	if obj == nil {
		obj = newHBAM7ContainerForks()
		s.objects[tableID][objID] = obj
	}
	if len(c.path) == 4 {
		switch {
		case c.kind == 'v' && c.key == 2:
			obj.recordDirectory(c.data)
		case c.kind == 'v' && c.key == 4 && len(c.data) == 16:
			obj.hash16 = append([]byte(nil), c.data...)
		case c.kind == 'l' && len(c.longKey) == 4:
			obj.addInlineFork(string(c.longKey), c.data)
		}
		return true
	}
	if len(c.path) == 5 && len(c.path[4]) == 4 {
		tag := string(c.path[4])
		if c.kind == 'v' || c.kind == 'g' {
			obj.addForkSegment(tag, c.key, c.data)
		}
	}
	return true
}

func (c *hbam7ContainerCell) observeRecordChunk(ch hbamChunk) {
	if c.forks == nil {
		c.forks = newHBAM7ContainerForks()
	}
	if len(ch.path) == 4 {
		switch {
		case ch.kind == 'l' && len(ch.longKey) == 4:
			c.hasData = true
			c.forks.addInlineFork(string(ch.longKey), ch.data)
		case (ch.kind == 'v' || ch.kind == 'g') && ch.key == 0:
			c.hasData = true
			c.refHeader = append(c.refHeader, ch.data...)
		case (ch.kind == 'v' || ch.kind == 'g') && ch.key == 1:
			c.hasData = true
			c.refPayload = append(c.refPayload, ch.data...)
		case (ch.kind == 'v' || ch.kind == 'g') && ch.key == 2:
			c.hasData = true
			c.forks.recordDirectory(ch.data)
		case (ch.kind == 'v' || ch.kind == 'g') && ch.key == 4 && len(ch.data) == 16:
			c.hasData = true
			c.forks.hash16 = append([]byte(nil), ch.data...)
		}
		return
	}
	if len(ch.path) == 5 && len(ch.path[4]) == 4 && (ch.kind == 'v' || ch.kind == 'g') {
		c.hasData = true
		c.forks.addForkSegment(string(ch.path[4]), ch.key, ch.data)
	}
}

type reconstructedContainer struct {
	Bytes []byte
	Meta  containermeta.Entry
}

type fnamEntry struct {
	scheme string
	path   string
}

// parseHBAM7FNAM parses FileMaker's FNAM descriptor:
//
//	[uint32 entryCount] followed by entryCount records of:
//	  [uint8 schemeLen][XOR-0x5a/SCSU scheme][uint16 pathLen][XOR-0x5a/SCSU path]
//
// It also falls back to a plain XOR-0x5a/SCSU "scheme:path" string when the
// binary header is absent.
func parseHBAM7FNAM(raw []byte) []fnamEntry {
	if len(raw) == 0 {
		return nil
	}
	if len(raw) >= 7 {
		count := int(binary.BigEndian.Uint32(raw[0:4]))
		if count >= 1 && count <= 16 {
			pos := 4
			var entries []fnamEntry
			ok := true
			for i := 0; i < count; i++ {
				if pos >= len(raw) {
					ok = false
					break
				}
				sLen := int(raw[pos])
				pos++
				if pos+sLen+2 > len(raw) {
					ok = false
					break
				}
				scheme, err := decodeHBAM7Text(raw[pos : pos+sLen])
				if err != nil {
					ok = false
					break
				}
				pos += sLen
				pLen := int(binary.BigEndian.Uint16(raw[pos : pos+2]))
				pos += 2
				if pos+pLen > len(raw) {
					ok = false
					break
				}
				pStr, err := decodeHBAM7Text(raw[pos : pos+pLen])
				if err != nil {
					ok = false
					break
				}
				pos += pLen
				entries = append(entries, fnamEntry{scheme: strings.ToLower(strings.TrimSpace(scheme)), path: pStr})
			}
			if ok && len(entries) > 0 {
				return entries
			}
		}
	}
	if text, err := decodeHBAM7Text(raw); err == nil && strings.TrimSpace(text) != "" {
		return parseTextContainerRefs(text)
	}
	return nil
}

func parseTextContainerRefs(text string) []fnamEntry {
	var out []fnamEntry
	for _, line := range strings.FieldsFunc(text, func(r rune) bool { return r == '\r' || r == '\n' }) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.IndexByte(line, ':'); idx > 0 && idx < 16 {
			scheme := strings.ToLower(line[:idx])
			if isKnownContainerScheme(scheme) {
				out = append(out, fnamEntry{scheme: scheme, path: line[idx+1:]})
				continue
			}
		}
		out = append(out, fnamEntry{path: line})
	}
	return out
}

func isKnownContainerScheme(scheme string) bool {
	switch scheme {
	case "file", "image", "movie", "remote", "filemac", "filewin", "imagemac", "imagewin", "moviemac", "moviewin", "size":
		return true
	}
	return false
}

func isExternalContainerScheme(scheme string) bool {
	switch scheme {
	case "remote", "filemac", "filewin", "imagemac", "imagewin", "moviemac", "moviewin":
		return true
	}
	return false
}

// resolveSection31ObjectID resolves the internal Section 31 object ID encoded
// in a record container cell's key=1 payload (observed in HBAM7 as leading
// XOR-0x5a ASCII digits such as "01", "02", "03").
func resolveSection31ObjectID(refPayload []byte, tableObjects map[int]*hbam7ContainerForks) (int, bool) {
	if len(refPayload) == 0 || len(tableObjects) == 0 {
		return 0, false
	}
	xored := make([]byte, len(refPayload))
	for i, b := range refPayload {
		xored[i] = b ^ 0x5a
	}
	// Extract leading decimal digits.
	end := 0
	for end < len(xored) && xored[end] >= '0' && xored[end] <= '9' {
		end++
	}
	if end > 0 {
		if id, err := strconv.Atoi(string(xored[:end])); err == nil && tableObjects[id] != nil {
			return id, true
		}
	}
	// Fallback: leading hex digits (for object IDs encoded in hex).
	hexEnd := 0
	for hexEnd < len(xored) && ((xored[hexEnd] >= '0' && xored[hexEnd] <= '9') ||
		(xored[hexEnd] >= 'a' && xored[hexEnd] <= 'f') ||
		(xored[hexEnd] >= 'A' && xored[hexEnd] <= 'F')) {
		hexEnd++
	}
	if hexEnd > 0 {
		if id, err := strconv.ParseInt(string(xored[:hexEnd]), 16, 64); err == nil && tableObjects[int(id)] != nil {
			return int(id), true
		}
	}
	// Fallback: 4-byte big-endian uint32 object ID.
	if len(refPayload) >= 4 {
		id := int(binary.BigEndian.Uint32(refPayload[:4]))
		if tableObjects[id] != nil {
			return id, true
		}
	}
	return 0, false
}

// reconstructContainerCell resolves one populated HBAM7 container cell into
// raw file bytes + verified metadata (filename, MIME type, SHA-256, size).
// For external container references whose backing file cannot be located, it
// returns (nil, externalRefPath, nil) so the caller can report the missing file.
func reconstructContainerCell(
	tableID int,
	cell *hbam7ContainerCell,
	store *hbam7ContainerStore,
	resolver externalContainerResolver,
) (*reconstructedContainer, string, error) {
	if cell == nil || !cell.hasData {
		return nil, "", nil
	}

	// 1. Check whether the cell points to an embedded Section 31 object.
	var sourceForks *hbam7ContainerForks
	if tableObjs := store.objects[tableID]; len(tableObjs) > 0 && len(cell.refPayload) > 0 {
		if objID, ok := resolveSection31ObjectID(cell.refPayload, tableObjs); ok {
			sourceForks = tableObjs[objID]
		}
	}
	// 2. Otherwise check inline forks directly stored in the record cell.
	if sourceForks == nil && cell.forks != nil && (len(cell.forks.payloadTags()) > 0 || len(cell.forks.fnamRaw) > 0) {
		sourceForks = cell.forks
	}

	var fnamEntries []fnamEntry
	if sourceForks != nil && len(sourceForks.fnamRaw) > 0 {
		fnamEntries = parseHBAM7FNAM(sourceForks.fnamRaw)
	}
	if len(fnamEntries) == 0 && len(cell.refPayload) > 0 {
		// Check whether key=1 itself holds an external path reference.
		if txt, err := decodeHBAM7Text(cell.refPayload); err == nil {
			if parsed := parseTextContainerRefs(txt); len(parsed) > 0 && (isExternalContainerScheme(parsed[0].scheme) || strings.ContainsAny(parsed[0].path, `/\`)) {
				fnamEntries = parsed
			}
		}
	}

	filename := extractContainerFilename(fnamEntries)

	// 3. A field stored externally by FileMaker keeps only a descriptor here.
	if sourceForks != nil {
		if out, missing, handled, err := externalStorageContainer(sourceForks, filename, resolver); handled {
			return out, missing, err
		}
	}

	// 4. Reconstruct embedded payload forks when present (choosing original over previews).
	if sourceForks != nil {
		if tag, ok := selectOriginalForkTag(sourceForks.payloadTags(), filename); ok {
			payload, expectedLen, _ := sourceForks.forkData(tag)
			if expectedLen > 0 && uint32(len(payload)) != expectedLen {
				return nil, "", fmt.Errorf("container fork %q truncated: expected %d bytes, got %d", tag, expectedLen, len(payload))
			}
			if len(payload) == 0 {
				return nil, "", fmt.Errorf("container fork %q is empty", tag)
			}
			mimeType := detectContainerMIME(filename, tag, payload)
			if filename == "" {
				filename = defaultContainerFilename(tag, mimeType)
			}
			sum := sha256.Sum256(payload)
			return &reconstructedContainer{
				Bytes: payload,
				Meta: containermeta.Entry{
					Filename:  filename,
					MimeType:  mimeType,
					SHA256:    hex.EncodeToString(sum[:]),
					SizeBytes: int64(len(payload)),
				},
			}, "", nil
		}
	}

	// 5. External storage reference: look up referenced file or report it missing.
	if extRef := extractExternalContainerRef(fnamEntries); extRef != "" {
		if resolver != nil {
			if fileBytes, found, err := resolver(extRef); err != nil {
				return nil, "", err
			} else if found {
				if filename == "" {
					filename = path.Base(strings.ReplaceAll(extRef, `\`, "/"))
				}
				mimeType := detectContainerMIME(filename, "", fileBytes)
				sum := sha256.Sum256(fileBytes)
				return &reconstructedContainer{
					Bytes: fileBytes,
					Meta: containermeta.Entry{
						Filename:  filename,
						MimeType:  mimeType,
						SHA256:    hex.EncodeToString(sum[:]),
						SizeBytes: int64(len(fileBytes)),
					},
				}, "", nil
			}
		}
		return nil, extRef, nil
	}

	return nil, "", fmt.Errorf("container cell has no reconstructable payload fork or external reference")
}

// selectOriginalForkTag picks the authoritative original file fork over
// FileMaker-generated preview/thumbnail forks (such as JPEG/PNGf/PICT previews
// generated alongside FILE or PDF ).
func selectOriginalForkTag(tags []string, filename string) (string, bool) {
	if len(tags) == 0 {
		return "", false
	}
	has := make(map[string]bool, len(tags))
	for _, t := range tags {
		has[t] = true
	}
	// 1. Original file / document forks always beat generated previews.
	for _, primary := range []string{"FILE", "PDF ", "FORK"} {
		if has[primary] {
			return primary, true
		}
	}
	// 2. Match the original filename's extension when available.
	if extTag := forkTagForFilename(filename); extTag != "" && has[extTag] {
		return extTag, true
	}
	// 3. Standard media forks, with legacy PICT thumbnail last.
	for _, fallback := range []string{"PNGf", "JPEG", "GIFf", "TIFF", "BMPf", "SVG ", "MOOV", "MP4 ", "MP3 ", "WAV ", "SND ", "ZIP ", "PICT"} {
		if has[fallback] {
			return fallback, true
		}
	}
	return tags[0], true
}

func forkTagForFilename(filename string) string {
	switch strings.ToLower(path.Ext(filename)) {
	case ".pdf":
		return "PDF "
	case ".png":
		return "PNGf"
	case ".jpg", ".jpeg":
		return "JPEG"
	case ".gif":
		return "GIFf"
	case ".tif", ".tiff":
		return "TIFF"
	case ".bmp":
		return "BMPf"
	case ".svg":
		return "SVG "
	case ".mov":
		return "MOOV"
	case ".mp4":
		return "MP4 "
	case ".mp3":
		return "MP3 "
	case ".wav":
		return "WAV "
	case ".zip":
		return "ZIP "
	case ".pct", ".pict":
		return "PICT"
	}
	return ""
}

func extractContainerFilename(entries []fnamEntry) string {
	for _, e := range entries {
		if e.scheme == "size" {
			continue
		}
		cleaned := strings.ReplaceAll(strings.TrimSpace(e.path), `\`, "/")
		if cleaned == "" {
			continue
		}
		base := path.Base(cleaned)
		if base != "" && base != "." && base != "/" {
			return base
		}
	}
	return ""
}

func extractExternalContainerRef(entries []fnamEntry) string {
	for _, e := range entries {
		if isExternalContainerScheme(e.scheme) && strings.TrimSpace(e.path) != "" {
			return e.scheme + ":" + strings.TrimSpace(e.path)
		}
	}
	for _, e := range entries {
		if e.scheme == "size" {
			continue
		}
		p := strings.TrimSpace(e.path)
		if p != "" && (e.scheme == "file" || strings.ContainsAny(p, `/\`)) {
			if e.scheme != "" {
				return e.scheme + ":" + p
			}
			return p
		}
	}
	return ""
}

func detectContainerMIME(filename, forkTag string, payload []byte) string {
	if ext := strings.ToLower(path.Ext(filename)); ext != "" {
		if t := mime.TypeByExtension(ext); t != "" {
			if semi := strings.IndexByte(t, ';'); semi >= 0 {
				t = strings.TrimSpace(t[:semi])
			}
			return t
		}
	}
	switch forkTag {
	case "JPEG":
		return "image/jpeg"
	case "PNGf":
		return "image/png"
	case "PDF ":
		return "application/pdf"
	case "GIFf":
		return "image/gif"
	case "TIFF":
		return "image/tiff"
	case "BMPf":
		return "image/bmp"
	case "PICT":
		return "image/x-pict"
	case "SVG ":
		return "image/svg+xml"
	case "MOOV":
		return "video/quicktime"
	case "MP4 ":
		return "video/mp4"
	case "MP3 ":
		return "audio/mpeg"
	case "WAV ":
		return "audio/wav"
	case "ZIP ":
		return "application/zip"
	}
	if len(payload) > 0 {
		detected := http.DetectContentType(payload)
		if semi := strings.IndexByte(detected, ';'); semi >= 0 {
			detected = strings.TrimSpace(detected[:semi])
		}
		return detected
	}
	return "application/octet-stream"
}

func defaultContainerFilename(forkTag, mimeType string) string {
	switch forkTag {
	case "JPEG":
		return "container.jpg"
	case "PNGf":
		return "container.png"
	case "PDF ":
		return "container.pdf"
	case "GIFf":
		return "container.gif"
	case "TIFF":
		return "container.tiff"
	case "BMPf":
		return "container.bmp"
	case "PICT":
		return "container.pict"
	}
	if exts, err := mime.ExtensionsByType(mimeType); err == nil && len(exts) > 0 {
		return "container" + exts[0]
	}
	return "container.bin"
}

// externalContainerResolver resolves an external container reference path to
// its raw file bytes. Returns (bytes, true, nil) when found, (nil, false, nil)
// when missing.
type externalContainerResolver func(refPath string) ([]byte, bool, error)

func buildMapExternalResolver(files map[string]string) (externalContainerResolver, error) {
	if len(files) == 0 {
		return nil, nil
	}
	byNorm := make(map[string][]byte, len(files))
	byBase := make(map[string][]byte, len(files))
	baseCount := make(map[string]int, len(files))
	for rawPath, b64 := range files {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
		if err != nil {
			return nil, fmt.Errorf("invalid base64 in external_files[%q]: %w", rawPath, err)
		}
		norm := normalizeContainerRefPath(rawPath)
		byNorm[strings.ToLower(norm)] = decoded
		base := strings.ToLower(path.Base(norm))
		baseCount[base]++
		byBase[base] = decoded
	}
	return func(refPath string) ([]byte, bool, error) {
		for _, candidate := range candidateExternalPaths(refPath) {
			if b, ok := byNorm[strings.ToLower(candidate)]; ok {
				return b, true, nil
			}
		}
		base := strings.ToLower(path.Base(normalizeContainerRefPath(refPath)))
		if baseCount[base] == 1 {
			return byBase[base], true, nil
		}
		return nil, false, nil
	}, nil
}

// NewDirectoryExternalContainerResolver builds a safe local-directory resolver
// for CLI tools (such as migration-preflight -external-containers). Path
// traversal outside rootDir is never permitted.
func NewDirectoryExternalContainerResolver(rootDir string) func(refPath string) ([]byte, bool, error) {
	cleanRoot := filepath.Clean(rootDir)
	return func(refPath string) ([]byte, bool, error) {
		for _, candidate := range candidateExternalPaths(refPath) {
			rel := filepath.FromSlash(candidate)
			joined := filepath.Clean(filepath.Join(cleanRoot, rel))
			if !isWithinDir(cleanRoot, joined) {
				continue
			}
			info, err := os.Stat(joined)
			if err != nil || info.IsDir() {
				continue
			}
			b, err := os.ReadFile(joined)
			if err != nil {
				return nil, false, err
			}
			return b, true, nil
		}
		return nil, false, nil
	}
}

func isWithinDir(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func normalizeContainerRefPath(raw string) string {
	s := strings.TrimSpace(raw)
	if idx := strings.IndexByte(s, ':'); idx > 0 && idx < 16 {
		if isKnownContainerScheme(strings.ToLower(s[:idx])) {
			s = s[idx+1:]
		}
	}
	s = strings.ReplaceAll(s, `\`, "/")
	s = strings.TrimLeft(s, "/")
	return path.Clean(s)
}

func candidateExternalPaths(raw string) []string {
	norm := normalizeContainerRefPath(raw)
	if norm == "" || norm == "." {
		return nil
	}
	out := []string{norm}
	// Strip leading volume segment (e.g. filemac:/Macintosh HD/Files/foo.pdf -> Files/foo.pdf)
	// and progressively shorter suffixes so both full and relative trees match.
	parts := strings.Split(norm, "/")
	for i := 1; i < len(parts); i++ {
		out = append(out, strings.Join(parts[i:], "/"))
	}
	return out
}

func addContainerMeta(row map[string]interface{}, colName string, entry containermeta.Entry) {
	existing, _ := row[containermeta.Key].(map[string]containermeta.Entry)
	if existing == nil {
		existing = map[string]containermeta.Entry{}
		row[containermeta.Key] = existing
	}
	existing[colName] = entry
}

// externalStorageFile is FileMaker's "file" fork for a container stored
// outside the database: the original size as a little-endian uint64, the MD5
// of the file as written to disk, then the stored file name, NUL-terminated.
// Observed in files created by FileMaker Pro 26.0.2 (see testdata/containers).
type externalStorageFile struct {
	size uint64
	md5  []byte
	name string
}

func parseExternalStorageFile(b []byte) (externalStorageFile, bool) {
	if len(b) < 8+md5.Size+1 {
		return externalStorageFile{}, false
	}
	name := b[8+md5.Size:]
	if i := bytes.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}
	if len(name) == 0 {
		return externalStorageFile{}, false
	}
	return externalStorageFile{
		size: binary.LittleEndian.Uint64(b[:8]),
		md5:  b[8 : 8+md5.Size],
		name: string(name),
	}, true
}

// externalStorageContainer resolves a container that FileMaker keeps in
// external storage. Open storage ("REMO": 8 bytes, then the folder relative
// to the field's base directory, NUL-terminated) is read through the resolver
// and checked against the size and MD5 FileMaker recorded. Secure storage
// ("ENCR") holds an encrypted file that only FileMaker can read, so it is
// refused with the steps that make it migratable. handled is false when the
// forks do not describe external storage.
func externalStorageContainer(forks *hbam7ContainerForks, filename string, resolver externalContainerResolver) (out *reconstructedContainer, missingRef string, handled bool, err error) {
	if _, _, ok := forks.forkData("ENCR"); ok {
		return nil, "", true, fmt.Errorf("the field uses FileMaker secure external storage, whose files only FileMaker can decrypt; " +
			"in FileMaker, change the field to open external storage or embedded storage and transfer its data (File > Manage > Containers), then migrate again")
	}
	remo, _, ok := forks.forkData("REMO")
	if !ok {
		return nil, "", false, nil
	}
	raw, _, _ := forks.forkData("file")
	info, ok := parseExternalStorageFile(raw)
	if !ok || len(remo) < 8 {
		return nil, "", true, fmt.Errorf("the open external storage descriptor is incomplete")
	}
	dir := remo[8:]
	if i := bytes.IndexByte(dir, 0); i >= 0 {
		dir = dir[:i]
	}
	ref := path.Join(strings.ReplaceAll(string(dir), `\`, "/"), info.name)
	if resolver == nil {
		return nil, ref, true, nil
	}
	data, found, rerr := resolver(ref)
	if rerr != nil {
		return nil, "", true, rerr
	}
	if !found {
		return nil, ref, true, nil
	}
	// MD5 is FileMaker's own record of the stored file; it is compared here
	// only to prove the right file was found, not to secure anything.
	sum := md5.Sum(data) // NOSONAR
	if uint64(len(data)) != info.size || !bytes.Equal(sum[:], info.md5) {
		return nil, "", true, fmt.Errorf("external container file %q does not match the size and MD5 FileMaker recorded for it", ref)
	}
	if filename == "" {
		filename = info.name
	}
	digest := sha256.Sum256(data)
	return &reconstructedContainer{
		Bytes: data,
		Meta: containermeta.Entry{
			Filename:  filename,
			MimeType:  detectContainerMIME(filename, "", data),
			SHA256:    hex.EncodeToString(digest[:]),
			SizeBytes: int64(len(data)),
		},
	}, "", true, nil
}
