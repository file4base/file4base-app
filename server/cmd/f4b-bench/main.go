// Command f4b-bench runs the same solution workload on PostgreSQL, MariaDB
// and SQLite through File4Base's own services, and reports latencies,
// throughput, sizes and result fingerprints (docs/specs/sqlite_engine.md,
// "Benchmarks"). Every value it writes is synthetic.
//
//	f4b-bench -engine sqlite   -admin /var/tmp/f4b-bench        -run all
//	f4b-bench -engine postgres -admin postgres://.../postgres  -container f4b-pg -run all
//	f4b-bench report sqlite.json postgres.json mariadb.json
//
// The database it creates (bench_<rows>) is dropped first and kept at the
// end, so a scenario can be run again on it with -reuse.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"slices"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/dbal/mariadb"
	"github.com/file4base/file4base-app/server/internal/dbal/postgres"
	"github.com/file4base/file4base-app/server/internal/dbal/sqlite"
)

func init() {
	dbal.RegisterDialect(dbal.EnginePostgres, func() dbal.Dialect { return postgres.New() })
	dbal.RegisterDialect(dbal.EngineMariaDB, func() dbal.Dialect { return mariadb.New() })
	dbal.RegisterDialect(dbal.EngineSQLite, func() dbal.Dialect { return sqlite.New() })
}

// The scenarios, in the order "all" runs them. crash and storage are
// SQLite's own; vacuum is SQLite's backup under load.
var scenarioOrder = []string{
	"fingerprint", "editors", "combined", "find", "import", "typechange", "storage",
	"backup", "vacuum", "crash", "unsafe-storage",
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "report":
			if err := report(os.Stdout, os.Args[2:]); err != nil {
				log.Fatal(err)
			}
			return
		case "crash-child":
			crashChild(os.Args[2:])
			return
		}
	}

	engine := flag.String("engine", string(dbal.DefaultEngine), "postgres, mariadb or sqlite")
	admin := flag.String("admin", "", "admin DSN (PostgreSQL, MariaDB) or data directory (SQLite)")
	rows := flag.Int("rows", 1_000_000, "records in the members table")
	noteBytes := flag.Int("note-bytes", 60, "length of each record's notes")
	run := flag.String("run", "all", "comma-separated scenarios: "+strings.Join(scenarioOrder, ","))
	duration := flag.Duration("duration", 15*time.Second, "length of each timed load")
	container := flag.String("container", "", "docker container of the server, for dumps and sizes (PostgreSQL, MariaDB)")
	out := flag.String("out", "", "write the results as JSON to this file")
	reuse := flag.Bool("reuse", false, "use the existing bench database instead of seeding a new one")
	busy := flag.Duration("busy-timeout", dbal.DefaultSQLiteBusyTimeout, "SQLite busy_timeout")
	backupDir := flag.String("backup-dir", "", "where backups are written (default: next to the data)")
	noRestore := flag.Bool("no-restore", false, "backup: do not restore (when the disk cannot hold a second copy)")
	flag.Parse()

	et, err := dbal.ParseEngineType(*engine)
	if err != nil {
		log.Fatal(err)
	}
	dbal.SetSQLiteBusyTimeout(*busy)
	wanted := scenarioOrder
	if *run != "all" {
		wanted = strings.Split(*run, ",")
		for _, s := range wanted {
			if !slices.Contains(scenarioOrder, s) {
				log.Fatalf("unknown scenario %q", s)
			}
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	b, err := open(ctx, et, *admin, *rows, *noteBytes, *reuse)
	if err != nil {
		log.Fatal(err)
	}
	defer b.close()
	b.duration = *duration
	b.container = *container
	b.backupDir = *backupDir
	b.noRestore = *noRestore
	stopWatch := b.watchWAL()
	defer stopWatch()
	b.results["meta"] = map[string]any{
		"engine": string(et), "rows": *rows, "note_bytes": *noteBytes, "duration": duration.String(),
		"busy_timeout": busy.String(), "server": b.serverVersion(ctx), "started": time.Now().UTC().Format(time.RFC3339),
	}

	for _, name := range wanted {
		if ctx.Err() != nil {
			break
		}
		if b.engine != dbal.EngineSQLite && slices.Contains([]string{"vacuum", "crash", "unsafe-storage"}, name) {
			continue
		}
		log.Printf("== %s", name)
		start := time.Now()
		res, err := b.scenario(ctx, name)
		if err != nil {
			log.Printf("%s failed: %v", name, err)
			res = map[string]any{"error": err.Error()}
		}
		res["elapsed_s"] = round(time.Since(start).Seconds(), 1)
		if b.engine == dbal.EngineSQLite {
			res["max_wal_mb"] = mb(b.maxWAL.Swap(0))
		}
		b.results[name] = res
		line, _ := json.Marshal(res)
		log.Printf("%s: %s", name, line)
		if *out != "" {
			if err := writeJSON(*out, b.results); err != nil {
				log.Fatal(err)
			}
		}
	}
}

func (b *bench) scenario(ctx context.Context, name string) (map[string]any, error) {
	switch name {
	case "fingerprint":
		return b.fingerprints(ctx)
	case "editors":
		return b.editorsScenario(ctx)
	case "combined":
		return b.combined(ctx)
	case "find":
		return b.finds(ctx)
	case "import":
		return b.importThroughput(ctx)
	case "typechange":
		return b.typeChange(ctx)
	case "storage":
		return b.storage(ctx)
	case "backup":
		return b.backupRestore(ctx)
	case "vacuum":
		return b.vacuumUnderLoad(ctx)
	case "crash":
		return b.crash(ctx)
	case "unsafe-storage":
		return unsafeStorage(), nil
	}
	return nil, fmt.Errorf("unknown scenario %q", name)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
