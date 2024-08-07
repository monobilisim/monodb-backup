package backup

import (
	"bytes"
	"io"
	"monodb-backup/config"
	"monodb-backup/notify"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func getPSQLList() []string {
	var remote config.Remote = params.Remote
	psqlArgs := []string{"-lqt"}
	var stderr bytes.Buffer

	if params.Remote.IsRemote {
		var pglink string
		if remote.Port != "" {
			pglink = "postgresql://" + remote.User + ":" + remote.Password + "@" + remote.Host + ":" + remote.Port + "/postgres"
		} else {
			pglink = "postgresql://" + remote.User + ":" + remote.Password + "@" + remote.Host + "/postgres"
		}
		psqlArgs = append(psqlArgs, pglink)
	}
	cmd := exec.Command("/usr/bin/psql", psqlArgs...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		notify.SendAlarm("Couldn't get the list of databases - stdout: "+string(out)+"\nError: "+stderr.String(), true)
		logger.Fatal("Couldn't get the list of databases - Error: " + string(out) + "\nError: " + stderr.String())
		return nil
	}

	var dbList []string
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		if len(line) > 0 {
			ln := strings.TrimSpace(strings.Split(string(line), "|")[0])
			if ln == "" || ln == "template0" || ln == "template1" || ln == "postgres" {
				continue
			}
			dbList = append(dbList, ln)
		}
	}
	return dbList
}

func dumpAndUploadPSQL(db string, pipeWriters []*io.PipeWriter) error {
	var cmd *exec.Cmd
	var stderr bytes.Buffer
	var remote config.Remote = params.Remote
	var writers []io.Writer
	for _, pw := range pipeWriters {
		writers = append(writers, pw)
	}

	var pgDumpArgs []string
	if remote.IsRemote {
		var pglink string
		if remote.Port != "" {
			pglink = "postgresql://" + remote.User + ":" + remote.Password + "@" + remote.Host + ":" + remote.Port + "/" + db
		} else {
			pglink = "postgresql://" + remote.User + ":" + remote.Password + "@" + remote.Host + "/" + db
		}
		pgDumpArgs = append(pgDumpArgs, pglink)
	} else {
		pgDumpArgs = append(pgDumpArgs, db)
	}

	pgDumpArgs = append(pgDumpArgs, "-Fc")

	cmd = exec.Command("/usr/bin/pg_dump", pgDumpArgs...)
	// cmd.Stdout = pipeWriter
	cmd.Stderr = os.Stderr
	cmd.Stdout = io.MultiWriter(writers...)
	err := cmd.Run()
	if err != nil {
		logger.Error("Couldn't back up " + db + " - Error: " + err.Error() + " - " + stderr.String())
		return err
	}
	return nil
}

func dumpPSQLDb(db string, dst string) (string, string, error) {
	encrypted := params.ArchivePass != ""
	var dumpPath string
	var format string
	var cmd *exec.Cmd
	var stderr bytes.Buffer
	var stderr1 bytes.Buffer
	var remote config.Remote = params.Remote

	name := dumpName(db, params.Rotation, "")

	if params.Format == "7zip" {
		format = "7zip"
	} else {
		format = "gzip"
	}
	logger.Info("PostgreSQL backup started. DB: " + db + " - Compression algorithm: " + format + " - Encrypted: " + strconv.FormatBool(encrypted))

	var pgDumpArgs []string
	if remote.IsRemote {
		var pglink string
		if remote.Port != "" {
			pglink = "postgresql://" + remote.User + ":" + remote.Password + "@" + remote.Host + ":" + remote.Port + "/" + db
		} else {
			pglink = "postgresql://" + remote.User + ":" + remote.Password + "@" + remote.Host + "/" + db
		}
		pgDumpArgs = append(pgDumpArgs, pglink)
	} else {
		pgDumpArgs = append(pgDumpArgs, db)
	}
	if err := os.MkdirAll(filepath.Dir(dst+"/"+name), 0770); err != nil {
		logger.Error("Couldn't create parent directories at backup destination. Name: " + name + " - Error: " + err.Error())
		return "", "", err
	}

	if !encrypted {
		name = name + ".dump"
		dumpPath = dst + "/" + name
		pgDumpArgs = append(pgDumpArgs, "-Fc", "-f", dumpPath)
		cmd = exec.Command("/usr/bin/pg_dump", pgDumpArgs...)
		cmd.Stderr = &stderr1
		err := cmd.Run()
		if err != nil {
			logger.Error("Couldn't back up " + db + " - Error: " + err.Error() + " - " + stderr1.String())
			return "", "", err
		}
	} else {
		if format == "gzip" {
			name = name + ".dump.7z"
			dumpPath = dst + "/" + name
			pgDumpArgs = append(pgDumpArgs, "-Fc")
			cmd = exec.Command("/usr/bin/pg_dump", pgDumpArgs...)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				logger.Error("Couldn't back up " + db + " - Error: " + err.Error())
				return "", "", err
			}
			cmd.Stderr = &stderr1
			err = cmd.Start()
			if err != nil {
				logger.Error("Couldn't back up " + db + " - Error: " + err.Error() + " - " + stderr1.String())
				return "", "", err
			}
			cmd2 := exec.Command("7z", "a", "-t7z", "-mx0", "-mhe=on", "-p"+params.ArchivePass, "-si", dumpPath)
			cmd2.Stdin = stdout
			cmd2.Stderr = &stderr

			err = cmd2.Run()
			if err != nil {
				logger.Error("Couldn't back up " + db + err.Error() + " - " + stderr.String())
				return "", "", err
			}
		} else if format == "7zip" {
			name = name + ".sql.7z"
			dumpPath = dst + "/" + name
			cmd = exec.Command("/usr/bin/pg_dump", pgDumpArgs...)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				logger.Error("Couldn't back up " + db + " - Error: " + err.Error())
				return "", "", err
			}
			cmd.Stderr = &stderr1
			err = cmd.Start()
			if err != nil {
				logger.Error("Couldn't back up " + db + " - Error: " + err.Error() + " - " + stderr1.String())
				return "", "", err
			}
			cmd2 := exec.Command("7z", "a", "-t7z", "-ms=on", "-mhe=on", "-p"+params.ArchivePass, "-si", dumpPath)
			cmd2.Stdin = stdout
			cmd2.Stderr = &stderr

			err = cmd2.Run()
			if err != nil {
				logger.Error("Couldn't back up " + db + " - Error: " + err.Error() + " - " + stderr.String())
				return "", "", err
			}
		}
	}
	logger.Info("Successfully backed up " + db + " at: " + dumpPath)
	return dumpPath, name, nil
}
