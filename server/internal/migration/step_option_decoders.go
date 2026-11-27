package migration

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/file4base/file4base-app/server/internal/schema"
)

// Decoders for the step options Save a Copy as XML writes in wrappers of its
// own: the Import Records field map, the Send Mail message, the Save Records
// as PDF destination, the Go to Portal Row target, the Replace Field Contents
// method and the Show Custom Dialog input fields. Each one turns what the step
// does in FileMaker into the operands the File4Base runner reads; whatever a
// decoder does not recognise is returned so the conversion report names it.

// universalPaths reads the paths of a <UniversalPathList>: one <Location> per
// path, or the list's own text, one path per line.
func universalPaths(n *xmlNode) []string {
	if n == nil {
		return nil
	}
	var out []string
	for _, loc := range findNodes(n, "Location") {
		out = append(out, pathLines(loc.Text)...)
	}
	if len(out) == 0 {
		out = pathLines(n.Text)
	}
	return out
}

// fileMakerFileName is the file a FileMaker path names: "file:Invoices",
// "fmnet:/host/Invoices.fmp12" and "filemac:/Disk/dir/Invoices.fmp12" all
// name "Invoices". A path computed from a variable names no file.
func fileMakerFileName(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || strings.Contains(path, "$") {
		return ""
	}
	if i := strings.IndexByte(path, ':'); i > 0 && isFileMakerPathStart(path[:i+1]) {
		path = path[i+1:]
	}
	path = strings.TrimRight(path, "/")
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		path = path[i+1:]
	}
	lower := strings.ToLower(path)
	for _, ext := range []string{".fmp12", ".fp7", ".fp5"} {
		if strings.HasSuffix(lower, ext) {
			path = path[:len(path)-len(ext)]
			break
		}
	}
	return strings.TrimSpace(path)
}

// Import Records methods, as FileMaker 2023 and later name them.
const (
	importMethodAdd     = "add"     // add new records
	importMethodUpdate  = "update"  // update matching records (match fields)
	importMethodReplace = "replace" // replace the found set, record by record
)

// decodeImportSource reads where an Import Records step reads from: a
// FileMaker file, another kind of file, or a folder of files.
func decodeImportSource(ds *xmlNode, out map[string]interface{}) {
	fileType := strings.ToUpper(firstAttr(ds, "fileType"))
	kind := "file"
	switch fileType {
	case "FMPR":
		kind = "filemaker"
	case "BTCH":
		kind = "folder"
	}
	paths := universalPaths(ds)
	computed := false
	for _, p := range paths {
		if strings.Contains(p, "$") {
			computed = true
		}
	}
	source := map[string]interface{}{"kind": kind, "paths": paths}
	if fileType != "" {
		source["file_type"] = fileType
	}
	if computed {
		source["path_is_calculation"] = true
	}
	if kind == "filemaker" {
		for _, p := range paths {
			if name := fileMakerFileName(p); name != "" {
				source["file"] = name
				break
			}
		}
	}
	out["import_source"] = source
}

// sourceFieldLabel matches the label FileMaker gives a source field it could
// not name because the source file was not open when the design was saved:
// "Source field 12 import to ".
var sourceFieldLabel = regexp.MustCompile(`(?i)^source field (\d+)\b`)

// mapVerbs are the endings FileMaker writes after the source field in a Map
// label; what precedes one is the source field's name, when FileMaker knew it.
var mapVerbs = []string{" not import to", " import to", " match with"}

