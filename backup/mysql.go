package backup

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"monodb-backup/notify"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	_ "github.com/go-sql-driver/mysql"
)

// processSemaphore controls the number of concurrent dump processes
var processSemaphore chan struct{}
var semaphoreOnce sync.Once

// initSemaphore initializes the process semaphore based on config
func initSemaphore() {
	semaphoreOnce.Do(func() {
		maxProcesses := params.MaxConcurrentProcesses
		if maxProcesses <= 0 {
			maxProcesses = 10 // Fallback default
		}
		processSemaphore = make(chan struct{}, maxProcesses)
		logger.Info("Initialized process semaphore with max " + strconv.Itoa(maxProcesses) + " concurrent processes")
	})
}

func isCommandAvailable(name string) (bool, string) {
	path, err := exec.LookPath(name)
	if err != nil {
		return false, ""
	}
	return true, path
}

var mysqlCommand string = "/usr/bin/mysql"
var dumpCommand string = "/usr/bin/mysqldump"

func getMySQLList() []string {
	mariadb, mysqlCommandTMP := isCommandAvailable("mariadb")
	if mariadb {
		mysqlCommand = mysqlCommandTMP
	}
	mysqlArgs := []string{"-e SHOW DATABASES;"}
	if params.Remote.IsRemote {
		mysqlArgs = append(mysqlArgs, "-h"+params.Remote.Host, "--port="+params.Remote.Port, "-u"+params.Remote.User, "-p"+params.Remote.Password)
	}
	cmd := exec.Command(mysqlCommand, mysqlArgs...)
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
// 	dblist = runCommand("-Ne SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = " + db)

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
//			// notify.SendAlarm("Couldn't get the list of databases - Error: "+stdout.String()+"\n"+stderr.String()+"\n"+err.Error(), true)
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

	filename := path + "/" + dbName + "/" + dbName + ".meta"

	if err := os.WriteFile(filename, []byte(charSet+" "+collationName), 0666); err != nil {
		logger.Error(err.Error())
		return make([]string, 0), "", err
	}

	return tableList, filename, nil
}

func dumpAndUploadMySQL(db string, pipeWriters []*io.PipeWriter) error {
	encrypted := params.ArchivePass != ""
	var format string
	var stderr bytes.Buffer
	output := make([]byte, 100)
	var writers []io.Writer
	for _, pw := range pipeWriters {
		writers = append(writers, pw)
	}

	mariadb, dumpCommandTMP := isCommandAvailable("mariadb-dump")
	if mariadb {
		dumpCommand = dumpCommandTMP
	}

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
	}
	cmd := exec.Command(dumpCommand, mysqlArgs...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.Error("Couldn't back up " + db + " - Error: " + err.Error())
		return err
	}
	stderr2, err := cmd.StderrPipe()
	if err != nil {
		logger.Error("Couldn't back up " + db + " - Error: " + err.Error())
		return err
	}

	err = cmd.Start()
	if err != nil {
		logger.Error("Couldn't back up " + db + " - Error: " + err.Error())
		return err
	}

	cmd2 := exec.Command("gzip")
	cmd2.Stdin = stdout

	cmd2.Stdout = io.MultiWriter(writers...)
	cmd2.Stderr = &stderr

	err = cmd2.Start()
	if err != nil {
		logger.Error("Couldn't compress " + db + " - Error: " + err.Error() + " - " + stderr.String())
		return err
	}

	err = cmd2.Wait()
	if err != nil {
		logger.Error("Couldn't compress " + db + " - Error: " + err.Error())
		return err
	}
	n, _ := stderr2.Read(output)
	if n > 0 {
		if !strings.Contains(string(string(output[:n])), "[Warning] Using a password on the command line interface can be insecure.") {
			logger.Error("Couldn't back up " + db + " - Error: " + string(string(output[:n])))
			return errors.New(string(output[:n]))
		}
	}
	return nil
}

