// Command migration-preflight audits explicitly selected local files. It never
// connects to a database or executes scripts, triggers, schedules or transfers.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/file4base/file4base-app/server/internal/migration"
)

func main() { os.Exit(run()) }
func run() int {
	design := flag.String("design", "", "design XML file")
	native := flag.String("native", "", "native database copy")
	order := flag.String("native-date-order", "iso", "native date order: iso, dmy, mdy")
	output := flag.String("report", "", "new private report file (must not already exist)")
	flag.Parse()
	if *design == "" || *native == "" || *output == "" {
		flag.Usage()
		return 1
	}
	designBytes, err := os.ReadFile(*design)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read design input")
		return 1
	}
	nativeBytes, err := os.ReadFile(*native)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read native input")
		return 1
	}
	converted, err := migration.AnalyzeAndConvertNative(migration.MigrationRequest{DesignXML: string(designBytes), NativeDateOrder: *order}, nativeBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "input conversion failed; no target was changed")
		return 1
	}
	report, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot create a new private report")
		return 1
	}
	encoder := json.NewEncoder(report)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(converted.Report)
	closeErr := report.Close()
	if encodeErr != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, "report write failed")
		return 1
	}
	counts := converted.Report.Counts
	fmt.Printf("tables=%d detected_records=%d ready_records=%d rejected=%d unsupported=%d manual_actions=%d\n", counts.Tables, counts.RecordsDetected, counts.RecordsReady, counts.RejectedCount, counts.UnsupportedCount, counts.ManualActionCount)
	if counts.RejectedCount > 0 {
		return 2
	}
	return 0
}
