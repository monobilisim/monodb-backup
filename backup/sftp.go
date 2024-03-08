package backup

import (
	"net"
	"os"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func SendSFTP(srcPath, dstPath, user, target, port string) error {
	sock, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
	if err != nil {
		return err
	}

	sockAgent := agent.NewClient(sock)

	signers, err := sockAgent.Signers()
	if err != nil {
		return err
	}
	auths := []ssh.AuthMethod{ssh.PublicKeys(signers...)}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, _ := ssh.Dial("tcp", target+":"+port, config)
	defer func() {
		err = client.Close()
		if err != nil {
			// TODO
		}
	}()

	sftpCli, err := sftp.NewClient(client)
	if err != nil {
		return err
	}
	defer func() {
		err = sftpCli.Close()
		if err != nil {
			// TODO
		}
	}()

	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() {
		err = src.Close()
		if err != nil {
			// TODO
		}
	}()

	dst, err := sftpCli.Create(dstPath)
	if err != nil {
		return err
	}
	defer func() {
		err = dst.Close()
		if err != nil {
			// TODO
		}
	}()

	if _, err := dst.ReadFrom(src); err != nil {
		return err
	}
	return nil
}
