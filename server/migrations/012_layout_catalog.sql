-- File4Base System Catalog Migration 012 (PostgreSQL & MariaDB)
--
-- The layout and script menus in FileMaker's order, with their folders and
-- separators, and FileMaker's type of a migrated field where File4Base's
-- own does not say it (#208): the FileMaker Data API lists the menus, and
-- describes a layout's fields, as FileMaker Server does.
--
--   sys_catalog_items                 a folder or a separator of the layout
--                                     menu (catalog 'layout') or the script
--                                     menu ('script'); a layout menu
--                                     separator also has an occurrence, as
--                                     FileMaker gives it one.
--   sys_layouts.folder_id,            where a layout is in its menu: its
--   sys_layouts.position              folder (a folder of sys_catalog_items,
--                                     NULL at the top) and its position in
--                                     the whole menu, from 1. NULL for a
--                                     layout created here, listed after the
--                                     others, by name.
--   sys_scripts.position              the same for a script, whose
--                                     folder_id is a folder of
--                                     sys_catalog_items.
--   sys_columns.source_type           FileMaker's "type/result" of a
--                                     migrated field File4Base keeps as
--                                     another type: normal/time,
--                                     calculation/time (both kept as
--                                     timestamps) and summary/<result>
--                                     (kept as a plain column). NULL
--                                     otherwise.
--
-- Every new column is nullable, and a catalog without menus lists its
-- layouts and scripts by name, as before.
--
-- The server applies this itself (schema.EnsureSystemTables, and the first
-- time a menu is read on an older database).

ALTER TABLE sys_columns ADD COLUMN IF NOT EXISTS source_type VARCHAR(32);
ALTER TABLE sys_layouts ADD COLUMN IF NOT EXISTS folder_id VARCHAR(36);
ALTER TABLE sys_layouts ADD COLUMN IF NOT EXISTS position INT;
ALTER TABLE sys_scripts ADD COLUMN IF NOT EXISTS position INT;

CREATE TABLE IF NOT EXISTS sys_catalog_items (
    id VARCHAR(36) PRIMARY KEY,
    catalog VARCHAR(16) NOT NULL,
    kind VARCHAR(16) NOT NULL,
    name VARCHAR(128) NOT NULL,
    folder_id VARCHAR(36),
    position INT,
    table_occurrence_id VARCHAR(36),
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- MariaDB: created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP.
