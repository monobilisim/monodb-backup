package backup

import (
	"context"
	"fmt"
	"io"
	"monodb-backup/notify"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	appStartTime       time.Time
	currentDB          string
	currentDBStartTime time.Time
	mu                 sync.Mutex // Mutex to protect access to currentDB and currentDBStartTime
)

var Retrying = false
var FailedDBNames []string

func init() {
	appStartTime = time.Now()
}

func SendHourlyUptimeStatus() {
	mu.Lock()
	db := currentDB
	dbStart := currentDBStartTime
	mu.Unlock()

	totalUptime := time.Since(appStartTime).Round(time.Second)
	message := fmt.Sprintf("Uptime: %s. ", totalUptime)

	if db != "" {
		dbUptime := time.Since(dbStart).Round(time.Second)
		message += fmt.Sprintf("Currently backing up: %s (started %s ago).", db, dbUptime)
	} else {
		message += "Currently idle."
	}

	logger.Info("Hourly Status: ", message)
	if totalUptime.Hours() > float64(params.Notify.UptimeStartLimit) {
		notify.SendAlarm(message, true)
	}
}

func getDBList() []string {
	if Retrying {
		dbList := FailedDBNames
		notify.FailedDBList = nil
		return dbList
	}
	switch params.Database {
	case "postgresql":
		return getPSQLList()
	case "mysql":
		return getMySQLList()
	case "mssql":
		return getMSSQLList()
	case "oracle":
		return getOracleList()
	default:
		return getPSQLList()
	}
}

func dumpAndUpload(db string, pipeWriters []*io.PipeWriter) error {
	switch params.Database {
	case "postgresql":
		return dumpAndUploadPSQL(db, pipeWriters)
	case "mysql":
		return dumpAndUploadMySQL(db, pipeWriters)
	default:
		return dumpAndUploadPSQL(db, pipeWriters)
	}
}

func dumpDB(db string, dst string) (string, string, error) {
	switch params.Database {
	case "postgresql":
		return dumpPSQLDb(db, dst)
	case "mysql":
		return dumpMySQLDb(db, dst)
	case "mssql":
		return dumpMSSQLDB(db, dst)
	//case "oracle":
	//	return dumpOracleDB(db, dst)
	default:
		return dumpPSQLDb(db, dst)
	}
}

