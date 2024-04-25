package backup

import (
	"bytes"
	"monodb-backup/config"
	"monodb-backup/notify"
	"os/exec"
	"strings"
)

func SendRsync(srcPath, dstPath string, target config.Target) {
	var dst string

	// Construct the full destination path
	if target.Path != "" {
		dst = target.Path + "/" + nameWithPath(dstPath)
	} else {
		dst = nameWithPath(dstPath)
	}

	err := sendRsync(srcPath, dst, target)
	if err == nil && params.Rotation.Enabled {
		extension := strings.Split(dstPath, ".")
		shouldRotate, dstPath := rotate(extension[0])
		for i := 1; i < len(extension); i++ {
			dstPath = dstPath + "." + extension[i]
		}
		if target.Path != "" {
			dstPath = target.Path + "/" + dstPath
		}
		if shouldRotate {
			_ = sendRsync(srcPath, dstPath, target)
		}
	}
}

func sendRsync(srcPath, dstPath string, target config.Target) error {
	// Create buffers to store stderr and stdout from commands
	var stderr1, stderr2, stdout bytes.Buffer

	// Log the start of the rsync transfer
	logger.Info("rsync transfer started.\n Source: " + srcPath + " - Destination: " + target.Host + ":" + dstPath)

	// Split the destination path to get the containing folder path
	fullPath := strings.Split(dstPath, "/")
	newPath := fullPath[0]
	for i := 1; i < len(fullPath)-1; i++ {
		newPath = newPath + "/" + fullPath[i]
	}

	// Create the destination folder on the remote host if it doesn't exist
	cmdMkdir := exec.Command("ssh", "-o", "HostKeyAlgorithms=+ssh-rsa", "-o", "PubKeyAcceptedKeyTypes=+ssh-rsa", target.Host, "mkdir -p "+newPath)
	err := cmdMkdir.Run()
	if err != nil {
		// If creating the folder fails, capture the error and send notifications
		cmdMkdir.Stderr = &stderr1
		notify.SendAlarm("Couldn't create folder "+newPath+" to upload backups at"+target.Host+":"+dstPath+"\nError: "+err.Error()+" "+stderr1.String(), true)
		logger.Error("Couldn't create folder " + newPath + " to upload backups at" + target.Host + ":" + dstPath + "\nError: " + err.Error() + " " + stderr1.String())
		return err
	}

	// Construct the rsync command to transfer the file
	cmdRsync := exec.Command("/usr/bin/rsync", target.Flags, "-e", "ssh -o HostKeyAlgorithms=+ssh-rsa -o PubKeyAcceptedKeyTypes=+ssh-rsa", srcPath, target.User+"@"+target.Host+":"+dstPath)
	cmdRsync.Stderr = &stderr2
	cmdRsync.Stdout = &stdout

	// Execute the rsync transfer
	err = cmdRsync.Run()
	if err != nil {
		// If the transfer fails, capture the error and send notifications
		notify.SendAlarm("Couldn't send "+srcPath+" to "+target.Host+":"+dstPath+"\nError: "+err.Error()+" "+stderr2.String()+" Stdout: "+stdout.String(), true)
		logger.Error("Couldn't send " + srcPath + " to " + target.Host + ":" + dstPath + "\nError: " + err.Error() + " " + stderr2.String() + " Stdout: " + stdout.String())
		return err
	}

	// If the transfer is successful, log and send a notification
	logger.Info("Successfully uploaded " + srcPath + " to " + target.Host + ":" + dstPath)
	notify.SendAlarm("Successfully uploaded "+srcPath+" to "+target.Host+":"+dstPath, false)

	return nil
}