// decodeImportField reads the field map of an Import Records step: the
// table it writes, the method, the auto-enter and repetition options, and
// for each target field whether it is imported into, matched on, or left
// alone, with the source field it is fed from.
//
// FileMaker refers to the source fields of a FileMaker file by position —
// "Source field 12" — and saves neither their names nor the source table
// unless the source file was open as the design was saved. A position is
// kept as a position; it is never resolved against a table File4Base picked.
func decodeImportField(n *xmlNode, out map[string]interface{}) []string {
	var unknown []string
	method := ""
	if action := findFirstChild(n, "action"); action != nil {
		switch strings.ToLower(firstAttr(action, "name")) {
		case "add":
			method = importMethodAdd
		case "update", "update matching":
			method = importMethodUpdate
		case "replace", "update existing":
			method = importMethodReplace
		}
		if method == "" {
			switch firstAttr(action, "value") {
			case "0":
				method = importMethodAdd
			case "1":
				method = importMethodReplace
			case "2":
				method = importMethodUpdate
			}
		}
		if method == "" {
			unknown = append(unknown, "Parameter.ImportField.action")
		}
	}
	if method == "" {
		method = importMethodAdd
	}
	out["import_method"] = method
	if v, ok := xmlAttrBool(n, "addasnew"); ok && method != importMethodAdd {
		out["add_remaining"] = v
	}
	if opts := findFirstChild(n, "Options"); opts != nil {
		for attr, key := range map[string]string{
			"doAutoEntry":        "auto_enter",
			"matchFieldNames":    "match_field_names",
			"splitRepetitions":   "split_repetitions",
			"keepOriginalData":   "keep_original_data",
			"copyContainersAsIs": "copy_containers_as_is",
		} {
			if v, ok := xmlAttrBool(opts, attr); ok {
				out[key] = v
			}
		}
	}
	if cs := findFirstChild(n, "CharacterSet"); cs != nil {
		if name := firstAttr(cs, "name"); name != "" {
			out["character_set"] = name
		}
	}
	if target := findFirstChild(n, "Target"); target != nil {
		if occ := findFirstChild(target, "TableOccurrenceReference"); occ != nil {
			if name := firstAttr(occ, "name", "Name"); name != "" {
				out["target_occurrence"] = name
			}
		}
	}

	var mappings []map[string]interface{}
	skipped, deletedTargets := 0, 0
	// FileMaker 2023 and later choose auto-enter field by field: a Map whose
	// Options carry 2 performs the field's auto-enter options, on a field
	// imported into or not. When the maps carry Options at all, that choice
	// is the one FileMaker applies, and its own step text shows it as "(Do
	// Auto-Enter)"; the document-wide doAutoEntry is only read without them.
	perField := false
	var autoEnterFields, autoEnterIDs []string
	if fields := findFirstChild(n, "Field"); fields != nil {
		for _, m := range fields.Children {
			if m.Name != "Map" {
				continue
			}
			ref := findFirstChild(m, "FieldReference")
			if ref == nil {
				// A field imported into or matched on whose reference
				// FileMaker no longer writes was deleted from the target
				// table, or the table itself was: FileMaker imports
				// nothing into it.
				if kind := firstAttr(m, "kind"); kind == "0" || kind == "2" {
					deletedTargets++
				}
				continue
			}
			if raw, ok := m.Attrs["Options"]; ok {
				perField = true
				if bits, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && bits&2 != 0 {
					if name := sanitizeIdentifier(firstAttr(ref, "name", "Name")); name != "" {
						id := ""
						if n, err := strconv.Atoi(strings.TrimSpace(firstAttr(ref, "id", "ID"))); err == nil && n > 0 {
							id = strconv.Itoa(n)
						}
						autoEnterFields = append(autoEnterFields, name)
						autoEnterIDs = append(autoEnterIDs, id)
					}
				}
			}
			var action string
			switch firstAttr(m, "kind") {
			case "0":
				action = "import"
			case "2":
				action = "match"
			case "1":
				skipped++
				continue
			default:
				unknown = append(unknown, "Parameter.ImportField.Map")
				continue
			}
			target := firstAttr(ref, "name", "Name")
			entry := map[string]interface{}{
				"action":             action,
				"target_field":       sanitizeIdentifier(target),
				"target_field_label": target,
			}
			// The target field's FileMaker id finds its column even where
			// the migration had to rename it.
			if id, err := strconv.Atoi(strings.TrimSpace(firstAttr(ref, "id", "ID"))); err == nil && id > 0 {
				entry["target_field_id"] = strconv.Itoa(id)
			}
			if pos, err := strconv.Atoi(firstAttr(m, "index")); err == nil && pos > 0 {
				entry["source_position"] = pos
			}
			label := strings.TrimSpace(firstAttr(m, "name"))
			if label != "" && !sourceFieldLabel.MatchString(label) {
				name := label
				for _, verb := range mapVerbs {
					if strings.HasSuffix(strings.ToLower(name), verb) {
						name = strings.TrimSpace(name[:len(name)-len(verb)])
						break
					}
				}
				if name != "" {
					entry["source_field"] = name
				}
			}
			mappings = append(mappings, entry)
		}
	}
	if mappings == nil {
		mappings = []map[string]interface{}{}
	}
	out["field_mappings"] = mappings
	if perField {
		if autoEnterFields == nil {
			autoEnterFields, autoEnterIDs = []string{}, []string{}
		}
		// The FileMaker ids, one per name, find a field the migration
		// renamed.
		out["auto_enter_fields"] = autoEnterFields
		out["auto_enter_field_ids"] = autoEnterIDs
	}
	out["skipped_source_fields"] = skipped
	if deletedTargets > 0 {
		out["deleted_target_fields"] = deletedTargets
	}
	return unknown
}