func dumpDBWithTables(db, dst string) ([]string, []string, error) {
	// Initialize semaphore on first use
	initSemaphore()

	var dumpPaths, names []string
	oldDst := dst
	if err := os.MkdirAll(dst+"/"+db, 0770); err != nil {
		message := "Couldn't create parent direectories at backup destination. dst: " + dst + "/" + db + " - Error: " + err.Error()
		logger.Error(message)
		notify.FailedDBList = append(notify.FailedDBList, db+" - "+message)
		return make([]string, 0), make([]string, 0), err
	}
	tableList, metaFile, err := getTableList(db, dst)
	if err != nil {
		message := "Couldn't get the list of tables. Error: " + err.Error()
		logger.Error(message)
		notify.FailedDBList = append(notify.FailedDBList, db+" - "+message)
		return nil, nil, err
	}
	dumpPaths = append(dumpPaths, metaFile)
	names = append(names, filepath.Dir(dumpName(db, params.Rotation, ""))+"/"+db+".meta")

	// Use goroutines with semaphore to control concurrency
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstError error

	type dumpResult struct {
		fPath string
		name  string
	}
	results := make([]dumpResult, 0, len(tableList))

	for _, table := range tableList {
		if db == "mysql" && table != "user" {
			continue
		}

		wg.Add(1)
		go func(table string) {
			defer wg.Done()

			// Acquire semaphore slot
			processSemaphore <- struct{}{}
			defer func() { <-processSemaphore }() // Release semaphore slot

			fPath, name, err := dumpTable(db, table, oldDst)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				if firstError == nil {
					firstError = err
				}
				message := "Couldn't dump table " + table + ". Error: " + err.Error()
				logger.Error(message)
				notify.FailedDBList = append(notify.FailedDBList, db+" - table: "+table+" - "+message)
			} else {
				results = append(results, dumpResult{fPath: fPath, name: name})
			}
		}(table)
	}

	wg.Wait()

	if firstError != nil {
		return nil, nil, firstError
	}

	// Collect results
	for _, result := range results {
		dumpPaths = append(dumpPaths, result.fPath)
		names = append(names, result.name)
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

	logger.Info("MySQL backup started. DB: " + db + " Table: " + table + " - Compression algorithm: " + format + " - Encrypted: " + strconv.FormatBool(encrypted))

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
		logger.Error("Couldn't create parent directories at backup destination. Name: " + name + " - Error: " + err.Error())
		return "", "", err
	}
	return mysqlDump(db, name, dst, encrypted, mysqlArgs)

}

func mysqlDump(db, name, dst string, encrypted bool, mysqlArgs []string) (string, string, error) {
	var cmd *exec.Cmd
	var cmd2 *exec.Cmd
	var stderr bytes.Buffer
	var format string

	mariadb, dumpCommandTMP := isCommandAvailable("mariadb-dump")
	if mariadb {
		dumpCommand = dumpCommandTMP
	}

	if encrypted {
		format = "7zip"
	} else if params.Format == "gzip" {
		format = "gzip"
	} else {
		format = "7zip"
	}
	output := make([]byte, 100)
	var dumpPath string
	cmd = exec.Command(dumpCommand, mysqlArgs...)
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

		if err := os.MkdirAll(filepath.Dir(dumpPath), 0770); err != nil {
			logger.Error("Couldn't create parent direectories at backup destination. dst: " + dst + " - Error: " + err.Error())
			return "", "", err
		}

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
		// Zombi process engelleme: mysqldump/mariadb-dump process'ini topla
		err = cmd.Wait()
		if err != nil {
			logger.Error("mysqldump process failed for " + db + " - Error: " + err.Error())
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
		// Zombi process engelleme: mysqldump/mariadb-dump process'ini topla
		err = cmd.Wait()
		if err != nil {
			logger.Error("mysqldump process failed for " + db + " - Error: " + err.Error())
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
