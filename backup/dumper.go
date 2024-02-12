package backup

import (
	"monodb-backup/config"
	"monodb-backup/notify"
	"os"
	"time"
)

var hostname, _ = os.Hostname()

type Dumper struct {
	p *config.Params
	l Logger
}

type rightNow struct {
	year   string
	month  string
	day    string
	hour   string
	minute string
	now    string
}

var dateNow rightNow

func dumpName(db string, params config.Rotation) string {
	if !params.Enabled {
		date := rightNow{
			year:  time.Now().Format("2006"),
			month: time.Now().Format("01"),
			now:   time.Now().Format("2006-01-02-150405"),
		}
		name := date.year + "/" + date.month + "/" + db + "-" + date.now
		return name
	} else {
		suffix := params.Suffix
		switch suffix {
		case "day":
			return "Daily/" + db + "-" + dateNow.day
		case "hour":
			return "Hourly/" + dateNow.day + db + "-" + dateNow.hour
		case "minute":
			return "Custom/" + dateNow.day + db + "-" + dateNow.minute
		default:
			return "Daily/" + db + "-" + dateNow.day
		}
	}
}

// NewDumper creates a new Dumper instance.
func NewDumper(params *config.Params, logger Logger) (d *Dumper) {
	d = &Dumper{
		p: params,
		l: logger,
	}
	return
}

func (d *Dumper) reportLog(message string, isError bool) {
	d.l.Info(message)
	if isError {
		notify.SendAlarm("[ERROR] monodb-backup at "+hostname+"\n"+message, isError)
	}
}

func (d *Dumper) getDBList() (dbList []string, err error) {
	if d.p.Database == "" || d.p.Database == "postgresql" {
		dbList, err = getPSQLList(d.p.Remote, d.l)
	} else if d.p.Database == "mysql" {
		dbList, err = getMySQLList(d.p.Remote, d.l)
	}
	return
}

func (d *Dumper) Dump() {
	d.reportLog("Database backup started.", false)

	dateNow = rightNow{
		day:    time.Now().Format("Mon"),
		hour:   time.Now().Format("Mon-15"),
		minute: time.Now().Format("Mon-15_04"),
		now:    time.Now().Format("2006-01-02-150405"),
	}

	if len(d.p.Databases) == 0 {
		d.reportLog("Getting database list...", false)
		databases, err := d.getDBList()
		if err != nil {
			d.reportLog(err.Error(), true)
		}
		d.p.Databases = databases
	}

	for _, db := range d.p.Databases {
		var subject string
		var message string

		filePath, name, err := d.dumpDB(db, d.p.BackupDestination)
		if err != nil {
			notify.SendAlarm("Couldn't back up "+db+" - Error: "+err.Error(), true)
			subject = "Backup failed db: " + db
			message = "Couldn't back up " + db + " - Error: " + err.Error()
			err := notify.Email(d.p, subject, message, true)
			if err != nil {
				d.l.Error("Couldn't send notification mail - Error: " + err.Error())
			}
		} else {
			d.l.Info("Successfully backed up " + db + " at " + filePath)
			notify.SendAlarm("Successfully backed up "+db+" at "+filePath, false)
			subject = "Successfully backed up db: " + db
			message = "Successfully backed up " + db + " at " + filePath
			err := notify.Email(d.p, subject, message, false)
			if err != nil {
				d.l.Error("Couldn't send notification mail - Error: " + err.Error())
			}

			if d.p.S3.Enabled {
				err = d.uploadS3(filePath, name, db)
				if err != nil {
					d.l.Error("Couldn't upload " + filePath + " to S3" + " - Error: " + err.Error())
					notify.SendAlarm("Couldn't upload "+filePath+" to S3"+" - Error: "+err.Error(), true)
					subject = "Couldn't upload db: " + db + " to S3"
					message = "Couldn't upload " + name + " at " + filePath + " to S3" + " - Error: " + err.Error()
					err := notify.Email(d.p, subject, message, true)
					if err != nil {
						d.l.Error("Couldn't send notification mail - Error: " + err.Error())
					}
				} else {
					d.l.Info("Successfully uploaded " + filePath + " to S3")
					notify.SendAlarm("Successfully uploaded "+filePath+" to S3", false)
					subject = "Successfully upload db: " + db + " to S3"
					message = "Successfully uploaded " + db + " at " + filePath + " to S3."
					err = notify.Email(d.p, subject, message, false)
					if err != nil {
						d.l.Error("Couldn't send notification mail - Error: " + err.Error())
					}
				}
			}

			if d.p.Minio.Enabled {
				err = d.uploadMinIO(filePath, name, db)
				if err != nil {
					d.l.Error("Couldn't upload " + filePath + " to MinIO" + " - Error: " + err.Error())
					notify.SendAlarm("Couldn't upload "+filePath+" to MinIO"+" - Error: "+err.Error(), true)
					subject = "Couldn't upload db: " + db + " to MinIO"
					message = "Couldn't upload " + name + " at " + filePath + " to MinIO" + " - Error: " + err.Error()
					err := notify.Email(d.p, subject, message, true)
					if err != nil {
						d.l.Error("Couldn't send notification mail - Error: " + err.Error())
					}
				} else {
					d.l.Info("Successfully uploaded " + filePath + " to MinIO")
					notify.SendAlarm("Successfully uploaded "+filePath+" to MinIO", false)
					subject = "Successfully upload db: " + db + " to S3"
					message = "Successfully uploaded " + db + " at " + filePath + " to MinIO."
					err = notify.Email(d.p, subject, message, false)
					if err != nil {
						d.l.Error("Couldn't send notification mail - Error: " + err.Error())
					}
				}
			}
		}

		if d.p.RemoveLocal {
			err = os.Remove(filePath)
			if err != nil {
				d.l.Error("Couldn't delete dump file at" + filePath + " - Error: " + err.Error())
			} else {
				d.l.Info("Dump file at" + filePath + " successfully deleted.")
			}
		}
	}

	d.reportLog("Database backup finished.", false)
}

func (d *Dumper) dumpDB(db string, dst string) (dumpPath string, name string, err error) {
	if d.p.Database == "" || d.p.Database == "postgresql" {
		dumpPath, name, err = dumpPSQLDb(db, dst, *d.p, d.l)
	} else if d.p.Database == "mysql" {
		dumpPath, name, err = dumpMySQLDb(db, dst, *d.p, d.l)
	}
	return
}

func (d *Dumper) uploadS3(filePath, name, db string) error {
	uploader, err := newS3Uploader(d.p.S3.Region, d.p.S3.AccessKey, d.p.S3.SecretKey)
	if err != nil {
		return err
	} else {
		target := name
		if d.p.S3.Path != "" {
			target = d.p.S3.Path + "/" + target
		}
		err = uploadFileToS3(uploader, filePath, d.p.S3.Bucket, target, d.p.Rotation, db, d.p.S3.Path)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *Dumper) uploadMinIO(filePath, name, db string) error {
	minioClient, err := newMinioClient(
		d.p.Minio.Endpoint,
		d.p.Minio.AccessKey,
		d.p.Minio.SecretKey,
		d.p.Minio.Secure,
		d.p.Minio.InsecureSkipVerify)
	if err != nil {
		return err
	} else {
		target := name
		if d.p.Minio.Path != "" {
			target = d.p.Minio.Path + "/" + target
		}
		err = uploadFileToMinio(minioClient, d.p.Rotation, db, filePath, d.p.Minio.Bucket, target, d.p.Minio.Path)
		if err != nil {
			return err
		}
	}

	return nil
}