// markSelfImports marks the Import Records steps that import from the very
// file the design was saved from, so the migration review proposes this
// solution as their source (#191).
func markSelfImports(bundle *schema.SolutionBundle, designFileName string) {
	self := fileMakerFileName(designFileName)
	if bundle == nil || self == "" {
		return
	}
	for i := range bundle.Scripts {
		for j := range bundle.Scripts[i].Steps {
			params := bundle.Scripts[i].Steps[j].Params
			if params["awaiting_transfer"] != true {
				continue
			}
			source, _ := params["import_source"].(map[string]interface{})
			if file, _ := source["file"].(string); file != "" && strings.EqualFold(file, self) {
				source["this_file"] = true
			}
		}
	}
}

// importRecordsNote says, for the report and for the script editor, what an
// Import Records step converted from FileMaker did and why it does not run
// until a transfer definition is named in it.
func importRecordsNote(params map[string]interface{}, targetTable string) string {
	source, _ := params["import_source"].(map[string]interface{})
	mappings, _ := params["field_mappings"].([]map[string]interface{})
	method, _ := params["import_method"].(string)

	var what strings.Builder
	switch kind, _ := source["kind"].(string); {
	case source == nil:
		what.WriteString("imports records")
	case kind == "filemaker":
		if file, _ := source["file"].(string); file != "" {
			fmt.Fprintf(&what, "imports from the FileMaker file %q", file)
		} else {
			what.WriteString("imports from a FileMaker file whose path is computed when the script runs")
		}
	case kind == "folder":
		what.WriteString("imports a folder of files")
	default:
		what.WriteString("imports from a file on disk")
	}
	if targetTable != "" {
		fmt.Fprintf(&what, " into %s", targetTable)
	}
	var keys, imported, positions, pairs []string
	named := 0
	for _, m := range mappings {
		field, _ := m["target_field"].(string)
		if m["action"] == "match" {
			keys = append(keys, field)
		} else {
			imported = append(imported, field)
		}
		from := "?"
		if name, ok := m["source_field"].(string); ok {
			named++
			from = name
		} else if pos, ok := m["source_position"].(int); ok {
			positions = append(positions, strconv.Itoa(pos))
			from = "source field " + strconv.Itoa(pos)
		}
		pairs = append(pairs, from+" -> "+field)
	}
	switch method {
	case importMethodUpdate:
		fmt.Fprintf(&what, ", updating the records that match on %s", joinOrNone(keys))
		if v, _ := params["add_remaining"].(bool); v {
			what.WriteString(" and adding the others")
		}
	case importMethodReplace:
		what.WriteString(", replacing the found set record by record")
	default:
		what.WriteString(" as new records")
	}
	fmt.Fprintf(&what, " (%d field(s) imported", len(imported))
	if v, ok := params["auto_enter"].(bool); ok {
		if v {
			what.WriteString(", auto-enter on")
		} else {
			what.WriteString(", auto-enter off")
		}
	}
	what.WriteString(")")

	var why string
	byName, _ := params["match_field_names"].(bool)
	switch kind, _ := source["kind"].(string); {
	case kind == "filemaker" && len(mappings) == 0 && intParam(params["deleted_target_fields"]) > 0:
		why = fmt.Sprintf("the %d field(s) it imports into or matches on were deleted from the target table in "+
			"FileMaker, or the table itself was, so FileMaker imports nothing either. The step stays disabled",
			intParam(params["deleted_target_fields"]))
	case kind == "filemaker" && byName:
		why = fmt.Sprintf("FileMaker pairs its fields by matching names each time the step runs, and did not "+
			"save the source table. Choose the solution that file became and its table under \"Import Records "+
			"from other FileMaker files\" in the migration review: it creates a transfer into %s that pairs %s "+
			"by name at each run and names it in the step, which stays disabled until then",
			orTable(targetTable), joinOrNone(append(append([]string{}, keys...), imported...)))
	case kind == "filemaker" && len(positions) > 0:
		why = fmt.Sprintf("FileMaker saved its source fields by position only, not by name, and did not save "+
			"the source table. Choose the solution that file became and its table under \"Import Records from "+
			"other FileMaker files\" in the migration review: it creates the transfer into %s mapping %s and "+
			"names it in the step, which stays disabled until then",
			orTable(targetTable), strings.Join(pairs, ", "))
	case kind == "filemaker" && named < len(mappings):
		why = "the source fields it reads are not named in the document. Create a transfer and name it in the step"
	case kind == "filemaker":
		why = fmt.Sprintf("FileMaker did not save the source table. Choose the solution that file became and "+
			"its table in the migration review: it creates the transfer into %s mapping %s and names it in the "+
			"step", orTable(targetTable), strings.Join(pairs, ", "))
	default:
		why = "File4Base scripts never read files from a disk. Import the file by hand (File > Import), or " +
			"move the data into a solution and run a transfer from it"
	}
	return what.String() + "; " + why
}

