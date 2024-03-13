package backup

import (
	"bytes"
	"monodb-backup/config"
	"monodb-backup/notify"
	"os"
	"os/exec"
)

var i int

func mountMinIO(minio config.MinIO) {
	var stderr bytes.Buffer
	data := []byte(minio.AccessKey + ":" + minio.SecretKey)
	err := os.WriteFile(minio.S3FS.PasswdFile, data, 0600)
	if err != nil {
		notify.SendAlarm("Couldn't write password file at path: "+minio.S3FS.PasswdFile+" - Error: "+err.Error(), true)
		logger.Fatal("Couldn't write password file at path: " + minio.S3FS.PasswdFile + " - Error: " + err.Error())
		return
	}

	err = os.MkdirAll(minio.S3FS.MountPath, os.ModePerm)
	if err != nil {
		notify.SendAlarm("Couldn't create folder to mount MinIO at path: "+minio.S3FS.MountPath+" - Error: "+err.Error(), true)
		logger.Fatal("Couldn't create folder to mount MinIO at path: " + minio.S3FS.MountPath + " - Error: " + err.Error())
		return
	}

	cmd := exec.Command("s3fs", minio.Bucket, minio.S3FS.MountPath, "-o", "passwd_file="+minio.S3FS.PasswdFile+",use_path_request_style,url=https://"+minio.Endpoint)
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		if i <= 1 {
			i++
			notify.SendAlarm("Couldn't mount "+minio.Bucket+" It might still be mounted from a previous run. Trying to unmouth...", true)
			logger.Error("Couldn't mount " + minio.Bucket + " It might still be mounted from a previous run. Trying to unmouth...")
			umountMinIO(minio)
			mountMinIO(minio)
			notify.SendAlarm("Successfully mounted bucket: "+minio.Bucket+" at path: "+minio.S3FS.MountPath, false)
			logger.Info("Successfully mounted bucket: " + minio.Bucket + " at path: " + minio.S3FS.MountPath)
			return
		}
		notify.SendAlarm("Couldn't mount "+minio.Bucket+" - Error: "+err.Error()+" - "+stderr.String(), true)
		logger.Fatal("Couldn't mount " + minio.Bucket + " - Error: " + err.Error() + " - " + stderr.String())
		return
	}
}

func umountMinIO(minio config.MinIO) {
	var stderr bytes.Buffer
	if !minio.S3FS.KeepPasswdFile {
		err := os.Remove(minio.S3FS.PasswdFile)
		if err != nil {
			notify.SendAlarm("Coudn't delete Password file:"+minio.S3FS.PasswdFile+" - Error: "+err.Error(), true)
			logger.Error("Couldn't delete Password file:" + minio.S3FS.PasswdFile + " - Error: " + err.Error())
		}
	}

	cmd := exec.Command("umount", minio.S3FS.MountPath)
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		notify.SendAlarm("Couldn't unmount "+minio.S3FS.MountPath+" - Error: "+err.Error()+" - "+stderr.String(), true)
		logger.Fatal("Couldn't unmount " + minio.S3FS.MountPath + " - Error: " + err.Error() + " - " + stderr.String())
		return
	}
}
