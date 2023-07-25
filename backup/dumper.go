package backup

import (
	"bytes"
	"os"
	"os/exec"
	"pgsql-backup/config"
	"pgsql-backup/notify"
	"strings"
	"time"
)

type Dumper struct {
	p *config.Params
	m *notify.Mattermost
	l Logger
}

type backupStatus struct {
	s3    uploadStatus
	minio uploadStatus
}

type uploadStatus struct {
	enabled bool
	success bool
	msg     string
}

func NewDumper(params *config.Params, logger Logger) (d *Dumper) {
	d = &Dumper{
		p: params,
		m: notify.NewMattermost(params),
		l: logger,
	}
	return
}

func (d *Dumper) reportLog(message string, boolean bool) {
	d.l.Info(message)
	d.m.Notify(message, "", "", boolean)
}

// getDBList returns list of databases to backup from psql
func (d *Dumper) getDBList() []string {
	cmd := exec.Command("sudo", "-u", "postgres", "/usr/bin/psql", "-lqt")
	out, err := cmd.Output()
	if err != nil {
		d.reportLog("Veritabanı listesi alınamadı: "+err.Error(), true)
		return nil
	}

	var dbList []string
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		if len(line) > 0 {
			// trim leading and trailing spaces
			ln := strings.TrimSpace(strings.Split(string(line), "|")[0])
			// if first character is not a letter, it's not a database
			if ln == "" || ln[0] < 'a' || ln[0] > 'z' {
				continue
			}
			d.l.Info(ln)
			dbList = append(dbList, ln)
		}
	}
	return dbList
}

func (d *Dumper) Dump() {
	d.reportLog("PostgreSQL veritabanı yedeklemesi başladı.", false)

	if len(d.p.Databases) == 0 {
		d.reportLog("Veritabanı listesi alınıyor...", false)
		d.p.Databases = d.getDBList()
	}

	for _, db := range d.p.Databases {
		d.dumpSingleDb(db, d.p.BackupDestination)
	}
	d.reportLog("PostgreSQL veritabanı yedeklemesi sona erdi.", false)
}

