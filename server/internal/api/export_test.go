package api

import "github.com/file4base/file4base-app/server/internal/data"

// ParseSortOrderForTest exposes the `sort` query parser to the package's
// external tests.
func ParseSortOrderForTest(values []string) []data.SortField { return parseSortOrder(values) }

// SetBackupHookForTest runs f while a backup is taken, between reading its
// design and its records. It returns what undoes it.
func SetBackupHookForTest(f func()) func() {
	backupHookForTest = f
	return func() { backupHookForTest = nil }
}
