package backup

import (
	"context"
	"io"
	"monodb-backup/notify"
	"os"
	"strconv"
	"strings"
	"time"
)

var didItWork bool = true // this variable is for determining if the app should send a notification after a failed backup to inform that it works now
func itWorksNow(message string, worked bool) {
	oldOnlyOnError := params.Notify.Webhook.OnlyOnError
	if !didItWork && worked && oldOnlyOnError {
		params.Notify.Webhook.OnlyOnError = false
		notify.SendAlarm(message, false)
		params.Notify.Webhook.OnlyOnError = oldOnlyOnError
	}
	didItWork = worked
}

func getDBList() (dbList []string) {
	switch params.Database {
	case "postgresql":
		dbList = getPSQLList()
	case "mysql":
		dbList = getMySQLList()
	case "mssql":
		dbList = getMSSQLList()
	case "oracle":
		return getOracleList()
	default:
		dbList = getPSQLList()
	}
	return
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
	notify.SendAlarm("Database backup started.", false)
	streamable := (params.Database == "" || params.Database == "postgresql" || (params.Database == "mysql" && !params.BackupAsTables)) && params.ArchivePass == ""

	dateNow = rightNow{
		day:    time.Now().Format("Mon"),
		hour:   time.Now().Format("Mon-15"),
		minute: time.Now().Format("Mon-15_04"),
		now:    time.Now().Format("2006-01-02-150405"),
	}

	if len(params.Databases) == 0 {
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

	if streamable {
		for _, db := range params.Databases {
			uploadWhileDumping(db)
		}
		return
	}
	for _, db := range params.Databases {
		dst := strings.TrimSuffix(params.BackupDestination, "/") + "/" + db
		if params.BackupAsTables && db != "mysql" {
			dumpPaths, names, err := dumpDBWithTables(db, dst)
			if err != nil {
				notify.SendAlarm("Problem during backing up "+db+" - Error: "+err.Error(), true)
				itWorksNow("", false)
			} else {
				logger.Info("Successfully backed up database:" + db + " with its tables separately, at " + params.BackupDestination + "/" + db)
				notify.SendAlarm("Successfully backed up "+db+" at "+params.BackupDestination+"/"+db, false)
				for i, filePath := range dumpPaths {
					name := names[i]
					upload(name, db, filePath)
				}
			}

		} else {
			filePath, name, err := dumpDB(db, dst)
			if err != nil {
				notify.SendAlarm("Problem during backing up "+db+" - Error: "+err.Error(), true)
				itWorksNow("", false)
			} else {
				logger.Info("Successfully backed up database:" + db + " at " + filePath)
				notify.SendAlarm("Successfully backed up "+db+" at "+filePath, false)

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
		}
	}
}

func uploadWhileDumping(db string) {
	ctx, cancel := context.WithCancel(context.Background())
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
			uploadDone[i] <- err
			close(uploadDone[i])
		}(i, instance)
	}

	err := dumpAndUpload(db, pipeWriters)
	if err != nil {
		cancel()
		notify.SendAlarm("Problem during backing up "+db+" - Error: "+err.Error(), true)
		itWorksNow("", false)
	}
	for _, writer := range pipeWriters {
		writer.Close()
	}
	for i, channel := range uploadDone {
		uploadErr := <-channel
		if uploadErr != nil {
			logger.Error(strconv.Itoa(i+1) + ") " + db + " - " + "Couldn't upload to S3: " + uploaders[i].instance.Endpoint + "  - Error: " + uploadErr.Error())
			notify.SendAlarm(strconv.Itoa(i+1)+") "+db+" - "+"Couldn't upload to S3: "+uploaders[i].instance.Endpoint+"  - Error: "+uploadErr.Error(), true)
			itWorksNow("", false)
		} else {
			logger.Info(strconv.Itoa(i+1) + ") " + db + " - " + "Successfully uploaded to S3: " + uploaders[i].instance.Endpoint)
			message := strconv.Itoa(i+1) + ") " + db + " - " + "Successfully uploaded to S3: " + uploaders[i].instance.Endpoint
			notify.SendAlarm(message, false)
			itWorksNow(message, true)
		}
	}
}

func upload(name, db, filePath string) {
	var err error
	switch params.BackupType.Type {
	case "s3", "minio":
		err = uploadToS3(filePath, name, db)
		if err != nil {
			itWorksNow("", false)
		}
	case "sftp":
		for _, target := range params.BackupType.Info[0].Targets {
			err = SendSFTP(filePath, name, db, target)
			if err != nil {
				itWorksNow("", false)
			}
		}
	case "rsync":
		for _, target := range params.BackupType.Info[0].Targets {
			err = SendRsync(filePath, name, target)
			if err != nil {
				itWorksNow("", false)
			}
		}
	}
}
