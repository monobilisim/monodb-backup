//go:build windows

package backup

import (
	"fmt"
	"os"

	"github.com/hectane/go-acl"
	"golang.org/x/sys/windows"
)

func initServerDirOwner(root string) error {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("backupDestination %q must be an existing directory owned by the SQL Server service account", root)
	}
	return nil
}

func grantServerAccess(_, dir string) error {
	return acl.Apply(dir, false, false, acl.GrantName(windows.GENERIC_READ|windows.GENERIC_WRITE, "NT SERVICE\\MSSQLSERVER"))
}
