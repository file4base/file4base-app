package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/file4base/file4base-app/server/internal/containermeta"
	"github.com/file4base/file4base-app/server/internal/sourcetext"
)

// A cascading delete (#36), across solutions (#107).
//
// Deleting a record deletes the records related to it through every
// relationship of its table whose "Delete related records" option is on,
// down the whole chain, each solution following its own relationships: a
// record of another solution reached through an external occurrence is
// deleted with the cascades of that solution's graph.
//
// It is done in two steps, so that a delete that cannot be completed changes
// nothing:
//
//  1. Plan. Every record the delete reaches is found, solution by solution,
//     with the session the group holds in each, and every refusal is
//     decided: a solution not signed in to, a table that account may not
//     read, or may not delete from, refuses the whole delete.
//  2. Execute. The records of each other solution are deleted in one
//     transaction of that solution, the solution deepest in the chain first;
//     then those of this solution, the record itself among them, in one
//     transaction of this one.
//
// No statement joins two solutions, and there is no two-phase commit: when
// this solution's transaction fails after another solution's committed, the
// delete fails with a *PartialDeleteError, leaving this solution's records
// without the related records already deleted elsewhere — what deleting the
// children first always meant.

// ErrPartialDelete is returned when a cascading delete committed in another
// solution and then failed. Every PartialDeleteError wraps it.
var ErrPartialDelete = errors.New("the delete was only partly done")

// PartialDeleteError says which solutions a cascading delete already deleted
// related records in when it failed in Database.
type PartialDeleteError struct {
	// Committed are the solutions whose records are deleted, in the order
	// they were.
	Committed []string
	// Database is the solution whose records could not be deleted.
	Database string
	cause    error
}

func (e *PartialDeleteError) Error() string {
	return fmt.Sprintf("%s: the related records in %s are deleted, but the records of %s could not be: %v; "+
		"they are still there, without those related records: delete them again once the cause is fixed",
		ErrPartialDelete, strings.Join(e.Committed, ", "), e.Database, e.cause)
}

func (e *PartialDeleteError) Unwrap() []error { return []error{ErrPartialDelete, e.cause} }

// cascadeBatch is the most records one statement of a cascade names, so a
// statement stays well under the engines' parameter limits.
const cascadeBatch = 500

// deletePlan is everything one delete removes, found before anything is.
type deletePlan struct {
	root  *Service
	table string
	id    string

	// solutions are the solutions the delete reaches, by name, in the order
	// they are met: the root's own first.
	solutions map[string]*plannedSolution
	order     []string
	seen      map[string]struct{}
}

// plannedSolution is what a delete removes from one solution, and the
// service it is removed with.
type plannedSolution struct {
	name string
	svc  *Service
	// via is the external occurrence the solution was first reached
	// through, which names it in a refusal; nil for the root's own.
	via    *ExternalTarget
	tables []string
	ids    map[string][]string
}

// recordKey identifies a record across solutions, so a cascade that points
// back at a record already on its way out, in this solution or another,
// stops there.
func recordKey(database, table, id string) string {
	return database + "\x00" + table + "\x00" + id
}

func (p *deletePlan) solution(name string, svc *Service, via *ExternalTarget) *plannedSolution {
	if sol, ok := p.solutions[name]; ok {
		return sol
	}
	sol := &plannedSolution{name: name, svc: svc, via: via, ids: map[string][]string{}}
	p.solutions[name] = sol
	p.order = append(p.order, name)
	return sol
}

// add puts a record in the plan, and reports whether it was not there yet.
func (p *deletePlan) add(sol *plannedSolution, table, id string) bool {
	key := recordKey(sol.name, table, id)
	if _, ok := p.seen[key]; ok {
		return false
	}
	p.seen[key] = struct{}{}
	if _, ok := sol.ids[table]; !ok {
		sol.tables = append(sol.tables, table)
	}
	sol.ids[table] = append(sol.ids[table], id)
	return true
}

// level is the records of one table of one solution whose related records
// are looked for next.
type level struct {
	sol   *plannedSolution
	table string
	ids   []string
}

