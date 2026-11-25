package data

import (
	"context"
	"errors"
	"fmt"
)

// What a write does to the records of a table, as a privilege names it.
const (
	ActionCreate = "create"
	ActionEdit   = "edit"
	ActionDelete = "delete"
)

// ErrNoPermission is returned when the account may read a table but may not
// do what is asked with its records. Every PermissionError wraps it.
var ErrNoPermission = errors.New("not permitted")

// PermissionError says which account may not do what with the records of
// which table. External is set when the table is another solution's, reached
// through an external occurrence (#107): the account is then the one the
// session group holds there, whose privileges are that solution's.
type PermissionError struct {
	Account  string
	Table    string
	Action   string
	External *ExternalTarget
}

func (e *PermissionError) Error() string {
	what := "modify records of"
	switch e.Action {
	case ActionCreate:
		what = "create records in"
	case ActionDelete:
		what = "delete records of"
	}
	account := "this account"
	if e.Account != "" {
		account = fmt.Sprintf("the account %q", e.Account)
	}
	t := e.External
	if t == nil {
		return fmt.Sprintf("%s: %s may not %s the table %q", ErrNoPermission, account, what, e.Table)
	}
	return fmt.Sprintf("%s: %q is an occurrence of the table %q of the solution %s, through the file reference %q, "+
		"and %s signed in to %s may not %s it; ask an administrator of %s for that privilege, "+
		"or sign in to %s with another account",
		ErrNoPermission, t.Occurrence, e.Table, t.Database, t.Reference,
		account, t.Database, what, t.Database, t.Database)
}

func (e *PermissionError) Unwrap() error { return ErrNoPermission }

// WithDatabase names the solution the service's driver is bound to. A
// cascading delete that crosses solutions tells their records apart by it,
// and recognizes the way back into this one when two solutions read each
// other (#107).
func (s *Service) WithDatabase(name string) *Service {
	s.database = name
	return s
}

// WithWriteAccess makes the service ask, before it deletes or creates the
// records of another solution on behalf of a request of this one, and before
// it creates a related record in any table, whether the caller may (#107):
// canWrite answers nil, or a *PermissionError. Without it every write is
// allowed, which is what an administrator and the server's own work get. The
// other writes to a table of this solution are authorized by the API before
// the service is called, as before.
func (s *Service) WithWriteAccess(canWrite func(ctx context.Context, tableName, action string) error) *Service {
	s.canWrite = canWrite
	return s
}

// checkWrite is nil when the caller may do action with the records of
// tableName.
func (s *Service) checkWrite(ctx context.Context, tableName, action string) error {
	if s.canWrite == nil {
		return nil
	}
	return s.canWrite(ctx, tableName, action)
}

type changeObserverKey struct{}

// WithChangeObserver has observe told of every table a request's work
// changes besides the one it names: the records a cascading delete removed,
// a related record created, and the records written in another solution
// (#107), with the solution each table is in (empty for the request's own,
// when the service was not told its name). The realtime notifications are
// published from it.
func WithChangeObserver(ctx context.Context, observe func(database, table string)) context.Context {
	return context.WithValue(ctx, changeObserverKey{}, observe)
}

// observeChange tells the request's observer, if any, that a table changed.
func observeChange(ctx context.Context, database, table string) {
	if observe, ok := ctx.Value(changeObserverKey{}).(func(database, table string)); ok && observe != nil {
		observe(database, table)
	}
}
