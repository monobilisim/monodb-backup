package backup

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDirsFrom(t *testing.T) {
	p := filepath.FromSlash
	got, err := dirsFrom(p("/var/backups"), p("/var/backups/Daily/Mon"))
	want := []string{p("/var/backups"), p("/var/backups/Daily"), p("/var/backups/Daily/Mon")}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v; want %v", got, err, want)
	}
	got, err = dirsFrom(p("/var/backups/"), p("/var/backups"))
	if err != nil || !reflect.DeepEqual(got, []string{p("/var/backups")}) {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, dir := range []string{"/var/backupsX/a", "/var/backups/../etc", "/var", "/etc"} {
		if _, err := dirsFrom(p("/var/backups"), p(dir)); err == nil {
			t.Errorf("dirsFrom(%q) want error", dir)
		}
	}
}

func TestInitServerDirOwnerRequiresDestination(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	if err := initServerDirOwner(missing); err == nil || !strings.Contains(err.Error(), "backupDestination") {
		t.Fatalf("missing dir: got %v", err)
	}
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := initServerDirOwner(f); err == nil {
		t.Fatal("regular file: want error")
	}
}