// planDelete finds every record deleting id from tableName removes, and
// decides every refusal, without deleting anything.
func (s *Service) planDelete(ctx context.Context, tableName, id string) (*deletePlan, error) {
	if _, err := s.tableFields(ctx, tableName); err != nil {
		return nil, err
	}
	if err := s.recordExists(ctx, tableName, id); err != nil {
		return nil, err
	}
	p := &deletePlan{root: s, table: tableName, id: id, solutions: map[string]*plannedSolution{}, seen: map[string]struct{}{}}
	root := p.solution(s.database, s, nil)
	p.add(root, tableName, id)

	// Breadth first, a table at a time: the records of a level are read in a
	// fixed number of statements, however many there are (no N+1).
	frontier := []level{{sol: root, table: tableName, ids: []string{id}}}
	for len(frontier) > 0 {
		var next []level
		for _, lv := range frontier {
			found, err := p.children(ctx, lv)
			if err != nil {
				return nil, err
			}
			next = append(next, found...)
		}
		frontier = next
	}

	// A record of another solution is deleted as the account the group
	// holds there, which must be allowed to.
	for _, name := range p.order[1:] {
		sol := p.solutions[name]
		for _, table := range sol.tables {
			if err := sol.svc.checkWrite(ctx, table, ActionDelete); err != nil {
				if sol.via != nil {
					via := *sol.via
					via.Table = table
					return nil, via.Wrap(err)
				}
				return nil, err
			}
		}
	}
	return p, nil
}

// children follows every cascading relationship of a level's table, in the
// graph of the solution the table is in, and puts the records it reaches in
// the plan. It returns the new records, a level per table.
func (p *deletePlan) children(ctx context.Context, lv level) ([]level, error) {
	svc := lv.sol.svc
	relationships, err := svc.cascadingRelationships(ctx, lv.table)
	if err != nil {
		return nil, err
	}
	var out []level
	for _, relID := range relationships {
		rel, err := svc.Relationship(ctx, relID)
		if err != nil {
			return nil, err
		}
		q, err := svc.relatedQueryOf(ctx, rel, LocalOrigin(lv.table), rel.Right.OccurrenceID, QueryOptions{})
		if err != nil {
			// A relationship pointing at a table of this solution that is no
			// longer a user table must not make a record undeletable. One
			// into another solution that cannot be read refuses the delete:
			// its records cannot be known to be gone.
			if rel.Right.External == nil && (errors.Is(err, ErrTableNotFound) || errors.Is(err, ErrUnknownField)) {
				continue
			}
			return nil, err
		}
		ids, err := q.relatedIDs(ctx, lv.ids)
		if err != nil {
			return nil, wrapSide(q.target, err)
		}
		if len(ids) == 0 {
			continue
		}
		sol := lv.sol
		if ext := q.target.External; ext != nil {
			sol = p.solution(ext.Database, q.dst, ext)
		}
		var fresh []string
		for _, id := range ids {
			if p.add(sol, q.target.TableName, id) {
				fresh = append(fresh, id)
			}
		}
		if len(fresh) > 0 {
			out = append(out, level{sol: sol, table: q.target.TableName, ids: fresh})
		}
	}
	return out, nil
}