func (d *Dumper) dumpSingleDb(db string, dst string) {
	dfp := dst + "/" + db + ".sql"
	y := time.Now().Format("2006")
	m := time.Now().Format("01")
	now := time.Now().Format("2006-01-02-150405")
	_ = os.MkdirAll(dst+"/"+y+"/"+m, os.ModePerm)
	tfp := dst + "/" + y + "/" + m + "/" + db + "--" + now + ".tar.gz"

	d.l.InfoWithFields(map[string]interface{}{
		"db":                         db,
		"dump konumu":                dfp,
		"sıkıştırılmış dosya konumu": tfp,
	},
		"Veritabanı yedekleniyor...",
	)

	cmd := exec.Command("/usr/bin/pg_dump", db)
	f, err := os.Create(dfp)
	if err != nil {

		d.l.ErrorWithFields(map[string]interface{}{
			"db":                         db,
			"dump konumu":                dfp,
			"sıkıştırılmış dosya konumu": tfp,
			"error":                      err.Error(),
		},
			"Çıktı dosyası oluşturulamadı.",
		)

		d.m.Notify(
			db+" veritabanı yedeklenirken hata oluştu.",
			"Çıktı dosyası oluşturulamadı:",
			err.Error(),
			true,
		)
		return
	}

	defer f.Close()
	defer func() {
		err = os.Remove(dfp)
	}()

	cmd.Stdout = f

	err = cmd.Start()
	if err != nil {
		d.l.ErrorWithFields(map[string]interface{}{
			"db":                         db,
			"dump konumu":                dfp,
			"sıkıştırılmış dosya konumu": tfp,
			"error":                      err.Error(),
		},
			"Dump alınamadı.",
		)

		d.m.Notify(
			db+" veritabanı yedeklenirken hata oluştu.",
			dfp+" dosyasına dump alınamadı:",
			err.Error(),
			true,
		)
		notify.Email(d.p, "Yedekleme hatası", db+" veritabanı yedeklenirken hata oluştu. "+dfp+" dosyasına dump alınamadı: "+err.Error(), true)
		return
	}
	err = cmd.Wait()
	if err != nil {
		d.l.ErrorWithFields(map[string]interface{}{
			"db":                         db,
			"dump konumu":                dfp,
			"sıkıştırılmış dosya konumu": tfp,
			"error":                      err.Error(),
		},
			"Dump alınamadı.",
		)

		d.m.Notify(
			db+" veritabanı yedeklenirken hata oluştu.",
			dfp+" dosyasına dump alınamadı:",
			err.Error(),
			true,
		)
		notify.Email(d.p, "Yedekleme hatası", db+" veritabanı yedeklenirken hata oluştu. "+dfp+" dosyasına dump alınamadı: "+err.Error(), true)
		return
	}

	cmd = exec.Command("/bin/tar", "zcf", tfp, dfp)
	err = cmd.Run()
	if err != nil {
		d.l.ErrorWithFields(map[string]interface{}{
			"db":                         db,
			"dump konumu":                dfp,
			"sıkıştırılmış dosya konumu": tfp,
			"error":                      err.Error(),
		},
			"Dump dosyası arşivlenemedi.",
		)

		d.m.Notify(
			db+" veritabanı yedeklenirken hata oluştu.",
			dfp+" dump dosyası "+tfp+" hedefine arşivlenemedi.",
			err.Error(),
			true,
		)
		notify.Email(d.p, "Yedekleme hatası", db+" veritabanı yedeklenirken hata oluştu. "+dfp+" dump dosyası "+tfp+" hedefine arşivlenemedi: "+err.Error(), true)
		return
	}

	d.l.InfoWithFields(map[string]interface{}{
		"db":                         db,
		"dump konumu":                dfp,
		"sıkıştırılmış dosya konumu": tfp,
	},
		"Veritabanı yedeklendi.",
	)

	d.m.Notify(db+" veritabanı "+tfp+" konumuna yedeklendi.", "", "", false)

	backupStatus := backupStatus{
		s3: uploadStatus{
			enabled: false,
			success: true,
			msg:     "",
		},
		minio: uploadStatus{
			enabled: false,
			success: true,
			msg:     "",
		},
	}

	if d.p.S3.Enabled {
		d.l.InfoWithFields(map[string]interface{}{
			"db":                         db,
			"dump konumu":                dfp,
			"sıkıştırılmış dosya konumu": tfp,
			"s3Bucket":                   d.p.S3.Bucket,
			"s3Path":                     d.p.S3.Path,
			"s3Region":                   d.p.S3.Region,
		},
			"Veritabanı yedeği S3'e yükleniyor...",
		)

		backupStatus.s3.enabled = true

		uploader, err := newS3Uploader(d.p.S3.Region, d.p.S3.AccessKey, d.p.S3.SecretKey)
		if err != nil {
			d.l.ErrorWithFields(map[string]interface{}{
				"db":                         db,
				"dump konumu":                dfp,
				"sıkıştırılmış dosya konumu": tfp,
				"s3Bucket":                   d.p.S3.Bucket,
				"s3Path":                     d.p.S3.Path,
				"s3Region":                   d.p.S3.Region,
				"error":                      err.Error(),
			},
				"Veritabanı yedeği S3'e yüklenemedi.",
			)

			d.m.Notify(db+" veritabanı yedeğini S3'e yüklemek için bağlantı sağlanamadı", "", err.Error(), true)
			backupStatus.s3.success = false
			backupStatus.s3.msg = db + " veritabanı yedeğini S3'e yüklemek için bağlantı sağlanamadı: " + err.Error()
		} else {
			target := y + "/" + m + "/" + db + "--" + now + ".tar.gz"
			if d.p.S3.Path != "" {
				target = d.p.S3.Path + "/" + target
			}
			err = uploadFileToS3(uploader, tfp, d.p.S3.Bucket, target)
			if err != nil {
				d.l.ErrorWithFields(map[string]interface{}{
					"db":                         db,
					"dump konumu":                dfp,
					"sıkıştırılmış dosya konumu": tfp,
					"s3Bucket":                   d.p.S3.Bucket,
					"s3Path":                     d.p.S3.Path,
					"s3Region":                   d.p.S3.Region,
					"target":                     target,
					"error":                      err.Error(),
				},
					"Veritabanı yedeği S3'e yüklenemedi.",
				)

				d.m.Notify(db+" veritabanı yedeği S3'e yüklenemedi", "", err.Error(), true)
				backupStatus.s3.success = false
				backupStatus.s3.msg = db + " veritabanı yedeği S3'e yüklenemedi: " + err.Error()
			} else {
				d.l.InfoWithFields(map[string]interface{}{
					"db":                         db,
					"dump konumu":                dfp,
					"sıkıştırılmış dosya konumu": tfp,
					"s3Bucket":                   d.p.S3.Bucket,
					"s3Path":                     d.p.S3.Path,
					"s3Region":                   d.p.S3.Region,
					"target":                     target,
				},
					"Veritabanı yedeği S3'e yüklendi.",
				)

				d.m.Notify(db+" veritabanı S3'e yüklendi.", "", "", false)
				backupStatus.s3.success = true
				backupStatus.s3.msg = db + " veritabanı yedeği S3'e yüklendi."

			}
		}
	}

	if d.p.Minio.Enabled {
		d.l.InfoWithFields(map[string]interface{}{
			"db":                         db,
			"dump konumu":                dfp,
			"sıkıştırılmış dosya konumu": tfp,
			"minioEndpoint":              d.p.Minio.Endpoint,
			"minioBucket":                d.p.Minio.Bucket,
			"minioPath":                  d.p.Minio.Path,
		},
			"Veritabanı yedeği MinIO'ya yükleniyor...",
		)

		backupStatus.minio.enabled = true

		minioClient, err := newMinioClient(
			d.p.Minio.Endpoint,
			d.p.Minio.AccessKey,
			d.p.Minio.SecretKey,
			d.p.Minio.Secure,
			d.p.Minio.InsecureSkipVerify)
		if err != nil {
			d.l.ErrorWithFields(map[string]interface{}{
				"db":                         db,
				"dump konumu":                dfp,
				"sıkıştırılmış dosya konumu": tfp,
				"minioEndpoint":              d.p.Minio.Endpoint,
				"minioBucket":                d.p.Minio.Bucket,
				"minioPath":                  d.p.Minio.Path,
				"error":                      err.Error(),
			},
				"Veritabanı yedeği MinIO'ya yüklenemedi.",
			)

			d.m.Notify(db+" veritabanı yedeğini MinIO'ya yüklemek için bağlantı sağlanamadı", "", err.Error(), true)
			backupStatus.minio.success = false
			backupStatus.minio.msg = db + " veritabanı yedeğini MinIO'ya yüklemek için bağlantı sağlanamadı: " + err.Error()
		} else {
			target := y + "/" + m + "/" + db + "--" + now + ".tar.gz"
			if d.p.Minio.Path != "" {
				target = d.p.Minio.Path + "/" + target
			}
			err = uploadFileToMinio(minioClient, tfp, d.p.Minio.Bucket, target)
			if err != nil {
				d.l.ErrorWithFields(map[string]interface{}{
					"db":                         db,
					"dump konumu":                dfp,
					"sıkıştırılmış dosya konumu": tfp,
					"minioEndpoint":              d.p.Minio.Endpoint,
					"minioBucket":                d.p.Minio.Bucket,
					"minioPath":                  d.p.Minio.Path,
					"target":                     target,
					"error":                      err.Error(),
				},
					"Veritabanı yedeği MinIO'ya yüklenemedi.",
				)

				d.m.Notify(db+" veritabanı yedeği MinIO'ya yüklenemedi", "", err.Error(), true)
				backupStatus.minio.success = false
				backupStatus.minio.msg = db + " veritabanı yedeği MinIO'ya yüklenemedi: " + err.Error()
			} else {
				d.l.InfoWithFields(map[string]interface{}{
					"db":                         db,
					"dump konumu":                dfp,
					"sıkıştırılmış dosya konumu": tfp,
					"minioEndpoint":              d.p.Minio.Endpoint,
					"minioBucket":                d.p.Minio.Bucket,
					"minioPath":                  d.p.Minio.Path,
					"target":                     target,
				},
					"Veritabanı yedeği MinIO'ya yüklendi.",
				)

				d.m.Notify(db+" veritabanı MinIO'ya yüklendi.", "", "", false)
				backupStatus.minio.success = true
				backupStatus.minio.msg = db + " veritabanı yedeği MinIO'ya yüklendi."
			}
		}
	}

	if !backupStatus.minio.enabled && !backupStatus.s3.enabled {
		err = notify.Email(d.p, "Yedekleme başarılı", db+" veritabanı "+tfp+" konumuna yedeklendi.", false)
		if err != nil {
			d.l.Error("Mail gönderilemedi: " + err.Error())
		}
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

		err = notify.Email(d.p, subject, body, backupStatus.minio.success)
		if err != nil {
			d.l.Error("Mail gönderilemedi: " + err.Error())
		}
	}

	if backupStatus.s3.enabled {
		if backupStatus.s3.success {
			subject += ", S3'e yükleme başarılı"
		} else {
			subject += ", S3'e yükleme başarısız"
		}
		body += " " + backupStatus.s3.msg

		err = notify.Email(d.p, subject, body, backupStatus.s3.success)
		if err != nil {
			d.l.Error("Mail gönderilemedi: " + err.Error())
		}
	}

	if d.p.RemoveLocal {
		_ = os.Remove(tfp)
	}
}
