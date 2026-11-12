# MSSQL Backup Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the existing half-finished `database: mssql` path into a backend that backs up every online user database with native `BACKUP DATABASE`, honours `archivePass`/rotation/retry/cron, and fails loudly.

**Architecture:** One platform-neutral `backup/mssql.go` owns connection, listing and the backup statement; per-platform `mssql_access_*.go` lets the SQL Server service account write into the folders monodb-backup creates. The `.bak` lands on local disk (SQL Server writes it on the same host), is optionally wrapped in 7z, then flows through the existing non-streamable `upload` path.

**Tech Stack:** Go 1.27, `database/sql` + `github.com/denisenkom/go-mssqldb` v0.12.3 (already in go.mod), `7z` CLI, SQL Server 2022 container for integration tests.

**Spec:** `docs/specs/2026-11-12-mssql-backup-design.md`

## Global Constraints

- No new config keys and no new Go module dependencies.
- `CGO_ENABLED=0 go build ./...` must pass for `GOOS=linux` and `GOOS=windows`.
- Backup statement options exactly: `COPY_ONLY, FORMAT, INIT, CHECKSUM`, plus `COMPRESSION` unless `SERVERPROPERTY('EngineEdition') = 4`.
- Database name and target path are bound parameters (`@db`, `@path`), never concatenated.
- Encrypted output is `<name>.bak.7z`, produced with `7z a -t7z -mhe=on -mx0 -p<archivePass>`; no plaintext `.bak` remains after success or failure.
- Shared `Backup()` path computation (`dst`, `removeLocal`) is not changed.
- Commit messages: English, imperative, one line ≤72 chars, no body.
- Code comments only for a non-obvious why; one line.
- `go test ./... -count=1` stays green without Docker or root; the integration test is skipped unless `MONODB_MSSQL_TEST_DSN` is set.

## Review Focus

1. Database name containing `]` or `'` (`we]ird'name`) → backed up and restorable, no T-SQL error. Pinned in Task 4 integration test.
2. Second `Backup()` call in the same process (cron mode, `retry: true`) → still connected and succeeds. Pinned in Task 4 integration test.
3. `remote.host` is another machine → startup alarm and exit before any `BACKUP` runs; file not visible locally after backup → that database fails, never counted as success. Pinned in Task 2 (`isLocalHost`) and Task 3 (`checkMSSQLBackupVisible`) unit tests.
4. `removeLocal: true` with rotation disabled deletes `backupDestination`; the next run recreates it as root → the service account still gets write access. Pinned in Task 1 root test.
5. Server unreachable or wrong password at startup → alarm and non-zero exit, not an empty successful run. Pinned in Task 4 verification step.

---

### Task 1: Server write access for created backup folders

**Review:** per-task — security (filesystem ownership and ACLs)

**Files:**
- Create: `backup/mssql_access.go` (shared helper)
- Create: `backup/mssql_access_unix.go` (`//go:build !windows`)
- Create: `backup/mssql_access_windows.go` (`//go:build windows`)
- Test: `backup/mssql_access_test.go`

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces:
  - `func dirsFrom(root, dir string) ([]string, error)` — cleaned paths from `root` (inclusive) down to `dir` (inclusive), outermost first; error when `dir` is not `root` or inside it.
  - `func initServerDirOwner(root string) error` — errors when `root` does not exist or is not a directory (message names `backupDestination` and says it must be owned by the SQL Server service account). Unix: when `os.Geteuid() == 0`, records `root`'s uid/gid in package state. Windows: existence check only.
  - `func grantServerAccess(root, dir string) error` — Unix: no-op unless an owner was recorded; otherwise `os.Chown` every entry of `dirsFrom(root, dir)`. Windows: ACL grant on `dir`.

- [ ] **Step 1: Write the failing tests**