func joinOrNone(fields []string) string {
	if len(fields) == 0 {
		return "no field"
	}
	return strings.Join(fields, ", ")
}

func orTable(table string) string {
	if table == "" {
		return "the target table"
	}
	return table
}

// decodeSendMail reads a <Send> element of a Send Mail step: how the mail
// leaves, its addressees, subject and body, and whether it is sent once per
// record of the found set.
//
// A step sent "via SMTP server" also carries FileMaker's SMTP options. The
// sender's name and address and the reply-to address are kept: File4Base
// sends with them. The server, port, account and password written in the
// step are not: File4Base sends through the SMTP profile a server
// administrator sets, and a password read from a design file is never
// stored in a converted step.
func decodeSendMail(send *xmlNode, out map[string]interface{}) []string {
	var unknown []string
	if v, ok := xmlAttrBool(send, "SMTP"); ok {
		if v {
			out["send_via"] = "smtp"
		} else {
			out["send_via"] = "email_client"
		}
	}
	if v, ok := xmlAttrBool(send, "OAuthAuthentication"); ok && v {
		out["oauth"] = true
	}
	for _, ch := range send.Children {
		switch ch.Name {
		case "To", "CC", "BCC", "Subject", "Message":
			key := strings.ToLower(ch.Name)
			readMailValue(ch, key, out)
			if collect := findFirstChild(ch, "CollectAddresses"); collect != nil {
				if v, ok := xmlAttrBool(collect, "value"); ok && v {
					out["collect_"+key] = true
				}
			}
		case "Multiple":
			if v, ok := xmlAttrBool(ch, "value"); ok {
				out["one_email_per_record"] = v
			}
		case "SMTP", "SMTPOptions", "SMTPServer", "SMTPInfo":
			unknown = append(unknown, decodeSMTPOptions(ch, out)...)
		default:
			if !decodeSMTPOption(ch, out) {
				unknown = append(unknown, "Parameter.Email."+ch.Name)
			}
		}
	}
	return unknown
}

