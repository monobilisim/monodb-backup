//go:build linux

package backup

import "errors"

func InitializeMSSQL() {
	logger.Error("Currently, MSSQL backups only works on Windows.")
}
func getMSSQLList() []string {
	logger.Error("Currently, MSSQL backups only works on Windows.")
	return make([]string, 0)
}
func dumpMSSQLDB(_, _ string) (string, string, error) {
	err := errors.New("currently, MSSQL backups only works on Windows")
	logger.Error(err)
	return "", "", err
}
