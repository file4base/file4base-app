package migration

import (
	"fmt"
	"sort"
	"strings"

	"github.com/file4base/file4base-app/server/internal/schema"
)

// collectFileReferences gives every external file a converted Perform Script
// step calls a file reference of its own (#107).
//
// FileMaker names the other file; File4Base needs to know which solution that
// is, and nothing in the design document says. So each reference is created
// unmapped — with no solution — and reported under manual_action_required.
// Until someone maps it, the step that uses it stops the script and names the
// reference: it is never resolved against the scripts of the solution being
// migrated, which is the same-named-local-script fallback #96 rules out.
func collectFileReferences(bundle *schema.SolutionBundle, report *ConversionReport) {
	type use struct {
		name  string
		steps []string
	}
	uses := map[string]*use{}

	for _, script := range bundle.Scripts {
		for _, step := range script.Steps {
			if !strings.EqualFold(step.StepType, "Perform Script") {
				continue
			}
			ref, _ := step.Params["file_reference"].(string)
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			key := strings.ToLower(ref)
			if uses[key] == nil {
				uses[key] = &use{name: ref}
			}
			uses[key].steps = append(uses[key].steps,
				fmt.Sprintf("%s (step %d)", script.Name, step.SequenceIdx))
		}
	}
	if len(uses) == 0 {
		return
	}

	keys := make([]string, 0, len(uses))
	for key := range uses {
		keys = append(keys, key)
	}
	sort.Strings(keys) // a report must come out the same way every time

	known := map[string]bool{}
	for _, existing := range bundle.FileReferences {
		known[strings.ToLower(existing.Name)] = true
	}

	for _, key := range keys {
		u := uses[key]
		if !known[key] {
			bundle.FileReferences = append(bundle.FileReferences, schema.BundleFileReference{Name: u.name})
			known[key] = true
		}
		addReportItem(report, ReportItem{
			Category:   CategoryManualActionRequired,
			Domain:     "script",
			SourceName: u.name,
			TargetName: u.name,
			Detail: fmt.Sprintf("Perform Script calls a script in the external file %q (%s). "+
				"A file reference of that name was created without a solution: map it to the "+
				"File4Base solution that file became. Until it is mapped those steps stop the "+
				"script; they never run a script of this solution that has the same name",
				u.name, strings.Join(u.steps, ", ")),
		})
	}
}