// readMailValue keeps a Send Mail value under key: a calculation, a value
// attribute, or text.
func readMailValue(n *xmlNode, key string, out map[string]interface{}) {
	if calc := findFirstChild(n, "Calculation"); calc != nil {
		out[key] = calculationText(calc)
		out[key+"_is_calculation"] = true
	} else if v := firstAttr(n, "value"); v != "" {
		out[key] = v
	} else if t := strings.TrimSpace(n.Text); t != "" {
		out[key] = t
	}
}

// smtpOptionKeys are the SMTP options of a Send Mail step File4Base sends
// with, by the lower-cased names FileMaker may give them.
var smtpOptionKeys = map[string]string{
	"fromname":       "smtp_from_name",
	"name":           "smtp_from_name",
	"from":           "smtp_from",
	"fromaddress":    "smtp_from",
	"fromemail":      "smtp_from",
	"email":          "smtp_from",
	"emailaddress":   "smtp_from",
	"replyto":        "smtp_reply_to",
	"replytoaddress": "smtp_reply_to",
	"replytoemail":   "smtp_reply_to",
}

// smtpServerOptions are the SMTP options of a Send Mail step that File4Base
// does not take from the step (the profile says them), by lower-cased name.
var smtpServerOptions = map[string]bool{
	"server": true, "smtpserver": true, "host": true, "port": true, "authentication": true,
	"auth": true, "encryption": true, "security": true, "ssl": true, "tls": true,
	"username": true, "user": true, "account": true, "password": true,
}

// decodeSMTPOptions reads a block of SMTP options.
func decodeSMTPOptions(n *xmlNode, out map[string]interface{}) []string {
	var unknown []string
	for _, ch := range n.Children {
		if !decodeSMTPOption(ch, out) {
			unknown = append(unknown, "Parameter.Email.SMTP."+ch.Name)
		}
	}
	return unknown
}

// decodeSMTPOption reads one SMTP option, and reports whether it was one.
func decodeSMTPOption(n *xmlNode, out map[string]interface{}) bool {
	name := strings.ToLower(n.Name)
	if key, ok := smtpOptionKeys[name]; ok {
		readMailValue(n, key, out)
		return true
	}
	if smtpServerOptions[name] {
		out["smtp_server_in_step"] = true
		return true
	}
	return false
}

// fixedDiskPaths are the paths of a list that name a fixed place on a
// FileMaker user's disk ("filemac:/Macintosh HD/Users/…", "/C:/Reports/…"),
// which File4Base cannot write or read: a script reaches only the File4Base
// folders, through Get ( TemporaryPath ), Get ( DocumentsPath ) and
// Get ( DesktopPath ). A path held by a variable, and a path relative to the
// file ("file:Report.pdf", saved in the documents folder), are not.
func fixedDiskPaths(paths []string) []string {
	var out []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || strings.Contains(p, "$") {
			continue
		}
		rest := p
		if i := strings.IndexByte(rest, ':'); i > 0 {
			switch strings.ToLower(rest[:i]) {
			case "file", "filemac", "filewin", "filelinux", "image", "imagemac", "imagewin":
				rest = rest[i+1:]
			}
		}
		rest = strings.ReplaceAll(rest, "\\", "/")
		if strings.HasPrefix(rest, "/") || (len(rest) > 1 && rest[1] == ':') {
			out = append(out, p)
		}
	}
	return out
}

