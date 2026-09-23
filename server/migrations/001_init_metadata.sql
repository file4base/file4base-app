-- File4Base System Catalog Migration (PostgreSQL & MariaDB compatible)

CREATE TABLE IF NOT EXISTS sys_tables (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(64) NOT NULL UNIQUE,
    display_name VARCHAR(128) NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sys_columns (
    id VARCHAR(36) PRIMARY KEY,
    table_id VARCHAR(36) NOT NULL REFERENCES sys_tables(id) ON DELETE CASCADE,
    name VARCHAR(64) NOT NULL,
    display_name VARCHAR(128) NOT NULL,
    field_type VARCHAR(32) NOT NULL,
    is_nullable BOOLEAN DEFAULT TRUE,
    is_primary_key BOOLEAN DEFAULT FALSE,
    default_value TEXT,
    calculation_formula TEXT,
    validation_rules TEXT,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_table_column UNIQUE (table_id, name)
);

CREATE TABLE IF NOT EXISTS sys_table_occurrences (
    id VARCHAR(36) PRIMARY KEY,
    base_table_id VARCHAR(36) NOT NULL REFERENCES sys_tables(id) ON DELETE CASCADE,
    name VARCHAR(64) NOT NULL UNIQUE,
    x_pos DOUBLE PRECISION DEFAULT 100,
    y_pos DOUBLE PRECISION DEFAULT 100,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sys_relationships (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(128) NOT NULL,
    left_occurrence_id VARCHAR(36) NOT NULL REFERENCES sys_table_occurrences(id) ON DELETE CASCADE,
    left_column_id VARCHAR(36) NOT NULL REFERENCES sys_columns(id) ON DELETE CASCADE,
    right_occurrence_id VARCHAR(36) NOT NULL REFERENCES sys_table_occurrences(id) ON DELETE CASCADE,
    right_column_id VARCHAR(36) NOT NULL REFERENCES sys_columns(id) ON DELETE CASCADE,
    operator VARCHAR(8) DEFAULT '=',
    allow_creation BOOLEAN DEFAULT FALSE,
    cascade_delete BOOLEAN DEFAULT FALSE,
    sort_related TEXT,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sys_layouts (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(128) NOT NULL,
    table_occurrence_id VARCHAR(36) NOT NULL REFERENCES sys_table_occurrences(id) ON DELETE CASCADE,
    definition JSONB NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
