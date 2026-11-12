# MSSQL Backup Support — Design

## Intent

`database: mssql` exists in monodb-backup today but is a half-finished path: it was
added for a Windows customer, ported to Linux, and never brought up to the standard
the MongoDB backend reached in v3.7.0. The goal is a Microsoft SQL Server backend an
operator can schedule under cron and trust the same way as the PostgreSQL, MySQL and
MongoDB ones: it backs up every user database, honours `archivePass`, rotation,
`removeLocal`, retry and every upload target, and it fails loudly instead of reporting
an empty success.

## What exists today (master @ f3e2ef6)

- `backup/mssql_linux.go` and `backup/mssql_windows.go` duplicate the same three
  functions (`InitializeMSSQL`, `getMSSQLList`, `dumpMSSQLDB`) using
  `github.com/denisenkom/go-mssqldb` v0.12.3 and native `BACKUP DATABASE ... TO DISK`.
- `backup.go` already routes `mssql` in `getDBList` and `dumpDB`, and keeps MSSQL out
  of the streamable path. `main.go` calls `InitializeMSSQL()` once at startup.

Defects found while reading it:

| # | Defect | Effect |
|---|--------|--------|
| 1 | `Backup()` calls `mssqlDB.Close()` at the end of every run | In cron mode every run after the first fails; `retry: true` always fails |
| 2 | `getMSSQLList` swallows errors and returns an empty list; `sql.Open` never connects | Wrong password or unreachable server gives a "successful" run that backed up nothing |
| 3 | Linux statement has no `INIT`/`FORMAT` | With rotation, `db-Mon.bak` gets a new backup set appended every week and grows forever |
| 4 | No `COPY_ONLY` | A full backup taken by monodb-backup resets the differential base of the customer's own backup jobs |
| 5 | Database name and path are concatenated into T-SQL | A database named with `]` or `'` breaks the statement (injection) |
| 6 | `archivePass` is logged as "Encrypted: true" but ignored | Unencrypted `.bak` files go to S3/SFTP while the operator believes they are encrypted |
| 7 | `WITH COMPRESSION` is unconditional on Linux | Every backup fails on Express edition |
| 8 | Connection string built with `fmt.Sprintf` | A password containing `;` breaks the connection |
| 9 | `master.dbo.sysdatabases WHERE dbid > 4` | Includes offline databases and snapshots, which cannot be backed up |
| 10 | Directories created as the monodb-backup user with mode 0770 | On Linux the `mssql` service account cannot write into new rotation folders |

## Goals

- Fix defects 1–10.
- One platform-neutral `backup/mssql.go`; only the "let SQL Server write here" step stays
  per-platform.
- No new config keys: reuse `remote`, `archivePass`, `rotation`, `removeLocal`,
  `ctxCancel`, `exclude`, `databases`.
- An env-gated integration test against a real SQL Server 2022 container, including a
  restore of the produced `.bak`.

## Non-goals

- Streaming to S3 (`BACKUP TO URL` needs SQL Server 2022+ and S3 credentials on the
  server side). MSSQL stays on the dump-then-upload path.
- Backups from a monodb-backup host that is not the SQL Server host (shared path / SMB
  mapping). See constraints.
- Differential and transaction log backups, point-in-time restore.
- Swapping to `github.com/microsoft/go-mssqldb`. The current driver is archived upstream
  but works; changing it is a separate dependency change.
- Windows release artifacts. `.goreleaser.yaml` builds linux/amd64 only; Windows must
  keep compiling and nothing more.

## Anti-goals

- A run that reports success while a database was not backed up.
- A plaintext `.bak` left on disk or uploaded when `archivePass` is set.
- Touching the customer's existing backup chain.

## Constraints

- `BACKUP DATABASE ... TO DISK` writes the file on the **SQL Server host's** file
  system. monodb-backup therefore must run on the SQL Server host. Two guards:
  at startup `remote.host` (the part before any `\instance`) must resolve to an address
  of this machine (loopback or a local interface), otherwise alarm and exit, so no
  plaintext `.bak` is ever written to a remote disk; after each backup the file must be
  visible locally and non-empty, otherwise that database fails with a message saying
  monodb-backup must run on the SQL Server host (catches container/mount mismatches).
