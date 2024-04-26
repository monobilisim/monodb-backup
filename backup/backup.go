package backup

import (
	"monodb-backup/notify"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func getDBList() (dbList []string) {
	if params.Database == "" || params.Database == "postgresql" {
		dbList = getPSQLList()
	} else if params.Database == "mysql" {
		dbList = getMySQLList()
	}
	return
}

func dumpDB(db string, dst string) (dumpPath string, name string, err error) {
	if params.Database == "" || params.Database == "postgresql" {
		dumpPath, name, err = dumpPSQLDb(db, dst)
	} else if params.Database == "mysql" {
		dumpPath, name, err = dumpMySQLDb(db, dst)
	}
	return
}

func Backup() {
	logger.Info("monodb-backup job started.")
	notify.SendAlarm("Database backup started.", false)

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

	if params.Minio.S3FS.ShouldMount {
		mountMinIO(params.Minio)
		defer umountMinIO(params.Minio)

		dst := params.Minio.S3FS.MountPath
		if params.Minio.Path != "" {
			dst = dst + "/" + params.Minio.Path
		}
		err := os.MkdirAll(dst+"/"+minioPath(), os.FileMode(0750))
		if err != nil {
			notify.SendAlarm("Couldn't create folder in MinIO at path: "+dst+" - Error: "+err.Error(), true)
			logger.Fatal("Couldn't create folder in MinIO at path: " + dst + " - Error: " + err.Error())
			return
		}
		if params.Rotation.Enabled {
			dst = dst + "/" + minioPath()
		}
		for _, db := range params.Databases {
			if !params.BackupAsTables {
				filePath, fileName, err := dumpDB(db, dst)
				if err != nil {
					notify.SendAlarm("Problem during backing up "+db+" - Error: "+err.Error(), true)
					err = os.Remove(filePath)
					if err != nil {
						logger.Error("Couldn't delete faulty dump file at " + filePath + " - Error: " + err.Error())
					} else {
						logger.Info("Faulty dump file at " + filePath + " successfully deleted.")
					}
				} else {
					logger.Info("Successfully backed up database:" + db + " at " + filePath)
					notify.SendAlarm("Successfully backed up "+db+" at "+filePath, false)

					if params.Rotation.Enabled {
						shouldRotate, name := rotate(db)
						if shouldRotate {
							var newDst string
							if params.Minio.Path != "" {
								newDst = params.Minio.Path
							}
							newDst = params.Minio.S3FS.MountPath + "/" + newDst
							newDst = strings.TrimSuffix(newDst, "/")
							err := os.MkdirAll(strings.TrimSuffix(newDst, "/")+"/"+rotatePath(), os.FileMode(0750))
							if err != nil {
								notify.SendAlarm("Couldn't create folder in MinIO at path: "+dst+" - Error: "+err.Error(), true)
								logger.Fatal("Couldn't create folder in MinIO at path: " + dst + " - Error: " + err.Error())
								return
							}
							extension := getFileExtension(fileName)
							name = newDst + "/" + name + extension
							_, err = copyFile(filePath, name)
							if err != nil {
								logger.Error("Couldn't create a copy of " + filePath + " for rotation\npath: " + name + "\n Error: " + err.Error())
								notify.SendAlarm("Couldn't create a copy of "+filePath+" for rotation\npath: "+name+"\n Error: "+err.Error(), true)
							} else {
								logger.Info("Successfully created a copy of " + filePath + " for rotation\npath: " + name)
								notify.SendAlarm("Successfully created a copy of "+filePath+" for rotation\npath: "+name, false)
							}
						}
					}
				}
			} else {
				dumpPaths, names, err := dumpDBWithTables(db, dst)
				if err != nil {
					notify.SendAlarm("Problem during backing up "+db+" - Error: "+err.Error(), true)
					for _, filePath := range dumpPaths {
						err = os.Remove(filePath)
						if err != nil {
							logger.Error("Couldn't delete dump file at " + filePath + " - Error: " + err.Error())
						} else {
							logger.Info("Dump file at " + filePath + " successfully deleted.")
						}
					}
				} else {
					for i, filePath := range dumpPaths {
						fileName := names[i]
						logger.Info("Successfully backed up database:" + db + " at " + filePath)
						notify.SendAlarm("Successfully backed up "+db+" at "+filePath, false)

						if params.Rotation.Enabled {
							name := filepath.Base(fileName)
							extension := getFileExtension(fileName)
							shouldRotate, name := rotate(strings.TrimSuffix(name, extension))
							newPath := filepath.Dir(name)
							baseName := filepath.Base(name)
							name = newPath + "/" + db + "/" + baseName
							if shouldRotate {
								var newDst string
								if params.Minio.Path != "" {
									newDst = params.Minio.Path
								}
								newDst = params.Minio.S3FS.MountPath + "/" + newDst
								newDst = strings.TrimSuffix(newDst, "/")
								err := os.MkdirAll(strings.TrimSuffix(newDst, "/")+"/"+rotatePath()+"/"+db, os.FileMode(0750))
								if err != nil {
									notify.SendAlarm("Couldn't create folder in MinIO at path: "+dst+" - Error: "+err.Error(), true)
									logger.Fatal("Couldn't create folder in MinIO at path: " + dst + " - Error: " + err.Error())
									return
								}
								name = newDst + "/" + name + extension
								_, err = copyFile(filePath, name)
								if err != nil {
									logger.Error("Couldn't create a copy of " + filePath + " for rotation\npath: " + name + "\n Error: " + err.Error())
									notify.SendAlarm("Couldn't create a copy of "+filePath+" for rotation\npath: "+name+"\n Error: "+err.Error(), true)
								} else {
									logger.Info("Successfully created a copy of " + filePath + " for rotation\npath: " + name)
									notify.SendAlarm("Successfully created a copy of "+filePath+" for rotation\npath: "+name, false)
								}
							}
						}
					}
				}
			}
		}
	} else {
		for _, db := range params.Databases {
			if params.BackupAsTables && db != "mysql" {
				dumpPaths, names, err := dumpDBWithTables(db, params.BackupDestination)
				if err != nil {
					notify.SendAlarm("Problem during backing up "+db+" - Error: "+err.Error(), true)
				} else {
					logger.Info("Successfully backed up database:" + db + " with its tables separately, at " + params.BackupDestination + "/" + db)
					notify.SendAlarm("Successfully backed up "+db+" at "+params.BackupDestination+"/"+db, false)
					for i, filePath := range dumpPaths {
						name := names[i]
						upload(name, db, filePath)
					}
				}
				if params.RemoveLocal {
					err = os.RemoveAll(params.BackupDestination + "/" + db)
					if err != nil {
						logger.Error("Couldn't delete dump file at " + params.BackupDestination + "/" + db + " - Error: " + err.Error())
					} else {
						logger.Info("Dump file at " + params.BackupDestination + "/" + db + " successfully deleted.")
					}
				}
			} else {
				filePath, name, err := dumpDB(db, params.BackupDestination)
				if err != nil {
					notify.SendAlarm("Problem during backing up "+db+" - Error: "+err.Error(), true)
				} else {
					logger.Info("Successfully backed up database:" + db + " at " + filePath)
					notify.SendAlarm("Successfully backed up "+db+" at "+filePath, false)

					upload(name, db, filePath)
				}
				if params.RemoveLocal {
					err = os.Remove(filePath)
					if err != nil {
						logger.Error("Couldn't delete dump file at " + filePath + " - Error: " + err.Error())
					} else {
						logger.Info("Dump file at " + filePath + " successfully deleted.")
					}
				}
			}
		}
	}
	logger.Info("monodb-backup job finished.")
	notify.SendAlarm("monodb-backup job finished.", false)
}

func upload(name, db, filePath string) {
	if params.S3.Enabled {
		target := nameWithPath(name)
		if params.S3.Path != "" {
			target = params.S3.Path + "/" + target
		}
		uploadFileToS3(filePath, target, db)
	}

	if params.Minio.Enabled {
		target := nameWithPath(name)
		if params.Minio.Path != "" {
			target = params.Minio.Path + "/" + target
		}
		uploadFileToMinio(filePath, target, db)
	}

	if params.SFTP.Enabled {
		for _, target := range params.SFTP.Targets {
			SendSFTP(filePath, name, db, target)
		}
	}
	if params.Rsync.Enabled {
		for _, target := range params.Rsync.Targets {
			SendRsync(filePath, name, target)
		}
	}
}
