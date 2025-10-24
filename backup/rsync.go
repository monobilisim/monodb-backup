package backup

import (
	"bytes"
	"monodb-backup/config"
	"os/exec"
	"strings"
)

var lastDB, lastHost, lastPath string
var folderCreated bool
var rotating bool

func SendRsync(srcPath, dstPath, db string, target config.Target) (string, error) {
	var dst string

	if target.Path != "" {
		dst = target.Path + "/" + nameWithPath(dstPath)
	} else {
		dst = nameWithPath(dstPath)
	}

	message, err := sendRsync(srcPath, dst, db, target)
	if err != nil {
		return message, err
	}

	if params.Rotation.Enabled {
		extension := strings.Split(dstPath, ".")
		shouldRotate, dstPath := rotate(extension[0])
		for i := 1; i < len(extension); i++ {
			dstPath = dstPath + "." + extension[i]
		}
		if target.Path != "" {
			dstPath = target.Path + "/" + dstPath
		}
		if shouldRotate {
			rotating = true
			return sendRsync(srcPath, dstPath, db, target)
		}
		rotating = false
		updateRotatedTimestamp(db)
	}
	return "", nil
}

func sendRsync(srcPath, dstPath, db string, target config.Target) (string, error) {
	var stderr1, stderr2, stdout bytes.Buffer

	logger.Info("rsync transfer started.\n Source: " + srcPath + " - Destination: " + target.Host + ":" + dstPath)

	fullPath := strings.Split(dstPath, "/")
	newPath := fullPath[0]
	for i := 1; i < len(fullPath)-1; i++ {
		newPath = newPath + "/" + fullPath[i]
	}

	// We need folderCreated in case if the folder creation failed at the first table
	// So even if the folder creation failed at the first table, it will be created this time
	if lastDB != db && folderCreated {
		folderCreated = false
	}

	if !params.BackupAsTables {
		cmdMkdir := exec.Command("ssh", "-o", "HostKeyAlgorithms=+ssh-rsa", "-o", "PubKeyAcceptedKeyTypes=+ssh-rsa", target.Host, "mkdir -p "+newPath)
		err := cmdMkdir.Run()
		if err != nil {
			cmdMkdir.Stderr = &stderr1
			message := "Couldn't create folder " + newPath + " to upload backups at" + target.Host + ":" + dstPath + "\nError: " + err.Error() + " " + stderr1.String()
			// notify.SendAlarm(message, true)
			logger.Error(message)
			lastDB = db
			lastPath = newPath
			lastHost = target.Host
			return message, err
		}
	} else {
		if (lastDB != db && !folderCreated) || lastHost != target.Host || rotating || lastPath != newPath {
			cmdMkdir := exec.Command("ssh", "-o", "HostKeyAlgorithms=+ssh-rsa", "-o", "PubKeyAcceptedKeyTypes=+ssh-rsa", target.Host, "mkdir -p "+newPath)
			err := cmdMkdir.Run()
			if err != nil {
				cmdMkdir.Stderr = &stderr1
				message := "Couldn't create folder " + newPath + " to upload backups at" + target.Host + ":" + dstPath + "\nError: " + err.Error() + " " + stderr1.String() + "\nStill following up in case the folder exists and this was just a connection error"
				// notify.SendAlarm(message, true)
				logger.Error(message)
				lastDB = db
				lastPath = newPath
				lastHost = target.Host
			} else {
				folderCreated = true
			}
		}
	}

	cmdRsync := exec.Command("/usr/bin/rsync", target.Flags, "-e", "ssh -o HostKeyAlgorithms=+ssh-rsa -o PubKeyAcceptedKeyTypes=+ssh-rsa", srcPath, target.User+"@"+target.Host+":"+dstPath)
	cmdRsync.Stderr = &stderr2
	cmdRsync.Stdout = &stdout

	err := cmdRsync.Run()
	if err != nil {
		message := "Couldn't send " + srcPath + " to " + target.Host + ":" + dstPath + "\nError: " + err.Error() + " " + stderr2.String() + " Stdout: " + stdout.String()
		// notify.SendAlarm(message, true)
		logger.Error(message)
		lastDB = db
		lastPath = newPath
		lastHost = target.Host
		return message, err
	}

	message := "Successfully uploaded " + srcPath + " to " + target.Host + ":" + dstPath
	logger.Info(message)
	// notify.SendAlarm(message, false)
	// itWorksNow(message, true)

	lastDB = db
	lastPath = newPath
	lastHost = target.Host
	return "", nil
}
