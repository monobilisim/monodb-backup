package backup

import (
	"bytes"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"monodb-backup/notify"
)

func mongoURI() string {
	remote := params.Remote
	if !remote.IsRemote {
		return "mongodb://localhost:27017"
	}
	u := url.URL{Scheme: "mongodb", Host: remote.Host, Path: "/"}
	if remote.Port != "" {
		u.Host += ":" + remote.Port
	}
	if remote.User != "" {
		u.User = url.UserPassword(remote.User, remote.Password)
		u.RawQuery = "authSource=admin"
	}
	return u.String()
}

func writeMongoSecretFile(suffix, content string) (string, error) {
	file, err := os.CreateTemp("", "monodb-backup-*"+suffix)
	if err != nil {
		return "", err
	}
	name := file.Name()
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func mongoShellCommand() (*exec.Cmd, func(), error) {
	const listDatabases = "db.adminCommand({listDatabases: 1, nameOnly: true}).databases.forEach(d => print(d.name))"
	if params.Remote.User == "" {
		return exec.Command("/usr/bin/mongosh", mongoURI(), "--quiet", "--eval", listDatabases), func() {}, nil
	}
	script := "const db = connect(" + strconv.Quote(mongoURI()) + ");\n" + listDatabases + "\n"
	name, err := writeMongoSecretFile(".js", script)
	if err != nil {
		return nil, nil, err
	}
	return exec.Command("/usr/bin/mongosh", "--nodb", "--quiet", "--file="+name), func() { _ = os.Remove(name) }, nil
}

func mongoDumpCommand(args []string) (*exec.Cmd, func(), error) {
	if params.Remote.User == "" {
		args = append([]string{"--uri=" + mongoURI()}, args...)
		return exec.Command("/usr/bin/mongodump", args...), func() {}, nil
	}
	name, err := writeMongoSecretFile(".yaml", "uri: "+strconv.Quote(mongoURI())+"\n")
	if err != nil {
		return nil, nil, err
	}
	args = append([]string{"--config=" + name}, args...)
	return exec.Command("/usr/bin/mongodump", args...), func() { _ = os.Remove(name) }, nil
}

func getMongoList() []string {
	var stderr bytes.Buffer
	cmd, cleanup, err := mongoShellCommand()
	if err != nil {
		notify.SendAlarm("Couldn't get the list of databases - Error: "+err.Error(), true)
		logger.Fatal("Couldn't get the list of databases - Error: " + err.Error())
		return nil
	}
	defer cleanup()
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		notify.SendAlarm("Couldn't get the list of databases - Error: "+err.Error()+" - "+stderr.String(), true)
		logger.Fatal("Couldn't get the list of databases - Error: " + err.Error() + " - " + stderr.String())
		return nil
	}

	var dbList []string
	for line := range strings.SplitSeq(string(out), "\n") {
		db := strings.TrimSpace(line)
		// local holds the oplog and config holds cluster metadata; neither restores meaningfully.
		if db != "" && db != "local" && db != "config" {
			dbList = append(dbList, db)
		}
	}
	return dbList
}

func dumpAndUploadMongo(db string, pipeWriters []*io.PipeWriter) error {
	var writers []io.Writer
	for _, pw := range pipeWriters {
		writers = append(writers, pw)
	}
	cmd, cleanup, err := mongoDumpCommand([]string{"--db=" + db, "--archive", "--gzip", "--quiet"})
	if err != nil {
		return err
	}
	defer cleanup()
	cmd.Stderr = os.Stderr
	cmd.Stdout = io.MultiWriter(writers...)
	if err := cmd.Run(); err != nil {
		logger.Error("Couldn't back up " + db + " - Error: " + err.Error())
		return err
	}
	return nil
}

func runMongoEncryptedPipeline(dumpCmd, sevenZipCmd *exec.Cmd) error {
	stdout, err := dumpCmd.StdoutPipe()
	if err != nil {
		return err
	}
	sevenZipCmd.Stdin = stdout
	if err := dumpCmd.Start(); err != nil {
		return err
	}
	if err := sevenZipCmd.Run(); err != nil {
		_ = dumpCmd.Process.Kill()
		_ = dumpCmd.Wait()
		return err
	}
	return dumpCmd.Wait()
}

func dumpMongoDB(db string, dst string) (string, string, error) {
	encrypted := params.ArchivePass != ""
	var stderr, stderr1 bytes.Buffer
	format := "gzip"
	if params.Format == "7zip" {
		format = "7zip"
	}
	logger.Info("MongoDB backup started. DB: " + db + " - Compression algorithm: " + format + " - Encrypted: " + strconv.FormatBool(encrypted))

	name := dumpName(db, params.Rotation, "")
	if err := os.MkdirAll(filepath.Dir(dst+"/"+name), 0770); err != nil {
		logger.Error("Couldn't create parent directories at backup destination. Name: " + name + " - Error: " + err.Error())
		return "", "", err
	}

	args := []string{"--uri=" + mongoURI(), "--db=" + db, "--quiet"}
	if !encrypted {
		name += ".archive.gz"
		dumpPath := dst + "/" + name
		cmd, cleanup, err := mongoDumpCommand(append(args, "--archive="+dumpPath, "--gzip"))
		if err != nil {
			return "", "", err
		}
		defer cleanup()
		cmd.Stderr = &stderr1
		if err := cmd.Run(); err != nil {
			logger.Error("Couldn't back up " + db + " - Error: " + err.Error() + " - " + stderr1.String())
			return "", "", err
		}
		logger.Info("Successfully backed up " + db + " at: " + dumpPath)
		return dumpPath, name, nil
	}

	sevenZipArgs := []string{"a", "-t7z", "-mhe=on", "-p" + params.ArchivePass, "-si"}
	args = append(args, "--archive")
	if format == "gzip" {
		name += ".archive.gz.7z"
		args = append(args, "--gzip")
		sevenZipArgs = append(sevenZipArgs, "-mx0")
	} else {
		name += ".archive.7z"
		sevenZipArgs = append(sevenZipArgs, "-ms=on")
	}
	dumpPath := dst + "/" + name

	cmd, cleanup, err := mongoDumpCommand(args)
	if err != nil {
		return "", "", err
	}
	defer cleanup()
	cmd.Stderr = &stderr1
	cmd2 := exec.Command("7z", append(sevenZipArgs, dumpPath)...)
	cmd2.Stderr = &stderr
	if err := runMongoEncryptedPipeline(cmd, cmd2); err != nil {
		logger.Error("Couldn't back up " + db + " - Error: " + err.Error() + " - " + stderr.String() + stderr1.String())
		return "", "", err
	}
	logger.Info("Successfully backed up " + db + " at: " + dumpPath)
	return dumpPath, name, nil
}
