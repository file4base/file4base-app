-- The API creates and drops one database per solution, so its account needs
-- server-level privileges. The MariaDB image only grants MARIADB_USER rights
-- on MARIADB_DATABASE, so they are widened here.
GRANT ALL PRIVILEGES ON *.* TO 'file4base'@'%' WITH GRANT OPTION;
FLUSH PRIVILEGES;