- The SQL Server service account (Linux: `mssql`, Windows default instance:
  `NT SERVICE\MSSQLSERVER`) needs write access to the folders monodb-backup creates, and
  monodb-backup needs read/delete access to the `.bak` SQL Server writes (7z, upload,
  `removeLocal`). Supported Linux run modes: as **root** (reads anything; chowns created
  folders, see Decisions) or as the **service account itself** (owns everything). Any
  other user is unsupported and documented as such.
- `backupDestination` must exist at startup for MSSQL, owned by the service account.
- `remote.isRemote: true` stays required for MSSQL (unchanged). `remote.user` and
  `remote.password` are a SQL login; on Windows an empty `remote.user` uses the driver's
  integrated (SSPI) authentication.

## Approaches considered

1. **Native `BACKUP DATABASE ... TO DISK` (chosen).** Consistent, restorable with plain
   `RESTORE`, already partly implemented, no extra binaries. Cost: co-location.
2. **`sqlpackage` export to `.bacpac`.** Works remotely, but it is a logical export that
   is not transactionally consistent on a live database, is slow on large databases, and
   adds a .NET dependency.
3. **`BACKUP TO URL` straight to S3.** Streams without local disk, but only SQL Server
   2022+, needs the S3 secret stored inside SQL Server, and does not cover SFTP/rsync.

## Decisions

- Statement: `BACKUP DATABASE @db TO DISK = @path WITH COPY_ONLY, FORMAT, INIT, CHECKSUM`
  plus `, COMPRESSION` unless `SERVERPROPERTY('EngineEdition') = 4` (Express). Name and
  path are bound parameters; the documented syntax accepts `@database_name_var` and
  `@physical_device_name_var`
  ([BACKUP (Transact-SQL)](https://learn.microsoft.com/en-us/sql/t-sql/statements/backup-transact-sql?view=sql-server-ver17)).
- Database list: `sys.databases` with `database_id > 4`, `state_desc = 'ONLINE'`,
  `source_database_id IS NULL`, ordered by name.
- Connection: `sqlserver://` URL built with `net/url` (`database=master`,
  `app name=monodb-backup`, `host\instance` mapped to the URL path); driver's default TLS
  behaviour, as on Linux today. The Windows-only `encrypt=disable;
  trustServerCertificate=true;trusted_connection=yes` string goes away.
  `InitializeMSSQL` pings with a 30 s timeout; failure sends an alarm and exits, the same
  as the MongoDB list failure.
- The connection pool lives for the whole process; `Backup()` no longer closes it.
- Each backup runs under `context.WithTimeout(ctxCancel hours)`; the driver sends a TDS
  attention on cancel, which aborts the server-side `BACKUP`. On any failure after the
  statement is issued, the target `.bak` is removed best-effort so no partial file stays.
- Encryption: after `BACKUP`, `7z a -t7z -mhe=on -mx0 -p<archivePass> <file>.bak.7z
  <file>.bak` (the `.bak` is already compressed), then the `.bak` is removed. On failure
  both files are removed and the database is marked failed. Same `-p` argument style as
  the MySQL and MongoDB backends.
- Server write access (Unix): at startup, when running as root, record the uid/gid of
  `backupDestination`. Before each backup, chown every directory from
  `backupDestination` (inclusive, because `removeLocal` with rotation disabled deletes and
  later recreates it) down to the backup folder to that uid/gid. Not root → no-op. The
  operator chowns `backupDestination` to `mssql` once.
- Server write access (Windows): the existing ACL grant to `NT SERVICE\MSSQLSERVER` stays,
  returning an error instead of `logger.Fatal`.
- `backupAsTables` is ignored for MSSQL, the same way it is for MongoDB (today it would
  route MSSQL into the MySQL-only `dumpDBWithTables`).
- Output names: `<db>-<rotation>.bak` or `<db>-<rotation>.bak.7z`, paths from the
  existing `dumpName`/`nameWithPath`.
- Shared `Backup()` path code is untouched: the Windows `dst` layout (no separator before
  the database name) and `removeLocal` deleting the whole destination when rotation is
  disabled are pre-existing behaviours of every backend and stay out of scope.

## Left to the builder

- Log wording, helper names not listed in the plan, test helper structure.
