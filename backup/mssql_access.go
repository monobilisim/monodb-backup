package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func dirsFrom(root, dir string) ([]string, error) {
	root, dir = filepath.Clean(root), filepath.Clean(dir)
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%s is not inside %s", dir, root)
	}
	dirs := []string{root}
	if rel == "." {
		return dirs, nil
	}
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		dirs = append(dirs, cur)
	}
	return dirs, nil
}

func statDestination(root string) (os.FileInfo, error) {
	const hint = "backupDestination %q must be an existing directory owned by the SQL Server service account"
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf(hint+": %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf(hint, root)
	}
	return info, nil
}
