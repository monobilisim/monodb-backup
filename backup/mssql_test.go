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
	"strings"
	"testing"

	"monodb-backup/config"
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
	for _, host := range []string{"127.0.0.1", "::1", "localhost", `localhost\SQLEXPRESS`} {
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
	err := checkMSSQLBackupVisible(filepath.Join(dir, "missing.bak"))
	if err == nil || !strings.Contains(err.Error(), "must run on the SQL Server host") {
		t.Errorf("missing path: err = %v", err)
	}
	empty := filepath.Join(dir, "empty.bak")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkMSSQLBackupVisible(empty); err == nil {
		t.Error("empty file: expected error")
	}
	one := filepath.Join(dir, "one.bak")
	if err := os.WriteFile(one, []byte{1}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkMSSQLBackupVisible(one); err != nil {
		t.Errorf("1-byte file: err = %v", err)
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
}

func TestEncryptMSSQLBackupFailureLeavesNoFiles(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("7z not installed")
	}
	oldParams := params
	t.Cleanup(func() { params = oldParams })
	params = &config.Params{ArchivePass: "s3cret"}

	bak := filepath.Join(t.TempDir(), "missing.bak")
	if _, err := encryptMSSQLBackup(bak); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(bak + ".7z"); !os.IsNotExist(err) {
		t.Errorf(".7z left behind: %v", err)
	}
}
