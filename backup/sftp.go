package backup

import (
	"monodb-backup/config"
	"monodb-backup/notify"
	"net"
	"os"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func SendSFTP(srcPath, dstPath, db string, target config.Target) error {
	dstPath = target.Path + "/" + nameWithPath(dstPath)
	logger.Info("SFTP transfer started.\n Source: " + srcPath + " - Destination: " + target.Host + ":" + dstPath)
	sock, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
	if err != nil {
		logger.Error("Couldn't get environment variable SSH_AUTH_SOCK - Error: " + err.Error())
		notify.SendAlarm("Couldn't upload backup "+srcPath+" to "+target.Host+":"+dstPath+"\nCouldn't get environment variable SSH_AUTH_SOCK - Error: "+err.Error(), true)
		return err
	}

	sockAgent := agent.NewClient(sock)

	signers, err := sockAgent.Signers()
	if err != nil {
		logger.Error("Couldn't get signers for ssh keys - Error: " + err.Error())
		notify.SendAlarm("Couldn't upload backup "+srcPath+" to "+target.Host+":"+dstPath+"\nCouldn't get signers for ssh keys - Error: "+err.Error(), true)
		return err
	}
	auths := []ssh.AuthMethod{ssh.PublicKeys(signers...)}

	config := &ssh.ClientConfig{
		User:            target.User,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, _ := ssh.Dial("tcp", target.Host+":"+target.Port, config)
	defer func() {
		err = client.Close()
		if err != nil {
			logger.Error("Couldn't close SSH client - Error: " + err.Error())
			notify.SendAlarm("Couldn't close SSH client - Error: "+err.Error(), true)
		}
	}()

	sftpCli, err := sftp.NewClient(client)
	if err != nil {
		logger.Error("Couldn't create an SFTP client - Error: " + err.Error())
		notify.SendAlarm("Couldn't upload backup "+srcPath+" to "+target.Host+":"+dstPath+"\nCouldn't create an SFTP client - Error: "+err.Error(), true)
		return err
	}
	defer func() {
		err = sftpCli.Close()
		if err != nil {
			logger.Error("Couldn't close SFTP client - Error: " + err.Error())
			notify.SendAlarm("Couldn't close SFTP client - Error: "+err.Error(), true)
		}
	}()

	src, err := os.Open(srcPath)
	if err != nil {
		logger.Error("Couldn't open source file " + srcPath + " for copying - Error: " + err.Error())
		notify.SendAlarm("Couldn't upload backup "+srcPath+" to "+target.Host+":"+dstPath+"\nCouldn't open source file "+srcPath+" for copying - Error: "+err.Error(), true)
		return err
	}
	defer func() {
		err = src.Close()
		if err != nil {
			logger.Error("Couldn't close source file: " + srcPath + " - Error: " + err.Error())
			notify.SendAlarm("Couldn't close source file: "+srcPath+" - Error: "+err.Error(), true)
		}
	}()

	err = sendOverSFTP(srcPath, dstPath, src, target, sftpCli)
	if err != nil {
		return err
	}

	if params.Rotation.Enabled {
		shouldRotate, newDst := rotate(db)
		if shouldRotate {
			extension := strings.Split(dstPath, ".")
			for i := 1; i < len(extension); i++ {
				newDst = newDst + "." + extension[i]
			}
			newDst = target.Path + "/" + newDst
			err = sendOverSFTP(srcPath, newDst, src, target, sftpCli)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func sendOverSFTP(srcPath, dstPath string, src *os.File, target config.Target, sftpCli *sftp.Client) error {
	fullPath := strings.Split(dstPath, "/")
	newPath := "/"
	for i := 0; i < len(fullPath)-1; i++ {
		newPath = newPath + "/" + fullPath[i]
	}
	err := sftpCli.MkdirAll(newPath)
	if err != nil {
		logger.Error("Couldn't create folders " + newPath + " - Error: " + err.Error())
		notify.SendAlarm("Couldn't upload backup "+srcPath+" to "+target.Host+":"+dstPath+"\nCouldn't create folders "+newPath+" - Error: "+err.Error(), true)
		return err
	}
	dst, err := sftpCli.Create(dstPath)
	if err != nil {
		logger.Error("Couldn't create file " + dstPath + " - Error: " + err.Error())
		notify.SendAlarm("Couldn't upload backup "+srcPath+" to "+target.Host+":"+dstPath+"\nCouldn't create file "+dstPath+" - Error: "+err.Error(), true)
		return err
	}
	defer func() {
		err = dst.Close()
		if err != nil {
			logger.Error("Couldn't close destination file: " + dstPath + " - Error: " + err.Error())
			notify.SendAlarm("Couldn't close destination file: "+dstPath+" - Error: "+err.Error(), true)
		}
	}()
	logger.Info("Created destination file " + dstPath + " Now starting copying")

	if _, err := dst.ReadFrom(src); err != nil {
		logger.Error("Couldn't read from file " + srcPath + " to write at " + dstPath + " - Error: " + err.Error())
		notify.SendAlarm("Couldn't upload backup "+srcPath+" to "+target.Host+":"+dstPath+"\nCouldn't read from file "+srcPath+" to write at "+dstPath+" - Error: "+err.Error(), true)
		return err
	}
	logger.Info("Successfully copied " + srcPath + " to " + target.Host + ":" + dstPath)
	message := "Successfully copied " + srcPath + " to " + target.Host + ":" + dstPath
	notify.SendAlarm(message, false)
	itWorksNow(message, true)
	return nil
}
