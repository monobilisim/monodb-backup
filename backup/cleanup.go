package backup

import (
	"sort"
	"time"
)

type BackupFile struct {
	Name string
	Time time.Time
	Path string
}

type ByTime []BackupFile

func (a ByTime) Len() int           { return len(a) }
func (a ByTime) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByTime) Less(i, j int) bool { return a[i].Time.After(a[j].Time) }

func getFilesToDelete(files []BackupFile, period string, keep int) []BackupFile {
	var toDelete []BackupFile
	if keep == 0 {
		return toDelete
	}

	if period == "daily" {
		cutoff := time.Now().AddDate(0, 0, -keep)
		for _, f := range files {
			if f.Time.Before(cutoff) {
				toDelete = append(toDelete, f)
			}
		}
	} else {
		if len(files) > keep {
			sort.Sort(ByTime(files))
			toDelete = append(toDelete, files[keep:]...)
		}
	}
	return toDelete
}