func stringList(v interface{}) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if t != "" {
			return []string{t}
		}
	}
	return nil
}

// fixedPathNote explains why fixed paths of a step will not be found.
func fixedPathNote(step string, fixed []string) string {
	quoted := make([]string, len(fixed))
	for i, p := range fixed {
		quoted[i] = fmt.Sprintf("%q", p)
	}
	return fmt.Sprintf("the path %s is a fixed place on a FileMaker user's disk, and a File4Base script reaches only "+
		"its own folders: %s fails there with error 800 (cannot create the file) or 100 (file missing). Build the "+
		"path from Get ( TemporaryPath ), Get ( DocumentsPath ) or Get ( DesktopPath )",
		strings.Join(quoted, ", "), step)
}

// sendMailNote explains what a Send Mail step converted from FileMaker
// needs to run as written, or "" when it runs as FileMaker runs it.
//
// File4Base sends mail itself through an SMTP profile that a server
// administrator sets in Server Tools (one per solution, or the server's).
// A step sent via the SMTP server needs one. A step sent via the e-mail
// client without a dialog — FileMaker sends it without asking — is sent
// through the profile when there is one, a deliberate deviation: it does not
// go through the user's e-mail client. A step with a dialog opens a draft in
// the user's e-mail client, as in FileMaker; a draft cannot carry
// attachments, so a step with attachments opens File4Base's own compose
// window and sends through the profile instead.
func sendMailNote(params map[string]interface{}) string {
	var why []string
	smtp := params["send_via"] == "smtp"
	dialog, _ := params["dialog"].(bool)
	attachments := stringList(params["attachments"])
	perRecord, _ := params["one_email_per_record"].(bool)
	switch {
	case smtp:
		what := "it sends through an SMTP server: File4Base sends it through the SMTP profile a server administrator " +
			"sets for this solution, or for the server, in Server Tools (without one the step fails with error 1506)"
		if params["smtp_server_in_step"] == true {
			what += "; the server, account and password written in the step are not used"
		}
		why = append(why, what)
	case !dialog:
		why = append(why, "FileMaker sends it via the e-mail client without asking: File4Base sends it through the "+
			"solution's SMTP profile when a server administrator has set one in Server Tools (a deliberate deviation: "+
			"it does not go through the user's e-mail client, and it leaves from the profile's address); without "+
			"one it opens a draft that the user sends")
	case len(attachments) > 0:
		why = append(why, "it attaches files, which a draft in the e-mail client cannot carry: File4Base opens its own "+
			"compose window with the files and sends through the solution's SMTP profile when a server administrator "+
			"has set one in Server Tools; without one it opens the draft and tells the user which files to attach")
	case perRecord:
		why = append(why, "it sends one e-mail per record of the found set, which File4Base does through the "+
			"solution's SMTP profile (set in Server Tools); without one the step fails with error 1506")
	}
	if fixed := fixedDiskPaths(attachments); len(fixed) > 0 {
		why = append(why, fixedPathNote("attaching it", fixed))
	}
	if len(why) == 0 {
		return ""
	}
	return "Send Mail: " + strings.Join(why, "; ")
}

// savePDFNote explains what a Save Records as PDF step converted from
// FileMaker needs to run as written, or "" when it runs as written.
func savePDFNote(params map[string]interface{}) string {
	var why []string
	if params["save_to"] == "variable" {
		why = append(why, "it saves the PDF into a variable, which File4Base does not do: the step fails with "+
			"error 3 (command unavailable); save it to a file instead")
	}
	if params["pdf_security"] == true {
		why = append(why, "it protects the PDF with a password (to open it, or to print or edit it), which File4Base "+
			"does not do: rather than write an unprotected PDF the step fails with error 3 (command unavailable); "+
			"the password was not migrated")
	}
	path, _ := params["path"].(string)
	paths := stringList(params["paths"])
	if len(paths) == 0 && path != "" {
		paths = []string{path}
	}
	if fixed := fixedDiskPaths(paths); len(fixed) > 0 {
		why = append(why, fixedPathNote("saving the PDF", fixed))
	}
	if len(why) == 0 {
		return ""
	}
	return "Save Records as PDF: " + strings.Join(why, "; ")
}

