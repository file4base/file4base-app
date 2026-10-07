package api

import "github.com/file4base/file4base-app/server/internal/data"

// ParseSortOrderForTest exposes the `sort` query parser to the package's
// external tests.
func ParseSortOrderForTest(values []string) []data.SortField { return parseSortOrder(values) }
