package backup

import (
	"bytes"
	"monodb-backup/config"
	"monodb-backup/notify"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var hostname, _ = os.Hostname()

type Dumper struct {
	p *config.Params
	l Logger
}

type rightNow struct {
	year  string
	month string
	now   string
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

func (d *Dumper) getDBList() ([]string, error) {
	psqlArgs := []string{"-lqt"}
	if d.p.Remote.IsRemote {
		pglink := "postgresql://" + d.p.Remote.User + ":" + d.p.Remote.Password + "@" + d.p.Remote.Host + ":" + d.p.Remote.Port
		psqlArgs = append(psqlArgs, pglink)
	}
	cmd := exec.Command("/usr/bin/psql", psqlArgs...)
	out, err := cmd.Output()
	if err != nil {
		d.l.Error("Could not get database list: " + err.Error())
		d.l.Error("Command output: " + string(out))
		return nil, err
	}

	var dbList []string
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		if len(line) > 0 {
			ln := strings.TrimSpace(strings.Split(string(line), "|")[0])
			if ln == "" || ln == "template0" || ln == "template1" || ln == "postgres" {
				continue
			}
			dbList = append(dbList, ln)
		}
	}
	return dbList, nil
}

func (d *Dumper) Dump() {
	d.reportLog("PostgreSQL database backup started.", false)

	if d.p.Remote.IsRemote {
		err := os.Setenv("PGPASSWORD", d.p.Remote.Password)
		if err != nil {
			d.l.Error("Couldn't set environment variable \"PGPASSWORD\"")
		}
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
			notify.SendAlarm("Couldn't backed up "+db+" - Error: "+err.Error(), true)
			subject = "Backup failed db: " + db
			message = "Couldn't backed up " + db + " - Error: " + err.Error()
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
				err = d.uploadS3(filePath, name)
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
				err = d.uploadMinIO(filePath, name)
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

	d.reportLog("PostgreSQL database backup finished.", false)
}

func (d *Dumper) dumpDB(db string, dst string) (string, string, error) {
	encrypted := d.p.ArchivePass != ""
	var dumpPath string
	var name string
	var format string
	var cmd *exec.Cmd
	if d.p.Format != "" {
		format = d.p.Format
	} else {
		format = "gzip"
	}
	d.l.Info("Backup started. DB: " + db + " - Compression algorithm: " + format + " - Encrypted: " + strconv.FormatBool(encrypted))

	var pgDumpArgs []string
	if d.p.Remote.IsRemote {
		pglink := "postgresql://" + d.p.Remote.User + ":" + d.p.Remote.Password + "@" + d.p.Remote.Host + ":" + d.p.Remote.Port + "/" + db
		pgDumpArgs = append(pgDumpArgs, pglink)
	} else {
		pgDumpArgs = append(pgDumpArgs, db)
	}

	date := rightNow{
		year:  time.Now().Format("2006"),
		month: time.Now().Format("01"),
		now:   time.Now().Format("2006-01-02-150405"),
	}
	_ = os.MkdirAll(dst+"/"+date.year+"/"+date.month, os.ModePerm)

	if !encrypted {
		if format == "gzip" {
			name = date.year + "/" + date.month + "/" + db + "-" + date.now + ".dump"
			dumpPath = dst + "/" + name
			pgDumpArgs = append(pgDumpArgs, "-Fc", "-f", dumpPath)
			cmd = exec.Command("/usr/bin/pg_dump", pgDumpArgs...)
			err := cmd.Run()
			if err != nil {
				d.l.Error("Couldn't backed up " + db + " - Error: " + err.Error())
				return "", "", err
			}
		} else if format == "7zip" {
			name = date.year + "/" + date.month + "/" + db + "-" + date.now + ".sql.7z"
			dumpPath = dst + "/" + name
			cmd = exec.Command("/usr/bin/pg_dump", pgDumpArgs...)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				d.l.Error("Couldn't backed up " + db + " - Error: " + err.Error())
				return "", "", err
			}
			err = cmd.Start()
			if err != nil {
				d.l.Error("Couldn't backed up " + db + " - Error: " + err.Error())
				return "", "", err
			}
			cmd2 := exec.Command("7z", "a", "-t7z", "-ms=on", "-si", dumpPath)
			cmd2.Stdin = stdout

			err = cmd2.Run()
		}
	} else {
		if format == "gzip" {
			name = date.year + "/" + date.month + "/" + db + "-" + date.now + ".dump.7z"
			dumpPath = dst + "/" + name
			pgDumpArgs = append(pgDumpArgs, "-Fc")
			cmd = exec.Command("/usr/bin/pg_dump", pgDumpArgs...)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				d.l.Error("Couldn't backed up " + db + " - Error: " + err.Error())
				return "", "", err
			}
			err = cmd.Start()
			if err != nil {
				d.l.Error("Couldn't backed up " + db + " - Error: " + err.Error())
				return "", "", err
			}
			cmd2 := exec.Command("7z", "a", "-t7z", "-mx0", "-mhe=on", "-p"+d.p.ArchivePass, "-si", dumpPath)
			cmd2.Stdin = stdout

			err = cmd2.Run()
		} else if format == "7zip" {
			name = date.year + "/" + date.month + "/" + db + "-" + date.now + ".sql.7z"
			dumpPath = dst + "/" + name
			cmd = exec.Command("/usr/bin/pg_dump", pgDumpArgs...)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				d.l.Error("Couldn't backed up " + db + " - Error: " + err.Error())
				return "", "", err
			}
			err = cmd.Start()
			if err != nil {
				d.l.Error("Couldn't backed up " + db + " - Error: " + err.Error())
				return "", "", err
			}
			cmd2 := exec.Command("7z", "a", "-t7z", "-ms=on", "-mhe=on", "-p"+d.p.ArchivePass, "-si", dumpPath)
			cmd2.Stdin = stdout

			err = cmd2.Run()
		}
	}
	d.l.Info("Successfully backed up " + db + " at: " + dumpPath)
	return dumpPath, name, nil
}

func (d *Dumper) uploadS3(filePath, name string) error {
	uploader, err := newS3Uploader(d.p.S3.Region, d.p.S3.AccessKey, d.p.S3.SecretKey)
	if err != nil {
		return err
	} else {
		target := name
		if d.p.S3.Path != "" {
			target = d.p.S3.Path + "/" + target
		}
		err = uploadFileToS3(uploader, filePath, d.p.S3.Bucket, target)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *Dumper) uploadMinIO(filePath, name string) error {
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
		err = uploadFileToMinio(minioClient, filePath, d.p.Minio.Bucket, target)
		if err != nil {
			return err
		}
	}

	return nil
}
