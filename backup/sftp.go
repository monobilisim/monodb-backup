package backup

import (
	"monodb-backup/config"
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
	client, err := ConnectToSSH(target)
	if err != nil {
		return err
	}
	defer func() {
		err = client.Close()
		if err != nil {
			logger.Error("Couldn't close SSH client - Error: " + err.Error())
		}
	}()

	sftpCli, err := sftp.NewClient(client)
	if err != nil {
		logger.Error("Couldn't create an SFTP client - Error: " + err.Error())
		// notify.SendAlarm("Couldn't upload backup "+srcPath+" to "+target.Host+":"+dstPath+"\nCouldn't create an SFTP client - Error: "+err.Error(), true)
		return err
	}
	defer func() {
		err = sftpCli.Close()
		if err != nil {
			logger.Error("Couldn't close SFTP client - Error: " + err.Error())
			// notify.SendAlarm("Couldn't close SFTP client - Error: "+err.Error(), true)
		}
	}()

	src, err := os.Open(srcPath)
	if err != nil {
		logger.Error("Couldn't open source file " + srcPath + " for copying - Error: " + err.Error())
		// notify.SendAlarm("Couldn't upload backup "+srcPath+" to "+target.Host+":"+dstPath+"\nCouldn't open source file "+srcPath+" for copying - Error: "+err.Error(), true)
		return err
	}
	defer func() {
		err = src.Close()
		if err != nil {
			logger.Error("Couldn't close source file: " + srcPath + " - Error: " + err.Error())
			// notify.SendAlarm("Couldn't close source file: "+srcPath+" - Error: "+err.Error(), true)
		}
	}()

	err = sendOverSFTP(srcPath, dstPath, src, target, sftpCli)
	if err != nil {
		return err
	}

	if params.Rotation.Enabled {
		shouldRotate, newDst := rotate(db, target.Host)
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
			updateRotatedTimestamp(db, target.Host)
		}
	}

	if params.Rotation.Keep.Daily > 0 || params.Rotation.Keep.Weekly > 0 || params.Rotation.Keep.Monthly > 0 {
		if err := CleanupSFTP(target, sftpCli, db); err != nil {
			logger.Error("Error during SFTP cleanup: " + err.Error())
		}
	}

	return nil
}

func CleanupSFTP(target config.Target, client *sftp.Client, db string) error {
	cleanupDir := func(dir string, keep int, period string) error {
		if keep == 0 {
			return nil
		}

		realPath := target.Path + "/" + dir
		files, err := client.ReadDir(realPath)
		if err != nil {
			return nil
		}

		var backups []BackupFile
		var recurseDirs []string

		for _, f := range files {
			if f.IsDir() {
				recurseDirs = append(recurseDirs, f.Name())
				continue
			}
			backups = append(backups, BackupFile{
				Name: f.Name(),
				Time: f.ModTime(),
				Path: realPath + "/" + f.Name(),
			})
		}

		for _, subDir := range recurseDirs {
			subPath := realPath + "/" + subDir
			subFiles, err := client.ReadDir(subPath)
			if err != nil {
				continue
			}
			for _, f := range subFiles {
				if !f.IsDir() {
					backups = append(backups, BackupFile{
						Name: f.Name(),
						Time: f.ModTime(),
						Path: subPath + "/" + f.Name(),
					})
				}
			}
		}

		toDelete := getFilesToDelete(backups, period, keep)
		for _, f := range toDelete {
			err := client.Remove(f.Path)
			if err != nil {
				logger.Error("Failed to delete old backup " + f.Path + ": " + err.Error())
			} else {
				logger.Info("Deleted old backup: " + f.Path)
			}
		}
		return nil
	}

	if params.Rotation.Keep.Daily > 0 {
		if err := cleanupDir("Daily", params.Rotation.Keep.Daily, "daily"); err != nil {
			return err
		}
	}
	if params.Rotation.Keep.Weekly > 0 {
		if err := cleanupDir("Weekly", params.Rotation.Keep.Weekly, "weekly"); err != nil {
			return err
		}
	}
	if params.Rotation.Keep.Monthly > 0 {
		if err := cleanupDir("Monthly", params.Rotation.Keep.Monthly, "monthly"); err != nil {
			return err
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
		// notify.SendAlarm("Couldn't upload backup "+srcPath+" to "+target.Host+":"+dstPath+"\nCouldn't create folders "+newPath+" - Error: "+err.Error(), true)
		return err
	}
	dst, err := sftpCli.Create(dstPath)
	if err != nil {
		logger.Error("Couldn't create file " + dstPath + " - Error: " + err.Error())
		// notify.SendAlarm("Couldn't upload backup "+srcPath+" to "+target.Host+":"+dstPath+"\nCouldn't create file "+dstPath+" - Error: "+err.Error(), true)
		return err
	}
	defer func() {
		err = dst.Close()
		if err != nil {
			logger.Error("Couldn't close destination file: " + dstPath + " - Error: " + err.Error())
			// notify.SendAlarm("Couldn't close destination file: "+dstPath+" - Error: "+err.Error(), true)
		}
	}()
	logger.Info("Created destination file " + dstPath + " Now starting copying")

	if _, err := dst.ReadFrom(src); err != nil {
		logger.Error("Couldn't read from file " + srcPath + " to write at " + dstPath + " - Error: " + err.Error())
		// notify.SendAlarm("Couldn't upload backup "+srcPath+" to "+target.Host+":"+dstPath+"\nCouldn't read from file "+srcPath+" to write at "+dstPath+" - Error: "+err.Error(), true)
		return err
	}
	message := "Successfully copied " + srcPath + " to " + target.Host + ":" + dstPath
	logger.Info(message)
	// notify.SendAlarm(message, false)
	// itWorksNow(message, true)
	return nil
}

func ConnectToSSH(target config.Target) (*ssh.Client, error) {
	port := target.Port
	if port == "" {
		port = "22"
	}
	sock, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
	if err != nil {
		logger.Error("Couldn't get environment variable SSH_AUTH_SOCK - Error: " + err.Error())
		return nil, err
	}

	sockAgent := agent.NewClient(sock)

	signers, err := sockAgent.Signers()
	if err != nil {
		logger.Error("Couldn't get signers for ssh keys - Error: " + err.Error())
		return nil, err
	}
	auths := []ssh.AuthMethod{ssh.PublicKeys(signers...)}

	sshConfig := &ssh.ClientConfig{
		User:            target.User,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	return ssh.Dial("tcp", target.Host+":"+port, sshConfig)
}
