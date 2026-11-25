package backup

import (
	"context"
	"crypto/rand"
	"database/sql"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"monodb-backup/clog"
	"monodb-backup/config"
	"monodb-backup/notify"
)

func TestMSSQLDSNEscapesCredentials(t *testing.T) {
	const pass = "p;w@d/:?#%"
	tests := []struct {
		name     string
		remote   config.Remote
		host     string
		path     string
		noUser   bool
		wantUser string
	}{
		{"host and port", config.Remote{Host: "db.internal", Port: "1433", User: "backup", Password: pass}, "db.internal:1433", "", false, "backup"},
		{"ipv6", config.Remote{Host: "::1", Port: "1433", User: "backup", Password: pass}, "[::1]:1433", "", false, "backup"},
		{"ipv6 no port", config.Remote{Host: "::1", User: "backup", Password: pass}, "[::1]:1433", "", false, "backup"},
		{"no port", config.Remote{Host: "db.internal", User: "backup", Password: pass}, "db.internal", "", false, "backup"},
		{"instance", config.Remote{Host: `localhost\SQLEXPRESS`, User: "backup", Password: pass}, "localhost", "/SQLEXPRESS", false, "backup"},
		{"dot", config.Remote{Host: ".", User: "backup", Password: pass}, "localhost", "", false, "backup"},
		{"dot instance", config.Remote{Host: `.\SQLEXPRESS`, User: "backup", Password: pass}, "localhost", "/SQLEXPRESS", false, "backup"},
		{"local alias", config.Remote{Host: "(LOCAL)", Port: "1434", User: "backup", Password: pass}, "localhost:1434", "", false, "backup"},
		{"no credentials", config.Remote{Host: "db.internal"}, "db.internal", "", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(mssqlDSN(tt.remote))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if u.Scheme != "sqlserver" {
				t.Errorf("scheme = %q", u.Scheme)
			}
			if got := u.Query().Get("database"); got != "master" {
				t.Errorf("database = %q", got)
			}
			if got := u.Query().Get("app name"); got != "monodb-backup" {
				t.Errorf("app name = %q", got)
			}
			if u.Query().Has("encrypt") || len(u.Query()) != 2 {
				t.Errorf("unexpected query %q", u.RawQuery)
			}
			if u.Host != tt.host {
				t.Errorf("host = %q, want %q", u.Host, tt.host)
			}
			if u.Path != tt.path {
				t.Errorf("path = %q, want %q", u.Path, tt.path)
			}
			if tt.noUser {
				if u.User != nil {
					t.Errorf("user = %v, want nil", u.User)
				}
				return
			}
			if u.User.Username() != tt.wantUser {
				t.Errorf("user = %q", u.User.Username())
			}
			if got, _ := u.User.Password(); got != pass {
				t.Errorf("password = %q", got)
			}
		})
	}
}

func TestIsLocalHost(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1", "localhost", `localhost\SQLEXPRESS`, ".", `.\SQLEXPRESS`, "(local)", "(LOCAL)"} {
		got, err := isLocalHost(host)
		if err != nil || !got {
			t.Errorf("isLocalHost(%q) = %v, %v; want true, nil", host, got, err)
		}
	}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipNet, ok := a.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if got, err := isLocalHost(ipNet.IP.String()); err != nil || !got {
				t.Errorf("isLocalHost(%s) = %v, %v; want true, nil", ipNet.IP, got, err)
			}
			break
		}
	}
	if got, err := isLocalHost("192.0.2.10"); err != nil || got {
		t.Errorf("isLocalHost(192.0.2.10) = %v, %v; want false, nil", got, err)
	}
	if _, err := isLocalHost(""); err == nil {
		t.Error("isLocalHost(\"\") returned no error")
	}
}

func TestMSSQLConnectErrorHidesPassword(t *testing.T) {
	db, err := sql.Open("sqlserver", mssqlDSN(config.Remote{Host: "localhost", Port: "x", User: "u", Password: "s3cret"}))
	if err == nil {
		defer func() { _ = db.Close() }()
		err = db.PingContext(context.Background())
	}
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if msg := mssqlConnectError(err); strings.Contains(msg, "s3cret") {
		t.Errorf("message leaks password: %s", msg)
	}
}

func TestMSSQLBackupStatement(t *testing.T) {
	const base = "BACKUP DATABASE @db TO DISK = @path WITH COPY_ONLY, FORMAT, INIT, CHECKSUM"
	if got := mssqlBackupStatement(false); got != base {
		t.Errorf("statement(false) = %q", got)
	}
	if got := mssqlBackupStatement(true); got != base+", COMPRESSION" {
		t.Errorf("statement(true) = %q", got)
	}
}

