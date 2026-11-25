//go:build !windows

package backup

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func ownerOf(t *testing.T, path string) (uint32, uint32) {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	st := fi.Sys().(*syscall.Stat_t)
	return st.Uid, st.Gid
}

func resetServerOwner(t *testing.T) {
	t.Helper()
	prev := serverOwner
	serverOwner = nil
	t.Cleanup(func() { serverOwner = prev })
}

func TestGrantServerAccessNoOwnerIsNoop(t *testing.T) {
	resetServerOwner(t)
	root := t.TempDir()
	leaf := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(leaf, 0o770); err != nil {
		t.Fatal(err)
	}
	uid, gid := ownerOf(t, leaf)
	if err := grantServerAccess(root, leaf); err != nil {
		t.Fatal(err)
	}
	if u, g := ownerOf(t, leaf); u != uid || g != gid {
		t.Errorf("owner changed to %d:%d", u, g)
	}
	if err := grantServerAccess(root, filepath.Dir(root)); err != nil {
		t.Errorf("no-owner call must not validate paths: %v", err)
	}
}

func TestGrantServerAccessRejectsDirOutsideRoot(t *testing.T) {
	resetServerOwner(t)
	serverOwner = &dirOwner{os.Getuid(), os.Getgid()}
	root := filepath.Join(t.TempDir(), "dest")
	if err := os.Mkdir(root, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := grantServerAccess(root, filepath.Dir(root)); err == nil {
		t.Fatal("want error for dir outside root")
	}
}

func TestGrantServerAccessRestoresRecreatedDestination(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	resetServerOwner(t)
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
		if u, g := ownerOf(t, d); u != 4242 || g != 4242 {
			t.Errorf("%s owner %d:%d", d, u, g)
		}
	}
}

func TestGrantServerAccessRefusesSymlinks(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	resetServerOwner(t)
	base := t.TempDir()
	root := filepath.Join(base, "dest")
	victim := filepath.Join(base, "victim")
	for _, d := range []string{root, filepath.Join(victim, "Mon")} {
		if err := os.MkdirAll(d, 0o770); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{victim, filepath.Join(victim, "Mon")} {
		if err := os.Chown(d, 5151, 5151); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chown(root, 4242, 4242); err != nil {
		t.Fatal(err)
	}
	if err := initServerDirOwner(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(root, "Daily")); err != nil {
		t.Fatal(err)
	}
	if err := grantServerAccess(root, filepath.Join(root, "Daily", "Mon")); err == nil {
		t.Fatal("want error for symlinked intermediate component")
	}
	for _, d := range []string{victim, filepath.Join(victim, "Mon")} {
		if u, g := ownerOf(t, d); u != 5151 || g != 5151 {
			t.Errorf("%s owner changed to %d:%d", d, u, g)
		}
	}

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, root); err != nil {
		t.Fatal(err)
	}
	if err := grantServerAccess(root, root); err == nil {
		t.Fatal("want error for symlinked root")
	}
	if u, g := ownerOf(t, victim); u != 5151 || g != 5151 {
		t.Errorf("victim owner changed to %d:%d", u, g)
	}
}

func TestRequireRealDirNamesFullPath(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if got := requireRealDir(link, info, err); got == nil || !strings.Contains(got.Error(), link) {
		t.Fatalf("got %v, want an error naming %s", got, link)
	}
}
