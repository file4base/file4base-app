-- File4Base System Catalog Migration 002 (PostgreSQL & MariaDB compatible)
--
-- Multi-predicate relationships. A relationship's first match criterion stays
-- in sys_relationships (left_column_id, operator, right_column_id); the
-- criteria after it are kept here, numbered from 1 in the order they are
-- matched in. A record is related when it meets every criterion.
--
-- Each side of a criterion keeps the field's name beside its id. The id is
-- what it is resolved by; the name is kept for sides that will have no local
-- field id (an occurrence of another solution's table).
--
-- The server applies this itself (schema.EnsureSystemTables), with DATETIME(6)
-- in place of TIMESTAMPTZ on MariaDB.

ALTER TABLE sys_relationships ADD COLUMN IF NOT EXISTS left_column_name VARCHAR(64);
ALTER TABLE sys_relationships ADD COLUMN IF NOT EXISTS right_column_name VARCHAR(64);

CREATE TABLE IF NOT EXISTS sys_relationship_predicates (
    id VARCHAR(36) PRIMARY KEY,
    relationship_id VARCHAR(36) NOT NULL REFERENCES sys_relationships(id) ON DELETE CASCADE,
    sequence_idx INT NOT NULL,
    left_column_id VARCHAR(36) NOT NULL REFERENCES sys_columns(id) ON DELETE CASCADE,
    left_column_name VARCHAR(64),
    operator VARCHAR(8) NOT NULL DEFAULT '=',
    right_column_id VARCHAR(36) NOT NULL REFERENCES sys_columns(id) ON DELETE CASCADE,
    right_column_name VARCHAR(64),
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_relationship_predicate UNIQUE (relationship_id, sequence_idx)
);