```go
func TestDirsFrom(t *testing.T) {
	// "/var/backups",  "/var/backups/Daily/Mon" -> ["/var/backups", "/var/backups/Daily", "/var/backups/Daily/Mon"]
	// "/var/backups/", "/var/backups"           -> ["/var/backups"]
	// "/var/backups",  "/var/backupsX/a"        -> error (prefix trick)
	// "/var/backups",  "/var/backups/../etc"    -> error
}

func TestInitServerDirOwnerRequiresDestination(t *testing.T) {
	// missing := filepath.Join(t.TempDir(), "nope"); err != nil && strings.Contains(err.Error(), "backupDestination")
	// a regular file instead of a dir -> err != nil
}

func TestGrantServerAccessRestoresRecreatedDestination(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() != 0 { t.Skip("needs root on unix") }
	// root := filepath.Join(t.TempDir(), "dest"); os.Mkdir(root, 0o770); os.Chown(root, 4242, 4242)
	// initServerDirOwner(root) == nil
	// os.RemoveAll(root); os.MkdirAll(root+"/2026/11", 0o770)   // removeLocal + rotation off
	// grantServerAccess(root, root+"/2026/11") == nil
	// root, root/2026, root/2026/11 all have Uid == 4242 && Gid == 4242
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./backup -run 'TestDirsFrom|TestInitServerDirOwner|TestGrantServerAccess' -count=1`
Expected: FAIL, `undefined: dirsFrom`.

- [ ] **Step 3: Implement**

- `dirsFrom` in `mssql_access.go` with `filepath.Clean` + `filepath.Rel`; reject a `Rel` result equal to `..` or starting with `..` + separator.
- Unix uid/gid from `info.Sys().(*syscall.Stat_t)`.
- Windows `grantServerAccess`: `acl.Apply(dir, false, false, acl.GrantName(windows.GENERIC_READ|windows.GENERIC_WRITE, "NT SERVICE\\MSSQLSERVER"))`; return the error.

- [ ] **Step 4: Run tests and builds**

Run: `go test ./backup -count=1 && GOOS=windows go build ./... && GOOS=darwin go vet ./backup`
Expected: `ok`, silent builds. Then once as root: `sudo go test ./backup -run TestGrantServerAccess -count=1 -v` → `PASS` (not `SKIP`).

- [ ] **Step 5: Commit**

```bash
git add backup/mssql_access.go backup/mssql_access_unix.go backup/mssql_access_windows.go backup/mssql_access_test.go
git commit -m "Grant SQL Server write access to created backup folders"
```

---

### Task 2: Platform-neutral connection, host guard and database listing

**Review:** per-task — security (credentials in connection string)

**Files:**
- Create: `backup/mssql.go`
- Create: `backup/mssql_test.go`
- Delete: `backup/mssql_linux.go`, `backup/mssql_windows.go`
- Modify: `backup/backup.go:220-222` (remove the `mssqlDB.Close()` block)

**Interfaces:**
- Consumes: `params`, `logger`, `notify.SendAlarm`.
- Produces:
  - `var mssqlDB *sql.DB`
  - `func mssqlDSN(r config.Remote) string`
  - `func isLocalHost(host string) (bool, error)`
  - `func InitializeMSSQL()` (exported, called by `main.go` unchanged)
  - `const mssqlListQuery = "SELECT name FROM sys.databases WHERE database_id > 4 AND state_desc = 'ONLINE' AND source_database_id IS NULL ORDER BY name"`
  - `func getMSSQLList() []string`
  - `func dumpMSSQLDB(dbName, dst string) (string, string, error)` — moved verbatim from `mssql_linux.go` so the build stays green; Task 3 rewrites it.

- [ ] **Step 1: Write the failing tests** in `backup/mssql_test.go`

```go
func TestMSSQLDSNEscapesCredentials(t *testing.T) {
	// table, each case: u, err := url.Parse(mssqlDSN(remote)); err == nil; u.Scheme == "sqlserver";
	// u.Query().Get("database") == "master"; u.Query().Get("app name") == "monodb-backup"
	// {Host: "db.internal", Port: "1433", User: "backup", Password: "p;w@d/:?#%"}:
	//     u.Host == "db.internal:1433"; u.User.Username() == "backup"; password == "p;w@d/:?#%"
	// {Host: "::1", Port: "1433", ...}               -> u.Host == "[::1]:1433"
	// {Host: "db.internal", Port: "", ...}           -> u.Host == "db.internal"
	// {Host: `localhost\SQLEXPRESS`, Port: "", ...}  -> u.Host == "localhost"; u.Path == "/SQLEXPRESS"
	// {Host: "db.internal", User: "", Password: ""}  -> u.User == nil
}

func TestIsLocalHost(t *testing.T) {
	// "127.0.0.1", "::1", "localhost", `localhost\SQLEXPRESS` -> true, nil
	// "192.0.2.10" (TEST-NET-1)                             -> false, nil
	// ""                                                     -> error
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./backup -run 'TestMSSQLDSN|TestIsLocalHost' -count=1`
Expected: FAIL, `undefined: mssqlDSN`.

