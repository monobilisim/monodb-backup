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

type rightNow struct {
	year  string
	month string
	now   string
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
	cmd := exec.Command("/usr/bin/psql", "-lqt")
	out, err := cmd.Output()
	if err != nil {
		d.reportLog("Veritabanı listesi alınamadı: "+err.Error(), true)
		return nil
	}

	var dbList []string
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		if len(line) > 0 {
			ln := strings.TrimSpace(strings.Split(string(line), "|")[0])
			if ln == "" || ln[0] < 'a' || ln[0] > 'z' || ln == "template0" || ln == "template1" || ln == "postgres" {
				continue
			}
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
	date := rightNow{
		year:  time.Now().Format("2006"),
		month: time.Now().Format("01"),
		now:   time.Now().Format("2006-01-02-150405"),
	}
	_ = os.MkdirAll(dst+"/"+date.year+"/"+date.month, os.ModePerm)
	tfp := dst + "/" + date.year + "/" + date.month + "/" + db + "--" + date.now + ".tar.gz"

	logInfo := map[string]interface{}{
		"db":                         db,
		"dump konumu":                dfp,
		"sıkıştırılmış dosya konumu": tfp,
	}

	d.l.InfoWithFields(logInfo, "Veritabanı yedekleniyor...")

	cmd := exec.Command("/usr/bin/pg_dump", db)
	f, err := os.Create(dfp)
	if err != nil {
		logInfo["error"] = err.Error()
		d.l.ErrorWithFields(logInfo, "Çıktı dosyası oluşturulamadı.")

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
		logInfo["error"] = err.Error()
		d.l.ErrorWithFields(logInfo, "Dump alınamadı.")

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
		logInfo["error"] = err.Error()
		d.l.ErrorWithFields(logInfo, "Dump alınamadı.")

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
		logInfo["error"] = err.Error()
		d.l.ErrorWithFields(logInfo, "Dump dosyası arşivlenemedi.")

		d.m.Notify(
			db+" veritabanı yedeklenirken hata oluştu.",
			dfp+" dump dosyası "+tfp+" hedefine arşivlenemedi.",
			err.Error(),
			true,
		)
		notify.Email(d.p, "Yedekleme hatası", db+" veritabanı yedeklenirken hata oluştu. "+dfp+" dump dosyası "+tfp+" hedefine arşivlenemedi: "+err.Error(), true)
		return
	}

	d.l.InfoWithFields(logInfo, "Veritabanı yedeklendi.")

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
		d.uploadToS3(db, dfp, tfp, &backupStatus, date)
	}

	if d.p.Minio.Enabled {
		d.uploadToMinio(db, dfp, tfp, &backupStatus, date)
	}

	if !backupStatus.minio.enabled && !backupStatus.s3.enabled {
		err = notify.Email(d.p, "Yedekleme başarılı", db+" veritabanı "+tfp+" konumuna yedeklendi.", false)
		if err != nil {
			d.l.Error("Mail gönderilemedi: " + err.Error())
		}
		return
	}

	if d.p.RemoveLocal {
		_ = os.Remove(tfp)
	}
}

