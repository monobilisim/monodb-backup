package backup

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"monodb-backup/clog"
	"monodb-backup/config"
	"monodb-backup/notify"
)

const weirdDBName = "we]ird'name"

func bracket(name string) string { return "[" + strings.ReplaceAll(name, "]", "]]") + "]" }

func literal(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func scanAll(t *testing.T, rows *sql.Rows) []map[string]any {
	t.Helper()
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = vals[i]
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func mssqlExec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := mssqlDB.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func restoreMSSQL(t *testing.T, bak, target string) {
	t.Helper()
	ctx := context.Background()
	rows, err := mssqlDB.QueryContext(ctx, "RESTORE FILELISTONLY FROM DISK = @p", sql.Named("p", bak))
	if err != nil {
		t.Fatal(err)
	}
	var moves []string
	for i, f := range scanAll(t, rows) {
		logical := fmt.Sprint(f["LogicalName"])
		ext := ".mdf"
		if fmt.Sprint(f["Type"]) == "L" {
			ext = ".ldf"
		}
		moves = append(moves, fmt.Sprintf("MOVE %s TO %s", literal(logical), literal(fmt.Sprintf("/var/opt/mssql/data/restored_%s_%d%s", strings.NewReplacer("]", "_", "'", "_").Replace(target), i, ext))))
	}
	mssqlExec(t, "RESTORE DATABASE "+bracket(target)+" FROM DISK = @p WITH "+strings.Join(moves, ", "), sql.Named("p", bak))
}

func countRows(t *testing.T, db string) int {
	t.Helper()
	var n int
	if err := mssqlDB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+bracket(db)+".dbo.t").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func dropAllTestDBs(t *testing.T) {
	t.Helper()
	rows, err := mssqlDB.QueryContext(context.Background(), `SELECT name FROM sys.databases WHERE name IN ('smoke', 'gone', 'we]ird''name') OR name LIKE '%\_restored' ESCAPE '\'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range scanAll(t, rows) {
		names = append(names, fmt.Sprint(r["name"]))
	}
	for _, n := range names {
		// Dropping an offline database leaves its files behind, so bring it online first.
		_, _ = mssqlDB.ExecContext(context.Background(), "ALTER DATABASE "+bracket(n)+" SET ONLINE")
		mssqlExec(t, "ALTER DATABASE "+bracket(n)+" SET SINGLE_USER WITH ROLLBACK IMMEDIATE")
		mssqlExec(t, "DROP DATABASE "+bracket(n))
	}
}

func TestMSSQLIntegration(t *testing.T) {
	dsn := os.Getenv("MONODB_MSSQL_TEST_DSN")
	dir := os.Getenv("MONODB_MSSQL_TEST_DIR")
	if dsn == "" || dir == "" {
		t.Skip("MONODB_MSSQL_TEST_DSN and MONODB_MSSQL_TEST_DIR are not set")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	clog.InitializeLogger()
	pass, _ := u.User.Password()
	oldParams, oldDB, oldDate := params, mssqlDB, dateNow
	t.Cleanup(func() { params, mssqlDB, dateNow = oldParams, oldDB, oldDate })
	params = &config.Params{
		Database:          "mssql",
		BackupDestination: dir,
		Rotation:          config.Rotation{Enabled: true, Suffix: "day"},
		CtxCancel:         1,
		Remote:            config.Remote{IsRemote: true, Host: u.Hostname(), Port: u.Port(), User: u.User.Username(), Password: pass},
	}
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mssqlDB = db
	if err := initServerDirOwner(dir); err != nil {
		t.Fatal(err)
	}

	dropAllTestDBs(t)
	t.Cleanup(func() { dropAllTestDBs(t) })
	for _, n := range []string{"smoke", weirdDBName, "gone"} {
		mssqlExec(t, "CREATE DATABASE "+bracket(n))
	}
	for _, n := range []string{"smoke", weirdDBName} {
		mssqlExec(t, "CREATE TABLE "+bracket(n)+".dbo.t (id int)")
		mssqlExec(t, "INSERT INTO "+bracket(n)+".dbo.t (id) SELECT TOP 1000 ROW_NUMBER() OVER (ORDER BY (SELECT NULL)) FROM sys.all_objects a CROSS JOIN sys.all_objects b")
	}
	mssqlExec(t, "ALTER DATABASE [gone] SET OFFLINE WITH ROLLBACK IMMEDIATE")

	dateNow = rightNow{day: "Mon"}
	bakPath := func(db string) string { return filepath.Join(dir, dumpName(db, params.Rotation, "")+".bak") }

	t.Run("list", func(t *testing.T) {
		list := getMSSQLList()
		for _, want := range []string{"smoke", weirdDBName} {
			if !slices.Contains(list, want) {
				t.Errorf("list %v misses %q", list, want)
			}
		}
		for _, bad := range []string{"master", "msdb", "gone"} {
			if slices.Contains(list, bad) {
				t.Errorf("list %v contains %q", list, bad)
			}
		}
	})

	t.Run("init overwrites", func(t *testing.T) {
		p1, _, err := dumpMSSQLDB("smoke", dir)
		if err != nil {
			t.Fatal(err)
		}
		fi1, err := os.Stat(p1)
		if err != nil {
			t.Fatal(err)
		}
		p2, _, err := dumpMSSQLDB("smoke", dir)
		if err != nil {
			t.Fatal(err)
		}
		fi2, err := os.Stat(p2)
		if err != nil {
			t.Fatal(err)
		}
		if p1 != p2 {
			t.Fatalf("paths differ: %s vs %s", p1, p2)
		}
		if fi2.Size() >= fi1.Size()*3/2 {
			t.Errorf("size grew from %d to %d, backup was appended instead of overwritten", fi1.Size(), fi2.Size())
		}
		rows, err := mssqlDB.QueryContext(context.Background(), "RESTORE HEADERONLY FROM DISK = @p", sql.Named("p", p2))
		if err != nil {
			t.Fatal(err)
		}
		hdr := scanAll(t, rows)
		if len(hdr) != 1 {
			t.Fatalf("backup set count = %d, want 1", len(hdr))
		}
		if v := fmt.Sprint(hdr[0]["IsCopyOnly"]); v != "true" && v != "1" {
			t.Errorf("IsCopyOnly = %v, want true", hdr[0]["IsCopyOnly"])
		}
	})

	t.Run("restore", func(t *testing.T) {
		for _, n := range []string{"smoke", weirdDBName} {
			p, _, err := dumpMSSQLDB(n, dir)
			if err != nil {
				t.Fatalf("%s: %v", n, err)
			}
			restoreMSSQL(t, p, n+"_restored")
			if got := countRows(t, n+"_restored"); got != 1000 {
				t.Errorf("%s: restored rows = %d, want 1000", n, got)
			}
		}
	})

	t.Run("missing db", func(t *testing.T) {
		dateNow = rightNow{day: "Wed"}
		if _, _, err := dumpMSSQLDB("does_not_exist", dir); err == nil {
			t.Fatal("expected an error")
		}
		if _, err := os.Stat(bakPath("does_not_exist")); !os.IsNotExist(err) {
			t.Errorf("backup file left behind: %v", err)
		}
	})

	t.Run("encrypted", func(t *testing.T) {
		dateNow = rightNow{day: "Tue"}
		params.ArchivePass = "s3cret"
		t.Cleanup(func() { params.ArchivePass = "" })
		p, name, err := dumpMSSQLDB("smoke", dir)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(p, ".bak.7z") || !strings.HasSuffix(name, ".bak.7z") {
			t.Fatalf("path %s, name %s: want .bak.7z", p, name)
		}
		if _, err := os.Stat(strings.TrimSuffix(p, ".7z")); !os.IsNotExist(err) {
			t.Errorf("plaintext .bak remains: %v", err)
		}
		out, err := os.MkdirTemp(dir, "extract")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(out) })
		if b, err := exec.Command("7z", "x", "-ps3cret", "-o"+out, p).CombinedOutput(); err != nil {
			t.Fatalf("7z x: %v: %s", err, b)
		}
		restoreMSSQL(t, filepath.Join(out, filepath.Base(strings.TrimSuffix(p, ".7z"))), "smoke_enc_restored")
		if got := countRows(t, "smoke_enc_restored"); got != 1000 {
			t.Errorf("restored rows = %d, want 1000", got)
		}
	})

	t.Run("two runs", func(t *testing.T) {
		params.Databases = []string{"smoke"}
		params.BackupType.Type = ""
		params.RemoveLocal = false
		notify.SuccessfulDBList, notify.FailedDBList = nil, nil
		Backup()
		Backup()
		if len(notify.SuccessfulDBList) != 2 || len(notify.FailedDBList) != 0 {
			t.Errorf("successful %v, failed %v; want 2 and 0", notify.SuccessfulDBList, notify.FailedDBList)
		}
	})
}
