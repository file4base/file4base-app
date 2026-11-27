package migration

import (
	"fmt"
	"sort"
	"strings"

	"github.com/file4base/file4base-app/server/internal/schema"
)

// privilegeLayoutAccess reads, for each privilege set with custom layout
// privileges, the access its accounts get to each migrated layout:
// layoutIDs maps a FileMaker layout id to the migrated layout's. A layout
// the set has no access to is none, and so is one whose records it may not
// see, File4Base having no layout that shows no records; one whose records
// it may only view is read_only; the others keep File4Base's default,
// read_write. A set with no access to any layout has none on all of them.
// Folders and separators carry no access here: FileMaker lists a folder
// whatever the set says of it.
func privilegeLayoutAccess(root *xmlNode, layoutIDs map[string]string) map[string][]schema.BundlePermission {
	out := map[string][]schema.BundlePermission{}
	for _, set := range catalogDefinitions(root, []string{"PrivilegeSetsCatalog"}, "PrivilegeSet") {
		name := strings.ToLower(strings.TrimSpace(firstAttr(set, "name")))
		layouts := childPath(set, "access", "Layouts")
		if layouts == nil {
			layouts = findFirstChild(set, "Layouts")
		}
		if name == "" || layouts == nil {
			continue
		}
		var perms []schema.BundlePermission
		if strings.EqualFold(firstAttr(layouts, "View"), "NoAccess") {
			// Unverified: no file at hand has "All no access" for layouts.
			for _, id := range sortedValues(layoutIDs) {
				perms = append(perms, schema.BundlePermission{LayoutID: id, AccessLevel: schema.AccessNone})
			}
			out[name] = perms
			continue
		}
		if !strings.EqualFold(firstAttr(layouts, "Custom"), "true") {
			continue
		}
		seen := map[string]bool{}
		for _, l := range findNodes(layouts, "Layout") {
			ref := findFirstChild(l, "LayoutReference")
			if ref == nil {
				continue // the privilege of layouts created later
			}
			id := layoutIDs[firstAttr(ref, "id")]
			if id == "" || seen[id] {
				continue
			}
			level := ""
			switch {
			case strings.EqualFold(firstAttr(l, "access"), "NoAccess"), strings.EqualFold(firstAttr(l, "records"), "NoAccess"):
				level = schema.AccessNone
			case strings.EqualFold(firstAttr(l, "records"), "ReadOnly"):
				level = schema.AccessReadOnly
			default:
				continue
			}
			seen[id] = true
			perms = append(perms, schema.BundlePermission{LayoutID: id, AccessLevel: level})
		}
		if len(perms) > 0 {
			out[name] = perms
		}
	}
	return out
}

func sortedValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	seen := map[string]bool{}
	for _, v := range m {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// applyLayoutAccess gives each migrated account its privilege set's layout
// access, File4Base keeping it per account, and reports it per set.
func applyLayoutAccess(users []schema.BundleAccount, setOf map[string]string, access map[string][]schema.BundlePermission) []ReportItem {
	accounts := map[string][]string{}
	for i := range users {
		u := &users[i]
		if u.Role == "owner" || u.Role == "admin" {
			continue // they reach every layout
		}
		set := setOf[u.ID]
		perms := access[strings.ToLower(set)]
		if len(perms) == 0 {
			continue
		}
		u.Permissions = append([]schema.BundlePermission(nil), perms...)
		accounts[set] = append(accounts[set], u.Username)
	}
	sets := make([]string, 0, len(accounts))
	for set := range accounts {
		sets = append(sets, set)
	}
	sort.Strings(sets)
	var items []ReportItem
	for _, set := range sets {
		hidden, readOnly := 0, 0
		for _, p := range access[strings.ToLower(set)] {
			if p.AccessLevel == schema.AccessNone {
				hidden++
			} else {
				readOnly++
			}
		}
		items = append(items, ReportItem{
			Category: CategoryTranslated, Domain: "account", SourceName: set, TargetName: strings.Join(accounts[set], ", "),
			Detail: fmt.Sprintf("Layout privileges of privilege set %q given to its account(s): %d layout(s) without access, %d with read-only records", set, hidden, readOnly),
		})
	}
	return items
}