func (d *Dumper) uploadToS3(db, dfp, tfp string, backupStatus *backupStatus, date rightNow) {
	logInfoS3 := map[string]interface{}{
		"db":                         db,
		"dump konumu":                dfp,
		"sıkıştırılmış dosya konumu": tfp,
		"s3Bucket":                   d.p.S3.Bucket,
		"s3Path":                     d.p.S3.Path,
		"s3Region":                   d.p.S3.Region,
	}
	d.l.InfoWithFields(logInfoS3, "Veritabanı yedeği S3'e yükleniyor...")

	backupStatus.s3.enabled = true

	uploader, err := newS3Uploader(d.p.S3.Region, d.p.S3.AccessKey, d.p.S3.SecretKey)
	if err != nil {
		logInfoS3["error"] = err.Error()
		d.l.ErrorWithFields(logInfoS3, "Veritabanı yedeği S3'e yüklenemedi.")

		d.m.Notify(db+" veritabanı yedeğini S3'e yüklemek için bağlantı sağlanamadı", "", err.Error(), true)
		backupStatus.s3.success = false
		backupStatus.s3.msg = db + " veritabanı yedeğini S3'e yüklemek için bağlantı sağlanamadı: " + err.Error()

		return
	} else {
		target := date.year + "/" + date.month + "/" + db + "--" + date.now + ".tar.gz"
		if d.p.S3.Path != "" {
			target = d.p.S3.Path + "/" + target
		}
		logInfoS3["target"] = target
		err = uploadFileToS3(uploader, tfp, d.p.S3.Bucket, target)
		if err != nil {
			logInfoS3["error"] = err.Error()
			d.l.ErrorWithFields(logInfoS3, "Veritabanı yedeği S3'e yüklenemedi.")

			d.m.Notify(db+" veritabanı yedeği S3'e yüklenemedi", "", err.Error(), true)
			backupStatus.s3.success = false
			backupStatus.s3.msg = db + " veritabanı yedeği S3'e yüklenemedi: " + err.Error()
		} else {
			d.l.InfoWithFields(logInfoS3, "Veritabanı yedeği S3'e yüklendi.")

			d.m.Notify(db+" veritabanı S3'e yüklendi.", "", "", false)
			backupStatus.s3.success = true
			backupStatus.s3.msg = db + " veritabanı yedeği S3'e yüklendi."

		}
	}

	subject := "Yedekleme başarılı"
	body := db + " veritabanı " + tfp + " konumuna yedeklendi."

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

func (d *Dumper) uploadToMinio(db, dfp, tfp string, backupStatus *backupStatus, date rightNow) {
	logInfoMinio := map[string]interface{}{
		"db":                         db,
		"dump konumu":                dfp,
		"sıkıştırılmış dosya konumu": tfp,
		"minioEndpoint":              d.p.Minio.Endpoint,
		"minioBucket":                d.p.Minio.Bucket,
		"minioPath":                  d.p.Minio.Path,
	}

	d.l.InfoWithFields(logInfoMinio, "Veritabanı yedeği MinIO'ya yükleniyor...")

	backupStatus.minio.enabled = true

	minioClient, err := newMinioClient(
		d.p.Minio.Endpoint,
		d.p.Minio.AccessKey,
		d.p.Minio.SecretKey,
		d.p.Minio.Secure,
		d.p.Minio.InsecureSkipVerify)
	if err != nil {
		logInfoMinio["error"] = err.Error()
		d.l.ErrorWithFields(logInfoMinio, "Veritabanı yedeği MinIO'ya yüklenemedi.")

		d.m.Notify(db+" veritabanı yedeğini MinIO'ya yüklemek için bağlantı sağlanamadı", "", err.Error(), true)
		backupStatus.minio.success = false
		backupStatus.minio.msg = db + " veritabanı yedeğini MinIO'ya yüklemek için bağlantı sağlanamadı: " + err.Error()
	} else {
		target := date.year + "/" + date.month + "/" + db + "--" + date.now + ".tar.gz"
		if d.p.Minio.Path != "" {
			target = d.p.Minio.Path + "/" + target
		}
		logInfoMinio["target"] = target
		err = uploadFileToMinio(minioClient, tfp, d.p.Minio.Bucket, target)
		if err != nil {
			logInfoMinio["error"] = err.Error()
			d.l.ErrorWithFields(logInfoMinio, "Veritabanı yedeği MinIO'ya yüklenemedi.")

			d.m.Notify(db+" veritabanı yedeği MinIO'ya yüklenemedi", "", err.Error(), true)
			backupStatus.minio.success = false
			backupStatus.minio.msg = db + " veritabanı yedeği MinIO'ya yüklenemedi: " + err.Error()
		} else {
			d.l.InfoWithFields(logInfoMinio, "Veritabanı yedeği MinIO'ya yüklendi.")

			d.m.Notify(db+" veritabanı MinIO'ya yüklendi.", "", "", false)
			backupStatus.minio.success = true
			backupStatus.minio.msg = db + " veritabanı yedeği MinIO'ya yüklendi."
		}
	}

	subject := "Yedekleme başarılı"
	body := db + " veritabanı " + tfp + " konumuna yedeklendi."
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