// relatedIDs are the ids of every record related to one of parents: all of
// them, not a page. The match values of the parents are read where they
// are, and the related records looked up where those are, in one statement
// per batch of parents and of match values.
func (q *relatedQuery) relatedIDs(ctx context.Context, parents []string) ([]string, error) {
	columns := sourceColumns(q.terms)
	dialect := q.dst.driver.Dialect()
	var out []string
	found := map[string]struct{}{}
	var conditions []string
	var args []interface{}
	flush := func() error {
		if len(conditions) == 0 {
			return nil
		}
		query := fmt.Sprintf("SELECT %s FROM %s WHERE %s",
			dialect.QuoteIdentifier("id"), dialect.QuoteIdentifier(q.target.TableName), strings.Join(conditions, " OR "))
		rows, err := q.dst.driver.DB().QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("failed reading the records of %s related to the records being deleted: %w", q.target.TableName, err)
		}
		defer rows.Close()
		for rows.Next() {
			var raw interface{}
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			id := fmt.Sprint(textOf(raw))
			if _, dup := found[id]; !dup {
				found[id] = struct{}{}
				out = append(out, id)
			}
		}
		conditions, args = nil, nil
		return rows.Err()
	}
	for start := 0; start < len(parents); start += cascadeBatch {
		batch := parents[start:min(start+cascadeBatch, len(parents))]
		values, err := q.src.matchValuesOf(ctx, q.source.TableName, columns, batch)
		if err != nil {
			return nil, wrapSide(q.source, err)
		}
		signatures := map[string]struct{}{}
		for _, id := range batch {
			record, ok := values[id]
			if !ok {
				continue
			}
			signature := matchSignature(q.terms, record)
			if _, done := signatures[signature]; done {
				continue
			}
			signatures[signature] = struct{}{}
			match, keys := matchCondition(dialect, q.terms, func(c string) interface{} { return record[c] }, len(args)+1)
			if match == "" {
				continue
			}
			conditions = append(conditions, "("+match+")")
			args = append(args, keys...)
			if len(args) >= cascadeBatch || len(conditions) >= cascadeBatch {
				if err := flush(); err != nil {
					return nil, err
				}
			}
		}
		if err := flush(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// recordExists is nil when the table has a record with that id.
func (s *Service) recordExists(ctx context.Context, tableName, id string) error {
	dialect := s.driver.Dialect()
	q := fmt.Sprintf("SELECT 1 FROM %s WHERE %s = %s",
		dialect.QuoteIdentifier(tableName), dialect.QuoteIdentifier("id"), dialect.Placeholder(1))
	var one int
	err := s.driver.DB().QueryRowContext(ctx, q, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrRecordNotFound, id)
	}
	if err != nil {
		return fmt.Errorf("failed reading %s: %w", tableName, err)
	}
	return nil
}

// execute deletes what the plan found: the other solutions' records first,
// the solution met last first, each in one transaction of its own, then this
// solution's in one transaction.
func (p *deletePlan) execute(ctx context.Context) error {
	var committed []string
	others := slices.Clone(p.order[1:])
	slices.Reverse(others)
	for _, name := range others {
		sol := p.solutions[name]
		if err := sol.deleteAll(ctx, "", ""); err != nil {
			if len(committed) > 0 {
				return &PartialDeleteError{Committed: committed, Database: name, cause: err}
			}
			if sol.via == nil {
				return err
			}
			classified := sol.via.Wrap(err)
			var denied *PermissionError
			if errors.Is(classified, ErrExternalUnavailable) || errors.As(classified, &denied) {
				return classified
			}
			return fmt.Errorf("the related records in %s, reached through the file reference %q, could not be deleted, "+
				"so nothing was: %w", name, sol.via.Reference, err)
		}
		committed = append(committed, name)
		for _, table := range sol.tables {
			observeChange(ctx, name, table)
		}
	}
	root := p.solutions[p.order[0]]
	if err := root.deleteAll(ctx, p.table, p.id); err != nil {
		if len(committed) > 0 {
			return &PartialDeleteError{Committed: committed, Database: root.name, cause: err}
		}
		return err
	}
	for _, table := range root.tables {
		if table != p.table {
			observeChange(ctx, root.name, table)
		}
	}
	return nil
}

// deleteAll deletes the planned records of one solution in one transaction.
// The record named by table and id must still be there; a related record
// another session deleted since the plan was made is simply gone already.
func (sol *plannedSolution) deleteAll(ctx context.Context, table, id string) error {
	db, dialect := sol.svc.driver.DB(), sol.svc.driver.Dialect()
	// A failed statement aborts a PostgreSQL transaction: the tables the
	// original texts and container metadata are kept in exist before it.
	if err := sourcetext.Ensure(ctx, db); err != nil {
		return err
	}
	if err := containermeta.Ensure(ctx, db); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// The record being deleted is deleted on its own, so that one another
	// session deleted meanwhile is reported rather than taken for done.
	if table != "" {
		res, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s = %s",
			dialect.QuoteIdentifier(table), dialect.QuoteIdentifier("id"), dialect.Placeholder(1)), id)
		if err != nil {
			return fmt.Errorf("failed deleting from %s: %w", table, err)
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			return fmt.Errorf("%w: %s", ErrRecordNotFound, id)
		}
	}
	for _, t := range sol.tables {
		ids := sol.ids[t]
		for start := 0; start < len(ids); start += cascadeBatch {
			batch := ids[start:min(start+cascadeBatch, len(ids))]
			if t == table {
				batch = slices.DeleteFunc(slices.Clone(batch), func(rid string) bool { return rid == id })
			}
			if len(batch) == 0 {
				continue
			}
			marks := make([]string, len(batch))
			args := make([]interface{}, len(batch))
			for i, rid := range batch {
				marks[i], args[i] = dialect.Placeholder(i+1), rid
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s IN (%s)",
				dialect.QuoteIdentifier(t), dialect.QuoteIdentifier("id"), strings.Join(marks, ", ")), args...); err != nil {
				return fmt.Errorf("failed deleting from %s: %w", t, err)
			}
		}
		if err := sourcetext.DeleteRecords(ctx, tx, dialect, t, ids); err != nil {
			return err
		}
		if err := containermeta.DeleteRecords(ctx, tx, dialect, t, ids); err != nil {
			return err
		}
	}
	return tx.Commit()
}