// pdfSecurityRequested tells whether a Save Records as PDF step asks for a
// protected PDF: a password to open it (<Security><Open value="True">) or
// to print, edit or copy from it (<Control value="True">), or any password
// the step carries.
func pdfSecurityRequested(step *xmlNode) bool {
	var walk func(n *xmlNode, inSecurity bool) bool
	walk = func(n *xmlNode, inSecurity bool) bool {
		if isSecretName(n.Name) || isSecretName(firstAttr(n, "type")) {
			if v, ok := xmlAttrBool(n, "value"); !ok || v {
				if ok || strings.TrimSpace(n.Text) != "" || len(n.Children) > 0 || len(n.Attrs) > 0 {
					return true
				}
			}
		}
		for k, v := range n.Attrs {
			if isSecretName(k) && strings.TrimSpace(v) != "" {
				return true
			}
		}
		if inSecurity && (strings.EqualFold(n.Name, "Open") || strings.EqualFold(n.Name, "Control")) {
			if v, ok := xmlAttrBool(n, "value"); ok && v {
				return true
			}
		}
		security := inSecurity || strings.EqualFold(n.Name, "Security")
		for _, ch := range n.Children {
			if walk(ch, security) {
				return true
			}
		}
		return false
	}
	return walk(step, false)
}

// decodeReplaceOptions reads the <List> of a Replace Field Contents step:
// replace with the current contents, with serial numbers, or with a
// calculation evaluated for each record.
func decodeReplaceOptions(list *xmlNode, out map[string]interface{}) []string {
	var unknown []string
	name := strings.ToLower(firstAttr(list, "name"))
	switch {
	case firstAttr(list, "value") == "3" || strings.Contains(name, "calculation"):
		out["replace_type"] = "calculation"
	case firstAttr(list, "value") == "2" || strings.Contains(name, "serial"):
		out["replace_type"] = "serial_numbers"
	case firstAttr(list, "value") == "1" || strings.Contains(name, "current"):
		out["replace_type"] = "current_contents"
	default:
		unknown = append(unknown, "Parameter.replace")
	}
	for _, ch := range list.Children {
		switch {
		case ch.Name == "Calculation" && out["replace_type"] == "calculation":
			out["value"] = calculationText(ch)
			out["value_is_calculation"] = true
		case ch.Name == "Boolean" && strings.Contains(strings.ToLower(firstAttr(ch, "type")), "auto-enter"):
			if v, ok := xmlAttrBool(ch, "value"); ok {
				out["skip_auto_enter"] = v
			}
		case out["replace_type"] == "serial_numbers":
			// FileMaker's serial-number operands: the initial value, the
			// increment, and whether the field's own serial is updated.
			kind := strings.ToLower(ch.Name + " " + firstAttr(ch, "type", "name"))
			text := firstAttr(ch, "value")
			if calc := findFirstChild(ch, "Calculation"); calc != nil {
				text = calculationText(calc)
			}
			switch {
			case strings.Contains(kind, "initial"):
				out["serial_initial"] = text
			case strings.Contains(kind, "increment"):
				out["serial_increment"] = text
			case strings.Contains(kind, "update"):
				if v, ok := xmlAttrBool(ch, "value"); ok {
					out["serial_update_entry_options"] = v
				}
			default:
				unknown = append(unknown, "Parameter.replace."+ch.Name)
			}
		default:
			unknown = append(unknown, "Parameter.replace."+ch.Name)
		}
	}
	if out["replace_type"] == "serial_numbers" {
		if _, ok := out["serial_initial"]; !ok {
			unknown = append(unknown, "Parameter.replace (serial numbers)")
		}
	}
	return unknown
}

