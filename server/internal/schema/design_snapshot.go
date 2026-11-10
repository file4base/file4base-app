package schema

import (
	"context"
	"fmt"
)

// #97 — undoing a migration that failed after its schema had already been
// merged into an existing database.
//
// A migration into the session's own database imports the converted design
// first and the records second. When the records could not be imported, only a
// newly created target database was dropped; merging into an existing database
// left the imported tables, layouts, scripts and accounts behind, although the
// documented contract promises full rollback — and a retry then built on that
// half-applied design.
//
// A snapshot taken before the merge says which design objects the database
// held and carries the bundle that describes them, which is enough to put the
// design back: whatever the merge added is removed, and whatever it changed is
// restored from the bundle.

// DesignSnapshot is the design of a database as it stood at one moment: the
// ids of every design object, and the solution bundle describing them.
type DesignSnapshot struct {
	SolutionName  string
	Bundle        []byte
	tableIDs      map[string]bool
	occurrenceIDs map[string]bool
	relationships map[string]bool
	layoutIDs     map[string]bool
	scriptIDs     map[string]bool
	valueListIDs  map[string]bool
	userIDs       map[string]bool
}

// SnapshotDesign records the design of the active database so that
// RestoreDesign can put it back. It reads only; nothing is written.
func (s *Service) SnapshotDesign(ctx context.Context, solutionName string) (*DesignSnapshot, error) {
	bundle, err := s.ExportSolution(ctx, ExportOptions{SolutionName: solutionName})
	if err != nil {
		return nil, fmt.Errorf("failed reading the current design: %w", err)
	}
	snap := &DesignSnapshot{
		SolutionName:  solutionName,
		Bundle:        bundle,
		tableIDs:      map[string]bool{},
		occurrenceIDs: map[string]bool{},
		relationships: map[string]bool{},
		layoutIDs:     map[string]bool{},
		scriptIDs:     map[string]bool{},
		valueListIDs:  map[string]bool{},
		userIDs:       map[string]bool{},
	}

	tables, err := s.ListTables(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range tables {
		snap.tableIDs[t.ID] = true
	}
	occurrences, err := s.ListTableOccurrences(ctx)
	if err != nil {
		return nil, err
	}
	for _, o := range occurrences {
		snap.occurrenceIDs[o.ID] = true
	}
	relationships, err := s.ListRelationships(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range relationships {
		snap.relationships[r.ID] = true
	}
	layouts, err := s.ListLayouts(ctx)
	if err != nil {
		return nil, err
	}
	for _, l := range layouts {
		snap.layoutIDs[l.ID] = true
	}
	scripts, err := s.ListScripts(ctx)
	if err != nil {
		return nil, err
	}
	for _, sc := range scripts {
		snap.scriptIDs[sc.ID] = true
	}
	valueLists, err := s.ListValueLists(ctx)
	if err != nil {
		return nil, err
	}
	for _, v := range valueLists {
		snap.valueListIDs[v.ID] = true
	}
	users, err := s.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		snap.userIDs[u.ID] = true
	}
	return snap, nil
}

// RestoreDesign puts the design back as the snapshot found it: every design
// object the snapshot does not know is deleted, in dependency order, and the
// snapshot's own bundle is then re-imported so that a definition the failed
// merge changed — a layout's objects, a script's steps — is the one it was.
//
// Records are not restored: a migration imports its records after its schema,
// in one transaction of its own, so a failure there has written none. The
// first error is returned, and the rest of the restore is still attempted, so
// one object that will not delete does not leave everything else behind.
func (s *Service) RestoreDesign(ctx context.Context, snap *DesignSnapshot) error {
	if snap == nil {
		return nil
	}
	var firstErr error
	fail := func(what string, err error) {
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed restoring the design (%s): %w", what, err)
		}
	}

	// Relationships and layouts first: an occurrence or a table cannot go
	// while something still points at it.
	if relationships, err := s.ListRelationships(ctx); err != nil {
		fail("relationships", err)
	} else {
		for _, r := range relationships {
			if !snap.relationships[r.ID] {
				fail("relationship "+r.Name, s.DeleteRelationship(ctx, r.ID))
			}
		}
	}
	if layouts, err := s.ListLayouts(ctx); err != nil {
		fail("layouts", err)
	} else {
		for _, l := range layouts {
			if !snap.layoutIDs[l.ID] {
				fail("layout "+l.Name, s.DeleteLayout(ctx, l.ID))
			}
		}
	}
	if occurrences, err := s.ListTableOccurrences(ctx); err != nil {
		fail("table occurrences", err)
	} else {
		for _, o := range occurrences {
			if !snap.occurrenceIDs[o.ID] {
				fail("table occurrence "+o.Name, s.DeleteTableOccurrence(ctx, o.ID))
			}
		}
	}
	if tables, err := s.ListTables(ctx); err != nil {
		fail("tables", err)
	} else {
		for _, t := range tables {
			if !snap.tableIDs[t.ID] {
				fail("table "+t.Name, s.DeleteTable(ctx, t.ID))
			}
		}
	}
	if scripts, err := s.ListScripts(ctx); err != nil {
		fail("scripts", err)
	} else {
		for _, sc := range scripts {
			if !snap.scriptIDs[sc.ID] {
				fail("script "+sc.Name, s.DeleteScript(ctx, sc.ID))
			}
		}
	}
	if valueLists, err := s.ListValueLists(ctx); err != nil {
		fail("value lists", err)
	} else {
		for _, v := range valueLists {
			if !snap.valueListIDs[v.ID] {
				fail("value list "+v.Name, s.DeleteValueList(ctx, v.ID))
			}
		}
	}
	if users, err := s.ListUsers(ctx); err != nil {
		fail("accounts", err)
	} else {
		for _, u := range users {
			if !snap.userIDs[u.ID] {
				fail("account "+u.Username, s.DeleteUser(ctx, u.ID))
			}
		}
	}

	// What the merge changed rather than added is put back from the bundle,
	// which matches its objects by the ids the snapshot recorded.
	if len(snap.Bundle) > 0 {
		if _, err := s.ImportSolution(ctx, snap.Bundle); err != nil {
			fail("the recorded design", err)
		}
	}
	return firstErr
}
