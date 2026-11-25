package backup

import (
	"fmt"
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