- [ ] **Step 3: Implement `backup/mssql.go`**

- `mssqlDSN`: split `Host` on the first `\` into host and instance; `url.URL{Scheme: "sqlserver", Host: ..., Path: "/"+instance when set}`; `net.JoinHostPort` when `Port != ""`, bare host otherwise; `url.UserPassword` only when `User != ""`. Query `database=master`, `app name=monodb-backup`. No `encrypt` parameter.
- `isLocalHost`: strip `\instance`; resolve with `net.LookupIP` (an IP literal parses directly); true when any address is loopback or equals an address from `net.InterfaceAddrs()`.
- `InitializeMSSQL`, in order, each failure → `notify.SendAlarm(msg, true)` then `logger.Fatal(msg)`:
  1. `!params.Remote.IsRemote` → existing "Remote should be enabled when backing up MSSQL databases." message.
  2. `isLocalHost` false or error → `"MSSQL backups must run on the SQL Server host; remote.host " + host + " is not an address of this machine"`.
  3. `sql.Open("sqlserver", mssqlDSN(params.Remote))` then `PingContext` (30 s timeout) error → `"Couldn't connect to MSSQL - Error: " + err.Error()`.
- `getMSSQLList`: run `mssqlListQuery`; query/scan/rows error → alarm + `logger.Fatal("Couldn't get the list of databases - Error: " + err.Error())`. Never close `mssqlDB`.
- Move `dumpMSSQLDB` unchanged, delete both platform files, remove the `Close()` block in `Backup()`.

- [ ] **Step 4: Run tests and both builds**

Run: `go test ./backup -count=1 && GOOS=linux go build ./... && GOOS=windows go build ./...`
Expected: `ok monodb-backup/backup`, both builds silent.

- [ ] **Step 5: Commit**

```bash
git add backup/mssql.go backup/mssql_test.go backup/backup.go backup/mssql_linux.go backup/mssql_windows.go
git commit -m "Unify MSSQL connection and keep the pool open across runs"
```

---

### Task 3: Safe, copy-only backup with encryption

**Review:** per-task — security (T-SQL injection, encryption at rest)

**Files:**
- Modify: `backup/mssql.go` (`InitializeMSSQL`, replace `dumpMSSQLDB`)
- Modify: `backup/backup.go:173` (skip `dumpDBWithTables` for MSSQL)
- Modify: `backup/rotation.go:25` (table naming off for MSSQL)
- Modify: `backup/mssql_test.go`

**Interfaces:**
- Consumes: `mssqlDB`, `InitializeMSSQL` (Task 2); `initServerDirOwner`, `grantServerAccess` (Task 1); `dumpName`, `params.Rotation`, `params.ArchivePass`, `params.CtxCancel`, `params.BackupDestination`.
- Produces:
  - `func mssqlBackupStatement(compress bool) string`
  - `func mssqlSupportsCompression(ctx context.Context) (bool, error)` — `SELECT CAST(SERVERPROPERTY('EngineEdition') AS int)`; false when 4.
  - `func checkMSSQLBackupVisible(path string) error`
  - `func encryptMSSQLBackup(bakPath string) (string, error)` — returns `bakPath + ".7z"`.
  - `func dumpMSSQLDB(dbName, dst string) (string, string, error)` — (local file path, upload name, error); signature unchanged so `dumpDB` needs no edit.

- [ ] **Step 1: Write the failing tests**

```go
func TestMSSQLBackupStatement(t *testing.T) {
	// false -> exactly "BACKUP DATABASE @db TO DISK = @path WITH COPY_ONLY, FORMAT, INIT, CHECKSUM"
	// true  -> the same + ", COMPRESSION"
}

func TestCheckMSSQLBackupVisibleExplainsColocation(t *testing.T) {
	// missing path         -> err != nil && strings.Contains(err.Error(), "must run on the SQL Server host")
	// existing empty file  -> err != nil
	// existing 1-byte file -> nil
}

func TestMSSQLDumpIgnoresBackupAsTables(t *testing.T) {
	// mirror TestMongoDumpIgnoresBackupAsTables with Database: "mssql":
	// dumpName("app", config.Rotation{Enabled: true, Suffix: "day"}, "") == "app-Thursday"
}

