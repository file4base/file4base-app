package transfer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// ErrNotAuthorized is returned when an account may not do the work one end of
// a transfer asks of it. Nothing has been read or written when it is.
var ErrNotAuthorized = errors.New("not authorized")

// Account is the identity one end of a transfer is checked for: the session
// that made a request, or the account a schedule runs as.
type Account struct {
	ID   string
	Role string
}

// CheckEnd verifies that an account may do one end's work: it holds the
// capability its role needs (bulk_export to read records out, bulk_import to
// write them in) and it reaches the table — read access for the source,
// read-write access for the destination.
//
// It is the one rule both ways of running a transfer follow: a request
// checks the sessions it was given, and a scheduled run checks the accounts
// it runs as, every time (#96, #108).
func CheckEnd(
	ctx context.Context, driver dbal.DatabaseDriver, account Account,
	database, capability, tableName string, writing bool,
) error {
	svc := schema.NewService(driver)

	allowed, err := svc.RoleAllows(ctx, account.Role, capability)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("%w: the account on %s does not hold the %s privilege",
			ErrNotAuthorized, describeEnd(database), capability)
	}

	access, err := svc.TableAccess(ctx, account.ID, tableName)
	if err != nil {
		return err
	}
	switch {
	case access == schema.AccessNone:
		return fmt.Errorf("%w: the account on %s cannot reach the table %s",
			ErrNotAuthorized, describeEnd(database), tableName)
	case writing && access != schema.AccessReadWrite:
		return fmt.Errorf("%w: the account on %s has read-only access to the table %s, which a transfer writes to",
			ErrNotAuthorized, describeEnd(database), tableName)
	}
	return nil
}

// ResolveAccount finds the account a schedule runs as, by username, and
// refuses one that is gone or disabled. A disabled account cannot sign in,
// and a schedule is not a way around that.
func ResolveAccount(ctx context.Context, driver dbal.DatabaseDriver, database, username string) (Account, error) {
	users, err := schema.NewService(driver).ListUsers(ctx)
	if err != nil {
		return Account{}, err
	}
	for _, u := range users {
		if !strings.EqualFold(u.Username, username) {
			continue
		}
		if !u.IsActive {
			return Account{}, fmt.Errorf("%w: the account %s on %s is disabled",
				ErrNotAuthorized, username, describeEnd(database))
		}
		return Account{ID: u.ID, Role: u.Role}, nil
	}
	return Account{}, fmt.Errorf("%w: %s has no account %s",
		ErrNotAuthorized, describeEnd(database), username)
}

func describeEnd(database string) string {
	if strings.TrimSpace(database) == "" {
		return "this solution"
	}
	return "the solution " + database
}
