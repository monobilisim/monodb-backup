package backup

import (
	"bytes"
	"monodb-backup/config"
	"monodb-backup/notify"
	"os/exec"
	"strings"
)

func SendRsync(srcPath, dstPath, db string, target config.Target) {
	var stderr1 bytes.Buffer
	var stderr2 bytes.Buffer
	var stdout bytes.Buffer
	logger.Info("rsync transfer started.\n Source: " + srcPath + " - Destination: " + target.Host + ":" + dstPath)
	if target.Path != "" {
		dstPath = target.Path + "/" + nameWithPath(dstPath)
	} else {
		dstPath = nameWithPath(dstPath)
	}
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
		return
	}
	cmdRsync := exec.Command("/usr/bin/rsync", target.Flags, "-e", "ssh -o HostKeyAlgorithms=+ssh-rsa -o PubKeyAcceptedKeyTypes=+ssh-rsa", srcPath, target.User+"@"+target.Host+":"+dstPath)
	cmdRsync.Stderr = &stderr2
	cmdRsync.Stdout = &stdout
	err = cmdRsync.Run()
	if err != nil {
		notify.SendAlarm("Couldn't send "+srcPath+" to "+target.Host+":"+dstPath+"\nError: "+err.Error()+" "+stderr2.String()+" Stdout: "+stdout.String(), true)
		logger.Error("Couldn't send " + srcPath + " to " + target.Host + ":" + dstPath + "\nError: " + err.Error() + " " + stderr2.String() + " Stdout: " + stdout.String())
		return
	}
	logger.Info("Successfully uploaded " + srcPath + " to " + target.Host + ":" + dstPath)
	notify.SendAlarm("Successfully uploaded "+srcPath+" to "+target.Host+":"+dstPath, false)
}
