package dbal

import "fmt"

// CreationOrderColumn is the column every user table numbers its records
// in, in the order they are created: what FileMaker's record IDs give, and
// the order it lists records in without a sort — a found set, and the
// records of a relationship that does not sort. A record's id is a random
// UUID, which says nothing of when it was created.
//
// It is the engine's own sequence (an identity on PostgreSQL, AUTO_INCREMENT
// on MariaDB), never a field: it is not in the catalog, no client reads or
// writes it, and "$" is in no field name (see CheckIdentifier), so no field
// can take its name.
const CreationOrderColumn = "f4b$creation_order"

// CreationOrderType is the definition of the creation order column, unique
// so that ordering by it is served by an index. A table that is given the
// column numbers the records it already has in the order the engine reads
// them: their physical order on PostgreSQL, close to the order they were
// inserted in, and their primary key's on MariaDB, whose InnoDB tables are
// stored in key order.
func CreationOrderType(engine EngineType) string {
	if engine == EngineMariaDB {
		return "BIGINT NOT NULL AUTO_INCREMENT UNIQUE"
	}
	return "BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE"
}

// AddCreationOrderSQL gives an existing table the creation order column. On
// MariaDB, a lockWait of some seconds bounds how long the statement waits for
// a table another session holds (ALTER TABLE ... WAIT); PostgreSQL bounds it
// with the transaction's lock_timeout instead.
func AddCreationOrderSQL(dialect Dialect, tableName string, lockWait int) string {
	wait := ""
	if dialect.Engine() == EngineMariaDB && lockWait > 0 {
		wait = fmt.Sprintf(" WAIT %d", lockWait)
	}
	return fmt.Sprintf("ALTER TABLE %s%s ADD COLUMN %s %s", dialect.QuoteIdentifier(tableName), wait,
		dialect.QuoteIdentifier(CreationOrderColumn), CreationOrderType(dialect.Engine()))
}
