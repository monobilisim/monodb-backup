package backup

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestDirsFrom(t *testing.T) {
	sep := string(filepath.Separator)
	p := func(s string) string { return filepath.FromSlash(s) }
	got, err := dirsFrom(p("/var/backups"), p("/var/backups/Daily/Mon"))
	want := []string{p("/var/backups"), p("/var/backups/Daily"), p("/var/backups/Daily/Mon")}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v; want %v", got, err, want)
	}
	got, err = dirsFrom(p("/var/backups/"), p("/var/backups"))
	if err != nil || !reflect.DeepEqual(got, []string{p("/var/backups")}) {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, dir := range []string{"/var/backupsX/a", "/var/backups/../etc"} {
		if _, err := dirsFrom(p("/var/backups"), p(dir)); err == nil {
			t.Errorf("dirsFrom(%q) want error (sep %q)", dir, sep)
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

func TestGrantServerAccessRestoresRecreatedDestination(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() != 0 {
		t.Skip("needs root on unix")
	}
	root := filepath.Join(t.TempDir(), "dest")
	if err := os.Mkdir(root, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(root, 4242, 4242); err != nil {
		t.Fatal(err)
	}
	if err := initServerDirOwner(root); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(root, "2026", "11")
	if err := os.MkdirAll(leaf, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := grantServerAccess(root, leaf); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{root, filepath.Join(root, "2026"), leaf} {
		fi, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		st := reflect.ValueOf(fi.Sys()).Elem()
		uid, gid := st.FieldByName("Uid").Uint(), st.FieldByName("Gid").Uint()
		if uid != 4242 || gid != 4242 {
			t.Errorf("%s owner %d:%d", d, uid, gid)
		}
	}
}