func Backup() {
	logger.Info("monodb-backup job started.")
	mu.Lock()
	currentDB = ""
	mu.Unlock()

	streamable := (params.Database == "" || params.Database == "postgresql" || (params.Database == "mysql" && !params.BackupAsTables)) && params.ArchivePass == ""

	dateNow = rightNow{
		day:    time.Now().Format("Mon"),
		hour:   time.Now().Format("Mon-15"),
		minute: time.Now().Format("Mon-15_04"),
		now:    time.Now().Format("2006-01-02-150405"),
	}

	if len(params.Databases) == 0 || Retrying {
		logger.Info("Getting database list...")
		databases := getDBList()
		params.Databases = databases
	}

	if len(params.Exclude) != 0 {
		excludeMap := make(map[string]bool)
		for _, item := range params.Exclude {
			excludeMap[item] = true
		}

		tmpDatabases := make([]string, 0, len(params.Databases))
		for _, item := range params.Databases {
			if !excludeMap[item] {
				tmpDatabases = append(tmpDatabases, item)
			}
		}
		params.Databases = tmpDatabases
	}

	if streamable && (params.BackupType.Type == "minio" || params.BackupType.Type == "s3") {
		for _, db := range params.Databases {
			mu.Lock()
			currentDB = db
			currentDBStartTime = time.Now()
			mu.Unlock()
			uploadWhileDumping(db)
		}
		mu.Lock()
		currentDB = ""
		mu.Unlock()
		logger.Info("monodb-backup streamable job finished.")
		return
	}
	for _, db := range params.Databases {
		mu.Lock()
		currentDB = db
		currentDBStartTime = time.Now()
		mu.Unlock()
		var dst string
		if runtime.GOOS == "windows" {
			dst = strings.TrimSuffix(params.BackupDestination, "/") + db
		} else {
			dst = strings.TrimSuffix(params.BackupDestination, "/") + "/" + nameWithPath(db)
		}
		fullPath := strings.Split(dst, "/")
		dst = fullPath[0]
		for i := 1; i < len(fullPath)-1; i++ {
			dst = dst + "/" + fullPath[i]
		}
		if params.BackupAsTables && db != "mysql" {
			dumpPaths, names, err := dumpDBWithTables(db, dst)
			if err != nil {
				// notify.SendAlarm("Problem during backing up "+db+" - Error: "+err.Error(), true)
				// itWorksNow("", false)
			} else {
				logger.Info("Successfully backed up database:" + db + " with its tables separately, at " + params.BackupDestination + "/" + db)
				// notify.SendAlarm("Successfully backed up "+db+" at "+params.BackupDestination+"/"+db, false)
				for i, filePath := range dumpPaths {
					name := names[i]
					upload(name, db, filePath)
				}
			}

		} else {
			filePath, name, err := dumpDB(db, dst)
			if err != nil {
				// notify.SendAlarm("Problem during backing up "+db+" - Error: "+err.Error(), true)
				notify.FailedDBList = append(notify.FailedDBList, db+" - Error: "+err.Error())
				FailedDBNames = append(FailedDBNames, db)
				// itWorksNow("", false)
			} else {
				logger.Info("Successfully backed up database:" + db + " at " + filePath)
				// notify.SendAlarm("Successfully backed up "+db+" at "+filePath, false)
				notify.SuccessfulDBList = append(notify.SuccessfulDBList, db)

				upload(name, db, filePath)
			}
		}
		if params.RemoveLocal {
			err := os.RemoveAll(dst)
			if err != nil {
				logger.Error("Couldn't delete dump file at " + params.BackupDestination + "/" + db + " - Error: " + err.Error())
			} else {
				logger.Info("Dump file at " + params.BackupDestination + "/" + db + " successfully deleted.")
			}
		} else {
			if params.Rotation.Keep.Daily > 0 || params.Rotation.Keep.Weekly > 0 || params.Rotation.Keep.Monthly > 0 {
				if err := cleanupLocal(); err != nil {
					logger.Error("Error during local cleanup for " + db + ": " + err.Error())
				}
			}
		}
	}
	mu.Lock()
	currentDB = ""
	mu.Unlock()
	if params.Database == "mssql" {
		mssqlDB.Close()
	}
	logger.Info("monodb-backup non-streamable job finished.")
}

func uploadWhileDumping(db string) {
	logger.Info("Backup started for " + db)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(params.CtxCancel)*time.Hour)
	defer cancel()
	var name string
	if db == "mysql" {
		name = nameWithPath(dumpName(db+"_users", params.Rotation, ""))
	} else {
		name = nameWithPath(dumpName(db, params.Rotation, ""))
	}
	switch params.Database {
	case "postgresql":
		name = name + ".dump"
	case "mysql":
		name = name + ".sql.gz"
	default:
		name = name + ".dump"
	}
	var pipeWriters []*io.PipeWriter
	var uploadDone []chan error
	for i, instance := range uploaders {
		dumpPath := instance.instance.Path + "/" + name
		pipeReader, pipeWriter := io.Pipe()
		pipeWriters = append(pipeWriters, pipeWriter)
		uploadDone = append(uploadDone, make(chan error))
		go func(i int, instance uploaderStruct) {

			err := uploadFileToS3(ctx, "", dumpPath, db, pipeReader, &instance)
			pipeWriter.Close()
			uploadDone[i] <- err
			close(uploadDone[i])
		}(i, instance)
	}

	err := dumpAndUpload(db, pipeWriters)
	if err != nil {
		logger.Error("Error during dump of " + db + " - Error: " + err.Error())
		for _, writer := range pipeWriters {
			writer.Close()
		}
		notify.FailedDBList = append(notify.FailedDBList, db+" - Dump Error: "+err.Error())
		FailedDBNames = append(FailedDBNames, db)
		return
	}

	for _, writer := range pipeWriters {
		writer.Close()
	}

	for i, channel := range uploadDone {
		select {
		case uploadErr := <-channel:
			if uploadErr != nil {
				logger.Error(strconv.Itoa(i+1) + ") " + db + " - " + "Couldn't upload to S3: " + uploaders[i].instance.Endpoint + "  - Error: " + uploadErr.Error())
				notify.FailedDBList = append(notify.FailedDBList, db+" to "+uploaders[i].instance.Endpoint+" - Error: "+uploadErr.Error())
				FailedDBNames = append(FailedDBNames, db)
			} else {
				message := strconv.Itoa(i+1) + ") " + db + " - " + "Successfully uploaded to S3: " + uploaders[i].instance.Endpoint
				logger.Info(message)
				notify.SuccessfulDBList = append(notify.SuccessfulDBList, db+" to "+uploaders[i].instance.Endpoint)
			}
		case <-ctx.Done():
			logger.Error(strconv.Itoa(i+1) + ") " + db + " - Upload timed out or was cancelled")
			notify.FailedDBList = append(notify.FailedDBList, db+" to "+uploaders[i].instance.Endpoint+" - Error: timeout")
			FailedDBNames = append(FailedDBNames, db)
		}
	}
}

