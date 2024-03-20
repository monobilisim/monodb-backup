package backup

import (
	"bytes"
	"errors"
	"monodb-backup/notify"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func getMySQLList() []string {
	mysqlArgs := []string{"-e SHOW DATABASES;"}
	if params.Remote.IsRemote {
		mysqlArgs = append(mysqlArgs, "-h"+params.Remote.Host, "--port="+params.Remote.Port, "-u"+params.Remote.User, "-p"+params.Remote.Password)
	}
	cmd := exec.Command("/usr/bin/mysql", mysqlArgs...)
	out, err := cmd.Output()
	if err != nil {
		notify.SendAlarm("Couldn't get the list of databases - Error: "+string(out), true)
		logger.Fatal("Couldn't get the list of databases - Error: " + string(out))
		return nil
	}

	var dbList []string
	for i, line := range bytes.Split(out, []byte{'\n'}) {
		if len(line) > 0 && i > 0 {
			ln := string(line)
			if ln == "" || ln == "information_schema" || ln == "performance_schema" || ln == "sys" {
				continue
			}
			dbList = append(dbList, ln)
		}
	}
	return dbList
}

func dumpMySQLDb(db, dst string) (string, string, error) {
	encrypted := params.ArchivePass != ""
	var format string
	var name string
	var cmd *exec.Cmd
	var cmd2 *exec.Cmd
	var stderr bytes.Buffer
	output := make([]byte, 100)

	if encrypted {
		format = "7zip"
	} else if params.Format == "gzip" {
		format = "gzip"
	} else {
		format = "7zip"
	}

	logger.Info("MySQL backup started. DB: " + db + " - Compression algorithm: " + format + " - Encrypted: " + strconv.FormatBool(encrypted))

	var mysqlArgs []string
	if params.Remote.IsRemote {
		mysqlArgs = append(mysqlArgs, "-h"+params.Remote.Host, "--port="+params.Remote.Port, "-u"+params.Remote.User, "-p"+params.Remote.Password)
	} else {
		mysqlArgs = append(mysqlArgs, "-u"+params.Remote.User, "-p"+params.Remote.Password)
	}

	mysqlArgs = append(mysqlArgs, "--single-transaction", "--quick", "--skip-lock-tables", "--routines", "--triggers", db)

	if db == "mysql" {
		mysqlArgs = append(mysqlArgs, "user")
		name = dumpName(db+"_users", params.Rotation)
	} else {
		name = dumpName(db, params.Rotation)
	}
	var dumpPath string

	cmd = exec.Command("/usr/bin/mysqldump", mysqlArgs...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.Error("Couldn't back up " + db + " - Error: " + err.Error())
		return "", "", err
	}
	stderr2, err := cmd.StderrPipe()
	if err != nil {
		logger.Error("Couldn't back up " + db + " - Error: " + err.Error())
		return "", "", err
	}

	err = cmd.Start()
	if err != nil {
		logger.Error("Couldn't back up " + db + " - Error: " + err.Error())
		return "", "", err
	}

	if !encrypted && format == "gzip" {
		name = name + ".sql.gz"
		dumpPath = dst + "/" + name
		cmd2 = exec.Command("gzip")
		cmd2.Stdin = stdout

		f, err := os.Create(dumpPath)
		if err != nil {
			logger.Error("Couldn't back up " + db + " - Error: " + err.Error())
			return "", "", err
		}

		defer func() {
			err := f.Close()
			if err != nil {
				logger.Error("Couldn't close file " + f.Name() + " - Error: " + err.Error())
			}
		}()

		cmd2.Stdout = f
		cmd2.Stderr = &stderr

		err = cmd2.Start()
		if err != nil {
			logger.Error("Couldn't compress " + db + " - Error: " + err.Error() + " - " + stderr.String())
			return "", "", err
		}

		err = cmd2.Wait()
		if err != nil {
			logger.Error("Couldn't compress " + db + " - Error: " + err.Error())
			return "", "", err
		}
	} else {
		name = name + ".sql.7z"
		dumpPath = dst + "/" + name
		if encrypted {
			cmd2 = exec.Command("7z", "a", "-t7z", "-ms=on", "-mhe=on", "-p"+params.ArchivePass, "-si", dumpPath)
		} else {
			cmd2 = exec.Command("7z", "a", "-t7z", "-ms=on", "-si", dumpPath)
		}

		cmd2.Stdin = stdout
		cmd2.Stderr = &stderr

		err = cmd2.Run()
		if err != nil {
			logger.Error("Couldn't compress " + db + " - Error: " + err.Error() + " - " + stderr.String())
			return "", "", err
		}
	}
	n, _ := stderr2.Read(output)
	if n > 0 {
		if !strings.Contains(string(string(output[:n])), "[Warning] Using a password on the command line interface can be insecure.") {
			logger.Error("Couldn't back up " + db + " - Error: " + string(string(output[:n])))
			return dumpPath, name, errors.New(string(output[:n]))
		}
	}
	return dumpPath, name, nil
}
