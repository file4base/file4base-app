package api

import (
	"context"
	"strings"

	"github.com/file4base/file4base-app/server/internal/migration"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// annotateExternalOccurrences completes the report of a converted file's
// external table occurrences (#107) with what only the server knows: the
// installed solution each data source most likely is — when exactly one
// solution's database or solution name matches the file a path names — and,
// for a signed-in caller, whether that solution's own external occurrences
// lead back to the file being migrated, a cycle: install both, then bind and
// verify each. Nothing is bound here; a suggestion is never applied.
//
// self names the file being migrated: its solution name and the database it
// goes to. Reads only.
func (h *SolutionHandler) annotateExternalOccurrences(ctx context.Context, report *migration.ConversionReport, signedIn bool, self ...string) {
	if report == nil || len(report.ExternalOccurrences) == 0 || h.dbMgr == nil {
		return
	}
	databases, err := h.dbMgr.ListDatabases(ctx)
	if err != nil {
		return
	}
	selfKeys := map[string]bool{}
	for _, s := range self {
		if k := fileKey(s); k != "" {
			selfKeys[k] = true
		}
	}

	// The keys each installed solution answers to.
	type installed struct {
		database string
		keys     map[string]bool
		svc      *schema.Service
	}
	var solutions []installed
	for _, db := range databases {
		if selfKeys[fileKey(db)] {
			continue
		}
		driver, err := h.dbMgr.DriverFor(ctx, db)
		if err != nil {
			continue
		}
		svc := schema.NewService(driver)
		if !svc.HasSystemCatalog(ctx) {
			continue
		}
		sol := installed{database: db, keys: map[string]bool{fileKey(db): true}, svc: svc}
		if meta, err := svc.GetSolutionMeta(ctx, db); err == nil && meta.SolutionName != "" {
			sol.keys[fileKey(meta.SolutionName)] = true
		}
		solutions = append(solutions, sol)
	}

	for i := range report.ExternalOccurrences {
		e := &report.ExternalOccurrences[i]
		wanted := map[string]bool{}
		for _, p := range e.Paths {
			if k := fileKey(p); k != "" {
				wanted[k] = true
			}
		}
		if len(wanted) == 0 {
			// A data source without a path is named after its file.
			wanted[fileKey(e.DataSource)] = true
		}
		var match []installed
		for _, sol := range solutions {
			for k := range wanted {
				if sol.keys[k] {
					match = append(match, sol)
					break
				}
			}
		}
		if len(match) != 1 {
			continue
		}
		e.SuggestedDatabase = match[0].database
		if signedIn {
			e.Cycle = leadsBack(ctx, match[0].svc, selfKeys)
		}
	}
}

// leadsBack reports whether one of a solution's external occurrences goes
// through a file reference mapped to, or naming a path to, the file self
// names.
func leadsBack(ctx context.Context, svc *schema.Service, self map[string]bool) bool {
	occurrences, err := svc.ListTableOccurrences(ctx)
	if err != nil {
		return false
	}
	used := map[string]bool{}
	for _, o := range occurrences {
		if o.External != nil {
			used[o.External.FileReferenceID] = true
		}
	}
	if len(used) == 0 {
		return false
	}
	refs, err := svc.ListFileReferences(ctx)
	if err != nil {
		return false
	}
	for _, ref := range refs {
		if !used[ref.ID] {
			continue
		}
		if self[fileKey(ref.Database)] || self[fileKey(ref.Name)] {
			return true
		}
		for _, p := range ref.Paths {
			if self[fileKey(p)] {
				return true
			}
		}
	}
	return false
}

// fileKey reduces a FileMaker path, a file name, a solution name or a
// database name to one comparable key: the file's name without its
// directory or extension, lower-cased, with every run of other characters
// than letters and digits as one underscore. "fmnet:/host/Sales Lead.fmp12",
// "Sales Lead" and "sales_lead" all give "sales_lead".
func fileKey(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexAny(s, "/\\:"); i >= 0 {
		s = s[i+1:]
	}
	for _, ext := range []string{".fmp12", ".fp7", ".fp5", ".fmp"} {
		if len(s) > len(ext) && strings.EqualFold(s[len(s)-len(ext):], ext) {
			s = s[:len(s)-len(ext)]
			break
		}
	}
	var b strings.Builder
	underscore := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			underscore = false
		} else if !underscore && b.Len() > 0 {
			b.WriteByte('_')
			underscore = true
		}
	}
	return strings.TrimSuffix(b.String(), "_")
}
