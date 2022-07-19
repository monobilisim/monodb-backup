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

func NewDumper(params *config.Params) (e *Dumper) {
	e = &Dumper{
		p: params,
		m: notify.NewMattermost(params),
	}
	return
}

func (e *Dumper) Dump() {
	e.m.Notify("PostgreSQL veritabanı yedeklemesi başladı.", "", "")
	for _, db := range e.p.Databases {
		e.dumpSingleDb(db, e.p.BackupDestination)
	}
	e.m.Notify("PostgreSQL veritabanı yedeklemesi sona erdi.", "", "")
}

func (e *Dumper) dumpSingleDb(db string, dst string) {
	dfp := dst + "/" + db + ".sql"
	y := time.Now().Format("2006")
	m := time.Now().Format("01")
	_ = os.MkdirAll(dst + "/" + y + "/" + m, os.ModePerm)
	tfp := dst + "/" + y + "/" + m + "/" + db + "--" + time.Now().Format("2006-01-02-150405") + ".tar.gz"

	cmd := exec.Command("/usr/bin/pg_dump", db)
	f, err := os.Create(dfp)
	if err != nil {
		e.m.Notify(
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
			e.m.Notify(
				db + " veritabanı yedeklenirken hata oluştu.",
				dfp + " dosyasına dump alınamadı:",
				err.Error(),
			)
			return
		}
	}
	err = cmd.Wait(); if err != nil {
		if err != nil {
			e.m.Notify(
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
			e.m.Notify(
				db + " veritabanı yedeklenirken hata oluştu.",
				dfp + " dump dosyası " + tfp + " hedefine arşivlenemedi.",
				err.Error(),
			)
			return
		}
	}

	e.m.Notify(db + " veritabanı " + tfp + " konumuna yedeklendi.", "", "")
}