// decodeDialogInputParameter reads one Show Custom Dialog input field written
// as <Parameter type="Field1">: the field or variable it fills, its label and
// whether it hides what is typed.
func decodeDialogInputParameter(p *xmlNode) map[string]interface{} {
	entry := map[string]interface{}{}
	for _, ch := range p.Children {
		switch ch.Name {
		case "Boolean":
			if strings.EqualFold(firstAttr(ch, "type"), "Password") {
				if v, ok := xmlAttrBool(ch, "value"); ok {
					entry["password"] = v
				}
			}
		case "Parameter":
			switch firstAttr(ch, "type") {
			case "Target":
				if fr := findFirstChild(ch, "FieldReference"); fr != nil {
					name := firstAttr(fr, "name", "Name")
					if name == "" {
						continue
					}
					entry["field"] = sanitizeIdentifier(name)
					entry["field_label"] = name
					if occ := findFirstChild(fr, "TableOccurrenceReference"); occ != nil {
						entry["field_table"] = firstAttr(occ, "name", "Name")
					}
				} else if v := findFirstChild(ch, "Variable"); v != nil {
					if name := firstAttr(v, "value", "name"); name != "" {
						entry["field"] = name
					}
				}
			case "Label":
				if calc := findFirstChild(ch, "Calculation"); calc != nil {
					entry["label"] = calculationText(calc)
					entry["label_is_calculation"] = true
				}
			}
		}
	}
	return entry
}

// stepOptionItems reports what a converted step's FileMaker options mean for
// running it in File4Base, and leaves the same explanation in the step as
// migration_note, which the script editor shows next to the step and the
// runner gives when the step stops. A step that runs as written gets none.
func stepOptionItems(canonical string, params map[string]interface{}, occTableByName map[string]string) []ReportItem {
	note := ""
	switch canonical {
	case "Import Records":
		if _, ok := params["field_mappings"]; !ok {
			return nil
		}
		table := ""
		if occ, _ := params["target_occurrence"].(string); occ != "" {
			table = occTableByName[strings.ToLower(occ)]
		} else if layout, _ := params["current_layout_table"].(string); layout != "" {
			// Saved without its target, the step imports into the table
			// of the current layout, which the Go to Layout before it
			// fixes.
			table = layout
			params["target_table_from_layout"] = true
		}
		delete(params, "current_layout_table")
		if table != "" {
			params["target_table"] = table
		}
		note = "Import Records " + importRecordsNote(params, table)
		if source, _ := params["import_source"].(map[string]interface{}); source["kind"] == "filemaker" {
			params["awaiting_transfer"] = true
		}
	case "Perform Script":
		switch {
		case params["script_missing"] == true:
			note = "Perform Script names no script: FileMaker wrote it as <unknown>, which it does for a script " +
				"that was deleted. FileMaker skips such a step with error 104 (script missing); File4Base " +
				"does the same and stops the script unless Set Error Capture is on. Point the step at a script or remove it"
		case params["script_unresolved"] == true:
			ref, _ := params["file_reference"].(string)
			note = fmt.Sprintf("Perform Script calls a script of the file %q that the document does not name "+
				"(FileMaker writes <unknown> when that file was not open as the design was saved). Name the "+
				"script in the step, or save the design again with %q open", ref, ref)
		}
	case "Send Mail":
		note = sendMailNote(params)
	case "Save Records as PDF":
		note = savePDFNote(params)
	}
	if note == "" {
		return nil
	}
	params["migration_note"] = note
	return []ReportItem{{
		Category: CategoryManualActionRequired,
		Domain:   "script",
		Detail:   note,
	}}
}

// intParam reads a count a decoder left in a step's parameters.
func intParam(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}
