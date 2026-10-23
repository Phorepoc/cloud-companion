# Choosing the database

phecloud keeps its state (devices, pairing codes, the owner's passkeys, connected accounts, SMS, merges) in one database. The default is a
**SQLite file** in the data volume (`/data/phecloud.db`): nothing to run, nothing to configure. For a bigger or shared setup use
**PostgreSQL** or **MariaDB / MySQL**. `PHECLOUD_DB` says which:

| `PHECLOUD_DB` | database |
|---|---|
| `/data/phecloud.db` (default) or any file path | SQLite |
| `postgres://user:password@host:5432/dbname?sslmode=disable` | PostgreSQL 13 or newer (tested with 16) |
| `mysql://user:password@host:3306/dbname` (also `mariadb://`) | MariaDB 10.6 or newer / MySQL 8 (tested with MariaDB 11) |

The database and user must exist (phecloud creates its tables, not the database). Use the `utf8mb4` character set; for MariaDB / MySQL pick a
case-sensitive collation (`utf8mb4_bin`), as ids and codes are case-sensitive. phecloud waits up to a minute for the database to come up, so
a database container that starts at the same time is fine.

## With Docker Compose
```
export PHECLOUD_DB_PASSWORD=<a long random password>
# PostgreSQL
export PHECLOUD_DB='postgres://phecloud:'$PHECLOUD_DB_PASSWORD'@postgres:5432/phecloud?sslmode=disable'
docker compose -f docker-compose.yml -f docker-compose.db.yml --profile db-postgres up -d
# or MariaDB
export PHECLOUD_DB='mysql://phecloud:'$PHECLOUD_DB_PASSWORD'@mariadb:3306/phecloud'
docker compose -f docker-compose.yml -f docker-compose.db.yml --profile db-mariadb up -d
```
`docker-compose.db.yml` defines the two database services (profiles `db-postgres`, `db-mariadb`; containers `phe-postgres`, `phe-mariadb`; capped;
not published outside the compose network; data in their own volumes).

## Schema and upgrades
The schema is created and upgraded when phecloud starts: **versioned migrations**, one list per database (`internal/store/migrate.go`), recorded in
`schema_migrations`. An existing SQLite file from an older version is upgraded in place (version 1 is `IF NOT EXISTS`; the columns it adds are
tolerated if present). The application's SQL is written once and rewritten per database just before it runs (placeholders, upserts, `INSERT OR IGNORE`,
`GROUP_CONCAT`, quoting of `key` for MySQL), so features behave the same everywhere.

## Backup and restore
- **SQLite**: `phecloud hub backup <dir>` makes a consistent copy (`phecloud.db`, with the certificate, the connector key file and the mail hub's volumes) and
  `phecloud hub restore` puts it back (docs/hub.md, Backup and restore).
- **PostgreSQL / MariaDB**: the backup command saves the certificate, the connector key file and the Stalwart volumes but **not** the database; back it up
  with the database's own tools and keep it with the backup:
  ```
  docker exec phe-postgres pg_dump -U phecloud -Fc phecloud > phecloud.pgdump          # restore: pg_restore -U phecloud -d phecloud --clean phecloud.pgdump
  docker exec phe-mariadb mariadb-dump -uphecloud -p"$PHECLOUD_DB_PASSWORD" --single-transaction phecloud > phecloud.sql   # restore: mariadb ... < phecloud.sql
  ```
- Either way, keep `connectors.key` (next to the certificate in `/data`) with the database: the accounts' passwords are sealed with it.

## Moving from SQLite to another database
There is no automatic copy. Pair the devices again on the new database (devices, passkeys and accounts are small), or ask for the one-off export tool
if you have many devices. Do this on a spare instance first: the running one keeps its SQLite file untouched until you change `PHECLOUD_DB`.

## Tests
`sh web/test/db_test.sh` runs the whole Go suite against SQLite, PostgreSQL 16 and MariaDB 11 in throwaway containers (`phe-pg-t`, `phe-maria-t`; every test gets
its own database); `sh web/test/dbstack.sh postgres|mariadb` runs the real app on the database and the pairing, SMS and passkey browser tests.

## Stalwart (the mail hub) on the same database server — not done here
Stalwart can keep its data in PostgreSQL or MySQL/MariaDB instead of its own embedded store (its data store, blob store, full-text index and in-memory
store each take a backend). That would let phecloud and the hub live in one external database server and one backup. This repository does **not**
set it up and the hub tests do not cover it: follow Stalwart's own documentation for the store configuration and test it on a spare hub first.
