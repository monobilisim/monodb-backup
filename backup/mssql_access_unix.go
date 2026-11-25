//go:build !windows

package backup

import (
	"fmt"
	"os"
	"syscall"
)

type dirOwner struct{ uid, gid int }

var serverOwner *dirOwner

func initServerDirOwner(root string) error {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("backupDestination %q must be an existing directory owned by the SQL Server service account", root)
	}
	if os.Geteuid() != 0 {
		return nil
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		serverOwner = &dirOwner{int(st.Uid), int(st.Gid)}
	}
	return nil
}

func grantServerAccess(root, dir string) error {
	if serverOwner == nil {
		return nil
	}
	dirs, err := dirsFrom(root, dir)
	if err != nil {
		return err
	}
	for _, d := range dirs {
		if err := os.Chown(d, serverOwner.uid, serverOwner.gid); err != nil {
			return err
		}
	}
	return nil
}
