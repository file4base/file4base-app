package designreport

import (
	"strings"
	"testing"
)

// A transfer pairing its fields by name at each run says so, as its mappings
// are only the pairing found when it was saved (#191).
func TestWriteHTMLMarksATransferPairingByName(t *testing.T) {
	report := &Report{Transfers: []Transfer{
		{Name: "by name", Source: "a.lines", Destination: "items", Mode: "add", MatchByName: true,
			Mappings: []TransferMapping{{Source: "CODE", Destination: "code"}}},
		{Name: "by field", Source: "a.lines", Destination: "items", Mode: "add",
			Mappings: []TransferMapping{{Source: "CODE", Destination: "code"}}},
	}}
	out, err := WriteHTML(report)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(out), "Paired by name at each run"); n != 1 {
		t.Fatalf("marked %d time(s):\n%s", n, out)
	}
	js, err := WriteJSON(report)
	if err != nil || !strings.Contains(string(js), `"match_by_name": true`) {
		t.Fatalf("json = %s, %v", js, err)
	}
}