func TestEncryptMSSQLBackupReplacesPlainFile(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil { t.Skip("7z not installed") }
	// params = &config.Params{ArchivePass: "s3cret"}; write 64 KiB random bytes to dir/app-Mon.bak
	// got, err := encryptMSSQLBackup(bak); err == nil; got == bak+".7z"; os.Stat(bak) is IsNotExist
	// `7z t -ps3cret got` exits 0; `7z t -pwrong got` exits non-zero
}

func TestEncryptMSSQLBackupFailureLeavesNoFiles(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil { t.Skip("7z not installed") }
	// bak := filepath.Join(dir, "missing.bak") (never created)
	// _, err := encryptMSSQLBackup(bak); err != nil; os.Stat(bak+".7z") is IsNotExist
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./backup -run 'TestMSSQLBackupStatement|TestCheckMSSQL|TestMSSQLDumpIgnores|TestEncryptMSSQL' -count=1`
Expected: FAIL, undefined symbols (and `TestMSSQLDumpIgnoresBackupAsTables` returning `app/-Thursday`).

- [ ] **Step 3: Implement**

- `InitializeMSSQL`: after the ping, `initServerDirOwner(params.BackupDestination)`; error → alarm + `logger.Fatal`.
- `backup.go:173` and `rotation.go:25`: add `&& params.Database != "mssql"` next to the existing MongoDB exclusion.
- `encryptMSSQLBackup`: `exec.Command("7z", "a", "-t7z", "-mhe=on", "-mx0", "-p"+params.ArchivePass, bakPath+".7z", bakPath)`, stderr captured into the returned error; on error remove `bakPath+".7z"` and `bakPath`; on success remove `bakPath`.
- `dumpMSSQLDB` flow:
  1. `name := dumpName(dbName, params.Rotation, "") + ".bak"`; `dumpPath := filepath.Join(dst, filepath.FromSlash(name))`.
  2. `os.MkdirAll(filepath.Dir(dumpPath), 0o770)`, then `grantServerAccess(params.BackupDestination, filepath.Dir(dumpPath))`.
  3. `ctx, cancel := context.WithTimeout(context.Background(), time.Duration(params.CtxCancel)*time.Hour)`.
  4. `compress, err := mssqlSupportsCompression(ctx)`; log `"MSSQL backup started. DB: <db> - Compression: <bool> - Encrypted: <bool>"`.
  5. `mssqlDB.ExecContext(ctx, mssqlBackupStatement(compress), sql.Named("db", dbName), sql.Named("path", dumpPath))`; on error `os.Remove(dumpPath)` (ignore not-exist) and return it.
  6. `checkMSSQLBackupVisible(dumpPath)`.
  7. `params.ArchivePass != ""` → `encryptMSSQLBackup`; on success return the `.7z` path and `name + ".7z"`.

- [ ] **Step 4: Run tests and both builds**

Run: `go test ./backup -count=1 && GOOS=windows go build ./...`
Expected: `ok monodb-backup/backup`, silent build.

- [ ] **Step 5: Commit**

```bash
git add backup/mssql.go backup/mssql_test.go backup/backup.go backup/rotation.go
git commit -m "Back up MSSQL copy-only with bound params and 7z encryption"
```

---

### Task 4: Integration test against SQL Server 2022 and docs

**Files:**
- Create: `backup/mssql_integration_test.go`
- Modify: `README.md`, `README.tr.md`, `config/config.sample.yml`

**Interfaces:**
- Consumes: `mssqlDSN`, `getMSSQLList`, `dumpMSSQLDB`, `Backup`, `mssqlDB`, `initServerDirOwner` (Tasks 1–3); `notify.SuccessfulDBList`, `notify.FailedDBList`.
- Produces: nothing other code uses.

- [ ] **Step 1: Write the integration test**

Skip unless `MONODB_MSSQL_TEST_DSN` (sqlserver URL) and `MONODB_MSSQL_TEST_DIR` (mounted at the same path inside the container) are set. Set `params` to `{Database: "mssql", BackupDestination: dir, Rotation: {Enabled: true, Suffix: "day"}, CtxCancel: 1, Remote: <parsed from DSN>}`, open `mssqlDB`, call `initServerDirOwner(dir)`. Setup creates `smoke` and `we]ird'name` (table with 1000 rows each) and `gone` (set `OFFLINE`); cleanup drops them and every `*_restored`.

