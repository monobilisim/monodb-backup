//go:build windows

package backup

import (
	"github.com/hectane/go-acl"
	"golang.org/x/sys/windows"
)

func initServerDirOwner(root string) error {
	_, err := statDestination(root)
	return err
}

func grantServerAccess(_, dir string) error {
	return acl.Apply(dir, false, false, acl.GrantName(windows.GENERIC_READ|windows.GENERIC_WRITE, "NT SERVICE\\MSSQLSERVER"))
}