func TestCheckMSSQLBackupVisibleExplainsColocation(t *testing.T) {
	dir := t.TempDir()
	err := checkMSSQLBackupVisible(filepath.Join(dir, "missing.bak"), time.Now())
	if err == nil || !strings.Contains(err.Error(), "must run on the SQL Server host") {
		t.Errorf("missing path: err = %v", err)
	}
	empty := filepath.Join(dir, "empty.bak")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkMSSQLBackupVisible(empty, time.Now()); err == nil {
		t.Error("empty file: expected error")
	}
	one := filepath.Join(dir, "one.bak")
	if err := os.WriteFile(one, []byte{1}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkMSSQLBackupVisible(one, time.Now()); err != nil {
		t.Errorf("1-byte file: err = %v", err)
	}
	old := filepath.Join(dir, "old.bak")
	if err := os.WriteFile(old, []byte{1}, 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	if err := checkMSSQLBackupVisible(old, time.Now()); err == nil {
		t.Error("stale file: expected error")
	}
	if err := checkMSSQLBackupVisible(old, past.Add(time.Second)); err != nil {
		t.Errorf("file within slack: err = %v", err)
	}
	link := filepath.Join(dir, "link.bak")
	if err := os.Symlink(one, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := checkMSSQLBackupVisible(link, time.Now()); err == nil {
		t.Error("symlink: expected error")
	}
}

func TestMSSQLDumpIgnoresBackupAsTables(t *testing.T) {
	oldParams, oldDateNow := params, dateNow
	t.Cleanup(func() {
		params, dateNow = oldParams, oldDateNow
	})
	params = &config.Params{Database: "mssql", BackupAsTables: true}
	dateNow = rightNow{day: "Thursday"}

	got := dumpName("app", config.Rotation{Enabled: true, Suffix: "day"}, "")
	if got != "app-Thursday" {
		t.Fatalf("dumpName() = %q, want %q", got, "app-Thursday")
	}
}

func TestEncryptMSSQLBackupReplacesPlainFile(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("7z not installed")
	}
	oldParams := params
	t.Cleanup(func() { params = oldParams })
	params = &config.Params{ArchivePass: "s3cret"}

	bak := filepath.Join(t.TempDir(), "app-Mon.bak")
	data := make([]byte, 64<<10)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bak, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := encryptMSSQLBackup(bak)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if got != bak+".7z" {
		t.Errorf("path = %q", got)
	}
	if _, err := os.Stat(bak); !os.IsNotExist(err) {
		t.Errorf("plain .bak still present: %v", err)
	}
	if out, err := exec.Command("7z", "t", "-ps3cret", got).CombinedOutput(); err != nil {
		t.Errorf("7z t with right password failed: %v\n%s", err, out)
	}
	if err := exec.Command("7z", "t", "-pwrong", got).Run(); err == nil {
		t.Error("7z t with wrong password succeeded")
	}
	if err := exec.Command("7z", "l", "-pwrong", got).Run(); err == nil {
		t.Error("7z l with wrong password succeeded: headers are not encrypted")
	}
}

func TestEncryptMSSQLBackupFailureLeavesNoFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell script as fake 7z")
	}
	fakeDir := t.TempDir()
	script := "#!/bin/sh\nfor a in \"$@\"; do case \"$a\" in *.7z) : > \"$a\";; esac; done\necho boom-from-7z >&2\nexit 2\n"
	if err := os.WriteFile(filepath.Join(fakeDir, "7z"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldParams := params
	t.Cleanup(func() { params = oldParams })
	params = &config.Params{ArchivePass: "s3cret"}

	bak := filepath.Join(t.TempDir(), "app.bak")
	if err := os.WriteFile(bak, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := encryptMSSQLBackup(bak)
	if err == nil || !strings.Contains(err.Error(), "boom-from-7z") {
		t.Fatalf("err = %v, want 7z stderr", err)
	}
	for _, p := range []string{bak, bak + ".7z"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s left behind: %v", p, err)
		}
	}
}

func TestEncryptMSSQLBackupReplacesArchiveWithOtherPassword(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("7z not installed")
	}
	oldParams := params
	t.Cleanup(func() { params = oldParams })
	params = &config.Params{ArchivePass: "s3cret"}

	dir := t.TempDir()
	old := filepath.Join(dir, "old.bak")
	if err := os.WriteFile(old, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	bak := filepath.Join(dir, "app.bak")
	if out, err := exec.Command("7z", "a", "-t7z", "-mhe=on", "-poldpass", bak+".7z", old).CombinedOutput(); err != nil {
		t.Fatalf("seed archive: %v\n%s", err, out)
	}
	if err := os.WriteFile(bak, []byte("new data"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := encryptMSSQLBackup(bak)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if out, err := exec.Command("7z", "t", "-ps3cret", got).CombinedOutput(); err != nil {
		t.Errorf("7z t failed: %v\n%s", err, out)
	}
}

func TestBackupSkipsDotDotDatabaseNames(t *testing.T) {
	oldParams, oldDateNow, oldFailed := params, dateNow, FailedDBNames
	oldList, oldOK := notify.FailedDBList, notify.SuccessfulDBList
	t.Cleanup(func() {
		params, dateNow, FailedDBNames = oldParams, oldDateNow, oldFailed
		notify.FailedDBList, notify.SuccessfulDBList = oldList, oldOK
	})
	clog.InitializeLogger()

	tree := t.TempDir()
	dest := filepath.Join(tree, "dest")
	victim := filepath.Join(tree, "victim")
	for _, d := range []string{dest, victim} {
		if err := os.Mkdir(d, 0o770); err != nil {
			t.Fatal(err)
		}
	}
	params = &config.Params{Database: "mssql", BackupDestination: dest, RemoveLocal: true, Databases: []string{"../victim/x"}}
	dateNow = rightNow{day: "Mon"}
	notify.FailedDBList, notify.SuccessfulDBList, FailedDBNames = nil, nil, nil

	Backup()

	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("victim directory was removed: %v", err)
	}
	if len(notify.FailedDBList) != 1 || len(notify.SuccessfulDBList) != 0 {
		t.Errorf("failed %v, successful %v; want 1 and 0", notify.FailedDBList, notify.SuccessfulDBList)
	}
}

func TestDumpMSSQLDBRejectsNameOutsideDestination(t *testing.T) {
	oldParams, oldDateNow := params, dateNow
	t.Cleanup(func() { params, dateNow = oldParams, oldDateNow })
	clog.InitializeLogger()

	tree := t.TempDir()
	dest := filepath.Join(tree, "dest")
	if err := os.Mkdir(dest, 0o770); err != nil {
		t.Fatal(err)
	}
	params = &config.Params{Database: "mssql", BackupDestination: dest, Rotation: config.Rotation{Enabled: true, Suffix: "day"}}
	dateNow = rightNow{day: "Mon"}

	if _, _, err := dumpMSSQLDB("../outside/new/x", dest); err == nil {
		t.Fatal("want an error")
	}
	if _, err := os.Stat(filepath.Join(tree, "outside")); !os.IsNotExist(err) {
		t.Errorf("directory outside the destination was created: %v", err)
	}
}

func TestRestoreMSSQLDestinationRecreatesRoot(t *testing.T) {
	oldParams := params
	t.Cleanup(func() { params = oldParams })
	clog.InitializeLogger()

	dest := filepath.Join(t.TempDir(), "dest")
	params = &config.Params{Database: "mssql", BackupDestination: dest}
	restoreMSSQLDestination()
	if fi, err := os.Stat(dest); err != nil || !fi.IsDir() {
		t.Fatalf("destination not recreated: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dest, "keep"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	restoreMSSQLDestination()
	if _, err := os.Stat(filepath.Join(dest, "keep")); err != nil {
		t.Errorf("existing destination was touched: %v", err)
	}
}

func TestDumpMSSQLDBDoesNotFollowSymlinkedDir(t *testing.T) {
	oldParams, oldDateNow := params, dateNow
	t.Cleanup(func() { params, dateNow = oldParams, oldDateNow })
	clog.InitializeLogger()

	tree := t.TempDir()
	dest := filepath.Join(tree, "dest")
	outside := filepath.Join(tree, "outside")
	for _, d := range []string{dest, outside} {
		if err := os.Mkdir(d, 0o770); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(dest, "Daily")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	params = &config.Params{Database: "mssql", BackupDestination: dest, Rotation: config.Rotation{Enabled: true, Suffix: "day"}}
	dateNow = rightNow{day: "Mon"}

	if _, _, err := dumpMSSQLDB("x", filepath.Join(dest, "Daily", "Mon")); err == nil {
		t.Fatal("want an error")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("created inside the symlink target: %v", entries)
	}
}
