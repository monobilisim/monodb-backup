package backup

import (
	"bytes"
	"monodb-backup/config"
	"os"
	"os/exec"
	"strconv"
	"time"
)

func getMySQLList(params config.Remote, logger Logger) ([]string, error) {
	mysqlArgs := []string{"-e SHOW DATABASES;"}
	if params.IsRemote {
		mysqlArgs = append(mysqlArgs, "-h"+params.Host, "--port="+params.Port, "-u"+params.User, "-p"+params.Password)
	}
	cmd := exec.Command("/usr/bin/mysql", mysqlArgs...)
	out, err := cmd.Output()
	if err != nil {
		logger.Error("Could not get database list: " + err.Error())
		logger.Error("Command output: " + string(out))
		return nil, err
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
	return dbList, nil
}

func dumpMySQLDb(db string, dst string, params config.Params, logger Logger) (string, string, error) {
	encrypted := params.ArchivePass != ""
	var format string
	var name string
	var cmd *exec.Cmd
	var cmd2 *exec.Cmd
	if encrypted {
		format = "7zip"
	} else {
		format = params.Format
	}

	logger.Info("MySQL backup started. DB: " + db + " - Compression algorithm: " + format + " - Encrypted: " + strconv.FormatBool(encrypted))

	var mysqlArgs []string
	if params.Remote.IsRemote {
		mysqlArgs = append(mysqlArgs, "-h"+params.Remote.Host, "--port="+params.Remote.Port, "-u"+params.Remote.User, "-p"+params.Remote.Password, db)
	} else {
		mysqlArgs = append(mysqlArgs, "-u"+params.Remote.User, "-p"+params.Remote.Password, db)
	}
	date := rightNow{
		year:  time.Now().Format("2006"),
		month: time.Now().Format("01"),
	}

	if db == "mysql" {
		mysqlArgs = append(mysqlArgs, "user")
		name = dumpName(db+"_users", params.Rotation)
	} else {
		name = dumpName(db, params.Rotation)
	}
	var dumpPath string
	_ = os.MkdirAll(dst+"/"+date.year+"/"+date.month, os.ModePerm)

	cmd = exec.Command("/usr/bin/mysqldump", mysqlArgs...)
	stdout, err := cmd.StdoutPipe()
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

		err = cmd2.Start()
		if err != nil {
			logger.Error("Couldn't compress " + db + " - Error: " + err.Error())
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

		err = cmd2.Run()
	}

	logger.Info("Successfully backed up " + db + " at: " + dumpPath)
	return dumpPath, name, nil
}
