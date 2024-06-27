package backup

import (
	"bytes"
	"monodb-backup/config"
	"monodb-backup/notify"
	"os/exec"
	"strings"
)

func SendRsync(srcPath, dstPath string, target config.Target) error {
	var dst string

	if target.Path != "" {
		dst = target.Path + "/" + nameWithPath(dstPath)
	} else {
		dst = nameWithPath(dstPath)
	}

	err := sendRsync(srcPath, dst, target)
	if err != nil {
		return err
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
			err = sendRsync(srcPath, dstPath, target)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func sendRsync(srcPath, dstPath string, target config.Target) error {
	var stderr1, stderr2, stdout bytes.Buffer

	logger.Info("rsync transfer started.\n Source: " + srcPath + " - Destination: " + target.Host + ":" + dstPath)

	fullPath := strings.Split(dstPath, "/")
	newPath := fullPath[0]
	for i := 1; i < len(fullPath)-1; i++ {
		newPath = newPath + "/" + fullPath[i]
	}

	cmdMkdir := exec.Command("ssh", "-o", "HostKeyAlgorithms=+ssh-rsa", "-o", "PubKeyAcceptedKeyTypes=+ssh-rsa", target.Host, "mkdir -p "+newPath)
	err := cmdMkdir.Run()
	if err != nil {
		cmdMkdir.Stderr = &stderr1
		notify.SendAlarm("Couldn't create folder "+newPath+" to upload backups at"+target.Host+":"+dstPath+"\nError: "+err.Error()+" "+stderr1.String(), true)
		logger.Error("Couldn't create folder " + newPath + " to upload backups at" + target.Host + ":" + dstPath + "\nError: " + err.Error() + " " + stderr1.String())
		return err
	}

	cmdRsync := exec.Command("/usr/bin/rsync", target.Flags, "-e", "ssh -o HostKeyAlgorithms=+ssh-rsa -o PubKeyAcceptedKeyTypes=+ssh-rsa", srcPath, target.User+"@"+target.Host+":"+dstPath)
	cmdRsync.Stderr = &stderr2
	cmdRsync.Stdout = &stdout

	err = cmdRsync.Run()
	if err != nil {
		notify.SendAlarm("Couldn't send "+srcPath+" to "+target.Host+":"+dstPath+"\nError: "+err.Error()+" "+stderr2.String()+" Stdout: "+stdout.String(), true)
		logger.Error("Couldn't send " + srcPath + " to " + target.Host + ":" + dstPath + "\nError: " + err.Error() + " " + stderr2.String() + " Stdout: " + stdout.String())
		return err
	}

	logger.Info("Successfully uploaded " + srcPath + " to " + target.Host + ":" + dstPath)
	message := "Successfully uploaded " + srcPath + " to " + target.Host + ":" + dstPath
	notify.SendAlarm(message, false)
	itWorksNow(message, true)

	return nil
}