```go
func TestMSSQLIntegration(t *testing.T) {
	// "list": getMSSQLList() contains "smoke" and "we]ird'name"; excludes "master", "msdb", "gone"
	// "init overwrites": dumpMSSQLDB("smoke", dst) twice with the same dateNow; size2 < size1*3/2;
	//     RESTORE HEADERONLY FROM DISK = @p returns exactly 1 row with IsCopyOnly = 1
	// "restore": for "smoke" and "we]ird'name": RESTORE FILELISTONLY, then
	//     RESTORE DATABASE [<db>_restored] FROM DISK = @p WITH MOVE ...; SELECT COUNT(*) == 1000
	// "missing db": dumpMSSQLDB("does_not_exist", dst) -> err != nil; no file at its .bak path
	// "encrypted": params.ArchivePass = "s3cret"; path ends with ".bak.7z"; no ".bak" beside it;
	//     7z x -ps3cret, restore the extracted .bak, COUNT(*) == 1000
	// "two runs": params.Databases = ["smoke"]; params.BackupType.Type = ""; params.RemoveLocal = false;
	//     reset notify lists; Backup(); Backup(); len(notify.SuccessfulDBList) == 2 && len(notify.FailedDBList) == 0
}
```

- [ ] **Step 2: Run it against a Developer edition container**

```bash
export MONODB_MSSQL_TEST_DIR=$(mktemp -d /tmp/mssql-it.XXXX)
export MSSQL_TEST_PASSWORD='Smoke-Pass-123'
docker run -d --name mssql-it --user root -e ACCEPT_EULA=Y -e MSSQL_SA_PASSWORD="$MSSQL_TEST_PASSWORD" \
  -p 14330:1433 -v "$MONODB_MSSQL_TEST_DIR:$MONODB_MSSQL_TEST_DIR" mcr.microsoft.com/mssql/server:2022-latest
export MONODB_MSSQL_TEST_DSN="sqlserver://sa:$MSSQL_TEST_PASSWORD@127.0.0.1:14330?database=master"
go test ./backup -run TestMSSQLIntegration -count=1 -v
```

Expected: every subtest `PASS`. If the driver rejects `BACKUP DATABASE @db`, stop and report; do not fall back to string concatenation.

- [ ] **Step 3: Re-run on Express and as the real service account**

Recreate the container with `-e MSSQL_PID=Express` (still `--user root`), run Step 2's `go test` → `PASS` (no-`COMPRESSION` path).
Recreate without `--user root` (container user `mssql`, uid 10001), `sudo chown 10001:10001 "$MONODB_MSSQL_TEST_DIR"`, run `sudo -E go test ./backup -run TestMSSQLIntegration -count=1 -v` → `PASS` (`grantServerAccess` on new rotation folders, root reading `mssql`-owned `.bak`).

- [ ] **Step 4: Verify the startup failure paths**

Throwaway config `/tmp/mssql-bad.yml`: `database: mssql`, `backupDestination: $MONODB_MSSQL_TEST_DIR`, `remote: {isRemote: true, host: 127.0.0.1, port: 14331, user: sa, password: x}`.
Run `go run . -config /tmp/mssql-bad.yml; echo exit=$?` → log contains `Couldn't connect to MSSQL - Error:` and `exit=1`.
Change `host` to `192.0.2.10`, run again → log contains `must run on the SQL Server host` and `exit=1`. Delete the config.

- [ ] **Step 5: Update docs**

- `README.md`: intro and features name Microsoft SQL Server. New `## MSSQL notes` section: run monodb-backup on the SQL Server host, as root or as the service account; `remote` block required (SQL login; on Windows an empty user uses integrated auth); `backupDestination` must exist and be owned by the service account (`chown mssql: /var/backups`), new subfolders follow automatically when running as root; backups are `COPY_ONLY` and leave existing backup chains alone; Express edition backups are uncompressed; MSSQL is written locally then uploaded, never streamed; `backupAsTables` is ignored.
- `README.tr.md`: same content in Turkish with correct characters (ç, ğ, ı, İ, ö, ş, ü).
- `config/config.sample.yml`: `database: postgresql # mysql, mongodb or mssql - default is postgresql`; on `isRemote`: `# must be true for mssql`.

- [ ] **Step 6: Full suite, then clean up**

Run: `go test ./... -count=1 && GOOS=windows go build ./...`
Expected: all `ok` (integration test `SKIP` without env). Then `docker rm -f mssql-it` and `trash-put "$MONODB_MSSQL_TEST_DIR"`.

- [ ] **Step 7: Commit**

```bash
git add backup/mssql_integration_test.go README.md README.tr.md config/config.sample.yml
git commit -m "Add MSSQL integration test and document MSSQL backups"
```
