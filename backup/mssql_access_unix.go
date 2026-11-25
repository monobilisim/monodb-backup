//go:build !windows

package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type dirOwner struct{ uid, gid int }

var serverOwner *dirOwner

func initServerDirOwner(root string) error {
	info, err := statDestination(root)
	if err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return nil
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("backupDestination %q: cannot read owner", root)
	}
	serverOwner = &dirOwner{int(st.Uid), int(st.Gid)}
	return nil
}

// Symlinks are never followed: the SQL Server account owns this tree and could plant one while we run as root.
func grantServerAccess(root, dir string) error {
	if serverOwner == nil {
		return nil
	}
	dirs, err := dirsFrom(root, dir)
	if err != nil {
		return err
	}
	root = dirs[0]
	info, err := os.Lstat(root)
	if err := requireRealDir(root, info, err); err != nil {
		return err
	}
	if err := os.Lchown(root, serverOwner.uid, serverOwner.gid); err != nil {
		return err
	}
	if len(dirs) == 1 {
		return nil
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, d := range dirs[1:] {
		rel, err := filepath.Rel(root, d)
		if err != nil {
			return err
		}
		info, err := r.Lstat(rel)
		if err := requireRealDir(d, info, err); err != nil {
			return err
		}
		if err := r.Lchown(rel, serverOwner.uid, serverOwner.gid); err != nil {
			return err
		}
	}
	return nil
}

func requireRealDir(path string, info os.FileInfo, err error) error {
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s is not a real directory", path)
	}
	return nil
}
