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
			return
		}
	}

	d.m.Notify(db + " veritabanı " + tfp + " konumuna yedeklendi.", "", "")

	if d.p.S3.Enabled {
		uploader, err := newS3Uploader(d.p.S3.Region, d.p.S3.AccessKey, d.p.S3.SecretKey)
		if err != nil {
			d.m.Notify(db + " veritabanını S3'e yüklemek için bağlantı sağlanamadı", "", err.Error())
		} else {
			target := y + "/" + m + "/" + db + "--" + now + ".tar.gz"
			if d.p.S3.Path != "" {
				target = d.p.S3.Path + "/" + target
			}
			err = uploadFileToS3(uploader, tfp, d.p.S3.Bucket, target)
			if err != nil {
				d.m.Notify(db+" veritabanı S3'e yüklenemedi", "", err.Error())
			}
		}
	}
}