func upload(name, db, filePath string) {
	var err error
	switch params.BackupType.Type {
	case "s3", "minio":
		uploadToS3(filePath, name, db)
	case "sftp":
		for _, target := range params.BackupType.Info[0].Targets {
			err = SendSFTP(filePath, name, db, target)
			if err != nil {
				// itWorksNow("", false)
				notify.FailedDBList = append(notify.FailedDBList, db+" - "+name+" - Error: "+err.Error())
				FailedDBNames = append(FailedDBNames, db)
			} else {
				notify.SuccessfulDBList = append(notify.SuccessfulDBList, db+" - "+name)
			}
		}
	case "rsync":
		for _, target := range params.BackupType.Info[0].Targets {
			message, err := SendRsync(filePath, name, db, target)
			if err != nil {
				// itWorksNow("", false)
				notify.FailedDBList = append(notify.FailedDBList, db+" - "+message)
				FailedDBNames = append(FailedDBNames, db)
			} else {
				notify.SuccessfulDBList = append(notify.SuccessfulDBList, db+" - "+name)
			}
		}
	}
}

func cleanupLocal() error {
	root := strings.TrimSuffix(params.BackupDestination, "/")

	cleanupDir := func(dir string, keep int, period string) error {
		if keep == 0 {
			return nil
		}
		path := filepath.Join(root, dir)
		entries, err := os.ReadDir(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		var backups []BackupFile
		var recurseDirs []string

		for _, e := range entries {
			if e.IsDir() {
				recurseDirs = append(recurseDirs, e.Name())
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}

			backups = append(backups, BackupFile{
				Name: e.Name(),
				Time: info.ModTime(),
				Path: filepath.Join(path, e.Name()),
			})
		}

		for _, subDir := range recurseDirs {
			subPath := filepath.Join(path, subDir)
			subEntries, err := os.ReadDir(subPath)
			if err != nil {
				continue
			}

			for _, se := range subEntries {
				if se.IsDir() {
					continue
				}
				info, err := se.Info()
				if err != nil {
					continue
				}

				backups = append(backups, BackupFile{
					Name: se.Name(),
					Time: info.ModTime(),
					Path: filepath.Join(subPath, se.Name()),
				})
			}
		}

		var toDelete []BackupFile

		re := regexp.MustCompile(`(.+)-(week_\d+|Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec|Mon|Tue|Wed|Thu|Fri|Sat|Sun)`)

		grouped := make(map[string][]BackupFile)
		for _, f := range backups {
			filename := f.Name

			dbName := ""
			matches := re.FindStringSubmatch(filename)
			if len(matches) > 1 {
				dbName = matches[1]
			} else {
				dbName = filename
			}
			grouped[dbName] = append(grouped[dbName], f)
		}

		for _, group := range grouped {
			toDelete = append(toDelete, getFilesToDelete(group, period, keep)...)
		}
		for _, f := range toDelete {
			err := os.Remove(f.Path)
			if err != nil {
				logger.Error("Failed to delete old local backup " + f.Path + ": " + err.Error())
			} else {
				logger.Info("Deleted old local backup: " + f.Path)
			}
		}
		return nil
	}

	if params.Rotation.Keep.Daily > 0 {
		if err := cleanupDir("Daily", params.Rotation.Keep.Daily, "daily"); err != nil {
			return err
		}
	}
	if params.Rotation.Keep.Weekly > 0 {
		if err := cleanupDir("Weekly", params.Rotation.Keep.Weekly, "weekly"); err != nil {
			return err
		}
	}
	if params.Rotation.Keep.Monthly > 0 {
		if err := cleanupDir("Monthly", params.Rotation.Keep.Monthly, "monthly"); err != nil {
			return err
		}
	}

	return nil
}
