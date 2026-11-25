-- File4Base System Catalog Migration 007 (PostgreSQL & MariaDB)
--
-- Custom functions: a solution's own functions, as FileMaker defines them in
-- Manage Custom Functions, which every formula of the solution may call.
--
--   sys_custom_functions.name        the function's name, unique whatever
--                                    its case (the index below on
--                                    PostgreSQL, the case-insensitive
--                                    collation on MariaDB), and never the
--                                    name of a built-in function (checked
--                                    by the server).
--   sys_custom_functions.parameters  its parameter names, a JSON array of
--                                    text, in order.
--   sys_custom_functions.formula     its formula, which reads its
--                                    parameters and may call itself and the
--                                    solution's other custom functions.
--   sys_custom_functions.access      'all', or 'full_access' for a function
--                                    only full-access accounts see in the
--                                    design (FileMaker's "Availability").
--
-- A catalog without the table has no custom functions. The server applies
-- this itself (schema.EnsureSystemTables); the timestamp columns are
-- TIMESTAMPTZ on PostgreSQL and DATETIME(6) on MariaDB.

CREATE TABLE IF NOT EXISTS sys_custom_functions (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(128) NOT NULL UNIQUE,
    parameters TEXT NOT NULL,
    formula TEXT NOT NULL,
    access VARCHAR(16) NOT NULL DEFAULT 'all',
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- PostgreSQL only; MariaDB's UNIQUE name is already case-insensitive.
CREATE UNIQUE INDEX IF NOT EXISTS sys_custom_functions_name_ci ON sys_custom_functions (LOWER(name));
