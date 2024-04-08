package backup

import (
	"bytes"
	"database/sql"
	"errors"
	"monodb-backup/notify"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	_ "github.com/go-sql-driver/mysql"
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

// func getTableList(db string) {
// 	dblist := runCommand("-Ne SHOW TABLES FROM " + db)
// 	fmt.Println(dblist)
// 	dblist = runCommand("-Ne SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = " + db)
// 	fmt.Println(dblist)

// }

//	func runCommand(arguement string) []string {
//		mysqlArgs := []string{arguement}
//		var stderr bytes.Buffer
//		var stdout bytes.Buffer
//		if params.Remote.IsRemote {
//			mysqlArgs = append(mysqlArgs, "-h"+params.Remote.Host, "--port="+params.Remote.Port, "-u"+params.Remote.User, "-p"+params.Remote.Password)
//		}
//		cmd := exec.Command("/usr/bin/mysql", mysqlArgs...)
//		cmd.Stderr = &stderr
//		cmd.Stdout = &stdout
//		err := cmd.Run()
//		if err != nil {
//			fmt.Println(cmd.String())
//			notify.SendAlarm("Couldn't get the list of databases - Error: "+stdout.String()+"\n"+stderr.String()+"\n"+err.Error(), true)
//			logger.Fatal("Couldn't get the list of databases - Error: " + stdout.String() + "\n" + stderr.String() + "\n" + err.Error())
//			return make([]string, 0)
//		}
//		var dbList []string
//		for _, line := range bytes.Split(stdout.Bytes(), []byte{'\n'}) {
//			if len(line) > 0 {
//				ln := string(line)
//				dbList = append(dbList, ln)
//			}
//		}
//		return dbList
//	}
func getTableList(dbName, path string) ([]string, string, error) {
	dsn := params.Remote.User + ":" + params.Remote.Password + "@tcp(" + params.Remote.Host + ":" + params.Remote.Port + ")/"

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		logger.Error(err.Error())
		return make([]string, 0), "", err
	}
	defer db.Close()

	rows, err := db.Query("SHOW TABLES FROM " + dbName)
	if err != nil {
		logger.Error(err.Error())
		return make([]string, 0), "", err
	}
	defer rows.Close()

	var table string
	var tableList []string
	for rows.Next() {
		if err := rows.Scan(&table); err != nil {
			logger.Error(err.Error())
			return make([]string, 0), "", err
		}
		tableList = append(tableList, table)
	}

	if err := rows.Err(); err != nil {
		logger.Error(err.Error())
		return make([]string, 0), "", err
	}

	newrows, err := db.Query("SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = '" + dbName + "'")
	if err != nil {
		logger.Error(err.Error())
		return make([]string, 0), "", err
	}
	defer newrows.Close()

	var charSet, collationName string
	for newrows.Next() {
		if err := newrows.Scan(&charSet, &collationName); err != nil {
			logger.Error(err.Error())
			return make([]string, 0), "", err
		}
	}

	if err := newrows.Err(); err != nil {
		logger.Error(err.Error())
		return make([]string, 0), "", err
	}

	filename := path + "/" + dbName + ".meta"

	if err := os.WriteFile(filename, []byte(charSet+" "+collationName), 0666); err != nil {
		logger.Error(err.Error())
		return make([]string, 0), "", err
	}

	return tableList, filename, nil
}

func dumpDBWithTables(db, dst string) ([]string, []string, error) {
	var dumpPaths []string
	var names []string
	if !params.Minio.S3FS.ShouldMount {
		dst = dst + "/" + db
	}
	oldDst := dst
	if !params.Rotation.Enabled && params.Minio.S3FS.ShouldMount {
		dst = dst + "/" + minioPath() + "/" + db
	}
	if params.Rotation.Enabled {
		dst = dst + "/" + db
	}
	if err := os.MkdirAll(dst, 0770); err != nil {
		logger.Error("Couldn't create parent direectories at backup destination. dst: " + dst + " - Error: " + err.Error())
		return make([]string, 0), make([]string, 0), err
	}
	tableList, metaFile, err := getTableList(db, dst)
	if err != nil {
		logger.Error("Couldn't get the list of tables. Error: " + err.Error())
		return nil, nil, err
	}
	dumpPaths = append(dumpPaths, metaFile)
	if !params.Minio.S3FS.ShouldMount {
		names = append(names, filepath.Dir(dumpName(db, params.Rotation, ""))+"/"+db+".meta")
	} else {
		if params.Rotation.Enabled {
			names = append(names, filepath.Dir(dumpName(db, params.Rotation, ""))+"/"+minioPath()+"/"+db+".meta")
		} else {
			names = append(names, filepath.Dir(dumpName(db, params.Rotation, ""))+"/"+db+".meta")
		}
	}
	for _, table := range tableList {
		filePath, name, err := dumpTable(db, table, oldDst+db) //TODO +
		if err != nil {
			logger.Error("Couldn't dump databases. Error: " + err.Error())
			return nil, nil, err
		}
		dumpPaths = append(dumpPaths, filePath)
		names = append(names, name)
	}
	return dumpPaths, names, nil
}

func dumpTable(db, table, dst string) (string, string, error) {
	var name string
	encrypted := params.ArchivePass != ""
	var format string

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

	mysqlArgs = append(mysqlArgs, "--single-transaction", "--quick", "--skip-lock-tables", "--routines", "--triggers", "--events", db, table)
	name = dumpName(db, params.Rotation, db+"_"+table)
	return mysqlDump(db, name, dst, encrypted, mysqlArgs)
}

func dumpMySQLDb(db, dst string) (string, string, error) {
	var name string
	encrypted := params.ArchivePass != ""
	var format string

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

	mysqlArgs = append(mysqlArgs, "--single-transaction", "--quick", "--skip-lock-tables", "--routines", "--triggers", "--events", db)

	if db == "mysql" {
		mysqlArgs = append(mysqlArgs, "user")
		name = dumpName(db+"_users", params.Rotation, "")
	} else {
		name = dumpName(db, params.Rotation, "")
	}
	if err := os.MkdirAll(filepath.Dir(dst+"/"+name), 0770); err != nil {
		logger.Error("Couldn't create parent direectories at backup destination. Name: " + name + " - Error: " + err.Error())
		return "", "", err
	}
	return mysqlDump(db, name, dst, encrypted, mysqlArgs)

}

func mysqlDump(db, name, dst string, encrypted bool, mysqlArgs []string) (string, string, error) {
	var cmd *exec.Cmd
	var cmd2 *exec.Cmd
	var stderr bytes.Buffer
	var format string

	if encrypted {
		format = "7zip"
	} else if params.Format == "gzip" {
		format = "gzip"
	} else {
		format = "7zip"
	}
	output := make([]byte, 100)
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
