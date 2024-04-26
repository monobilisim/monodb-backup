package backup

import (
	"fmt"
	"io"
	"monodb-backup/clog"
	"monodb-backup/config"
	"os"
	"path/filepath"
	"strings"
)

var logger *clog.CustomLogger = &clog.Logger
var params *config.Params = &config.Parameters

func copyFile(src, dst string) (int64, error) {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return 0, err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer destination.Close()
	nBytes, err := io.Copy(destination, source)
	return nBytes, err
}

func getFileExtension(path string) string {
	fileExt1 := filepath.Ext(path)
	path = strings.TrimSuffix(path, fileExt1)
	fileExt2 := filepath.Ext(path)
	return fileExt2 + fileExt1
}
