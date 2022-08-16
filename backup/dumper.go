package backup

import (
	"os"
	"os/exec"
	"pgsql-backup/config"
	"pgsql-backup/notify"
	"time"
)

type Dumper struct {
	p *config.Params
	m *notify.Mattermost
}

type backupStatus struct {
	s3 uploadStatus
	minio uploadStatus
}

type uploadStatus struct {
	enabled bool
	success bool
	msg string
}

func NewDumper(params *config.Params) (d *Dumper) {
	d = &Dumper{
		p: params,
		m: notify.NewMattermost(params),
	}
	return
}

func (d *Dumper) Dump() {
	d.m.Notify("PostgreSQL veritabanı yedeklemesi başladı.", "", "")
	for _, db := range d.p.Databases {
		d.dumpSingleDb(db, d.p.BackupDestination)
	}
	d.m.Notify("PostgreSQL veritabanı yedeklemesi sona erdi.", "", "")
}

func (d *Dumper) dumpSingleDb(db string, dst string) {
	dfp := dst + "/" + db + ".sql"
	y := time.Now().Format("2006")
	m := time.Now().Format("01")
	now := time.Now().Format("2006-01-02-150405")
	_ = os.MkdirAll(dst + "/" + y + "/" + m, os.ModePerm)
	tfp := dst + "/" + y + "/" + m + "/" + db + "--" + now + ".tar.gz"

	cmd := exec.Command("/usr/bin/pg_dump", db)
	f, err := os.Create(dfp)
	if err != nil {
		d.m.Notify(
			db + " veritabanı yedeklenirken hata oluştu.",
			"Çıktı dosyası oluşturulamadı:",
			err.Error(),
		)
		return
	}

	defer f.Close()
	defer func() {
		err = os.Remove(dfp)
	}()

	cmd.Stdout = f

	err = cmd.Start(); if err != nil {
		if err != nil {
			d.m.Notify(
				db + " veritabanı yedeklenirken hata oluştu.",
				dfp + " dosyasına dump alınamadı:",
				err.Error(),
			)
			notify.Email(d.p, "Yedekleme hatası", db + " veritabanı yedeklenirken hata oluştu. " + dfp + " dosyasına dump alınamadı: " + err.Error())
			return
		}
	}
	err = cmd.Wait(); if err != nil {
		if err != nil {
			d.m.Notify(
				db + " veritabanı yedeklenirken hata oluştu.",
				dfp + " dosyasına dump alınamadı:",
				err.Error(),
			)
			notify.Email(d.p, "Yedekleme hatası", db + " veritabanı yedeklenirken hata oluştu. " + dfp + " dosyasına dump alınamadı: " + err.Error())
			return
		}
	}

	cmd = exec.Command("/bin/tar", "zcf", tfp, dfp)
	err = cmd.Run(); if err != nil {
		if err != nil {
			d.m.Notify(
				db + " veritabanı yedeklenirken hata oluştu.",
				dfp + " dump dosyası " + tfp + " hedefine arşivlenemedi.",
				err.Error(),
			)
			notify.Email(d.p, "Yedekleme hatası", db + " veritabanı yedeklenirken hata oluştu. " + dfp + " dump dosyası " + tfp + " hedefine arşivlenemedi: " + err.Error())
			return
		}
	}

	d.m.Notify(db + " veritabanı " + tfp + " konumuna yedeklendi.", "", "")

	backupStatus := backupStatus{
		s3:    uploadStatus{
			enabled: false,
			success: true,
			msg:   "",
		},
		minio: uploadStatus{
			enabled: false,
			success: true,
			msg:   "",
		},
	}

	if d.p.S3.Enabled {
		backupStatus.s3.enabled = true
		uploader, err := newS3Uploader(d.p.S3.Region, d.p.S3.AccessKey, d.p.S3.SecretKey)
		if err != nil {
			d.m.Notify(db + " veritabanı yedeğini S3'e yüklemek için bağlantı sağlanamadı", "", err.Error())
			backupStatus.s3.success = false
			backupStatus.s3.msg = db + " veritabanı yedeğini S3'e yüklemek için bağlantı sağlanamadı: " + err.Error()
		} else {
			target := y + "/" + m + "/" + db + "--" + now + ".tar.gz"
			if d.p.S3.Path != "" {
				target = d.p.S3.Path + "/" + target
			}
			err = uploadFileToS3(uploader, tfp, d.p.S3.Bucket, target)
			if err != nil {
				d.m.Notify(db+" veritabanı yedeği S3'e yüklenemedi", "", err.Error())
				backupStatus.s3.success = false
				backupStatus.s3.msg = db + " veritabanı yedeği S3'e yüklenemedi: " + err.Error()
			} else {
				d.m.Notify(db+" veritabanı S3'e yüklendi.", "", "")
				backupStatus.s3.success = true
				backupStatus.s3.msg = db + " veritabanı yedeği S3'e yüklendi."

				/*
				if d.p.S3.RemoveLocal {
					_ = os.Remove(tfp)
				}
				*/
			}
		}
	}

	if d.p.Minio.Enabled {
		minioClient, err := newMinioClient(
			d.p.Minio.Endpoint,
			d.p.Minio.AccessKey,
			d.p.Minio.SecretKey,
			d.p.Minio.Secure,
			d.p.Minio.InsecureSkipVerify)
		if err != nil {
			d.m.Notify(db + " veritabanı yedeğini MinIO'ya yüklemek için bağlantı sağlanamadı", "", err.Error())
			backupStatus.minio.success = false
			backupStatus.minio.msg = db + " veritabanı yedeğini MinIO'ya yüklemek için bağlantı sağlanamadı: " + err.Error()
		} else {
			target := y + "/" + m + "/" + db + "--" + now + ".tar.gz"
			if d.p.Minio.Path != "" {
				target = d.p.Minio.Path + "/" + target
			}
			err = uploadFileToMinio(minioClient, tfp, d.p.Minio.Bucket, target)
			if err != nil {
				d.m.Notify(db+" veritabanı yedeği MinIO'ya yüklenemedi", "", err.Error())
				backupStatus.minio.success = false
				backupStatus.minio.msg = db + " veritabanı yedeği MinIO'ya yüklenemedi: " + err.Error()
			} else {
				d.m.Notify(db+" veritabanı MinIO'ya yüklendi.", "", "")
				backupStatus.minio.success = true
				backupStatus.minio.msg = db + " veritabanı yedeği MinIO'ya yüklendi."
			}
		}
	}

	if !backupStatus.minio.enabled && !backupStatus.s3.enabled {
		notify.Email(d.p, "Yedekleme başarılı", db + " veritabanı " + tfp + " konumuna yedeklendi.")
		return
	}

	subject := "Yedekleme başarılı"
	body := db + " veritabanı " + tfp + " konumuna yedeklendi."

	if backupStatus.minio.enabled {
		if backupStatus.minio.success {
			subject += ", MinIO'ya yükleme başarılı"
		} else {
			subject += ", MinIO'ya yükleme başarısız"
		}
		body += " " + backupStatus.minio.msg
	}

	if backupStatus.s3.enabled {
		if backupStatus.s3.success {
			subject += ", S3'e yükleme başarılı"
		} else {
			subject += ", S3'e yükleme başarısız"
		}
		body += " " + backupStatus.s3.msg
	}

	notify.Email(d.p, subject, body)
}
