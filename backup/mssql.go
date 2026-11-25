package backup

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"monodb-backup/config"
	"monodb-backup/notify"

	_ "github.com/denisenkom/go-mssqldb"
)

const mssqlListQuery = "SELECT name FROM sys.databases WHERE database_id > 4 AND state_desc = 'ONLINE' AND source_database_id IS NULL ORDER BY name"

var mssqlDB *sql.DB

func splitMSSQLHost(host string) (string, string) {
	name, instance, _ := strings.Cut(host, `\`)
	return name, instance
}

// "." and "(local)" are the Windows spellings of the local machine.
func isLocalAlias(name string) bool {
	return name == "." || strings.EqualFold(name, "(local)")
}

func mssqlDSN(r config.Remote) string {
	host, instance := splitMSSQLHost(r.Host)
	if isLocalAlias(host) {
		host = "localhost"
	}
	u := url.URL{Scheme: "sqlserver"}
	if r.Port != "" {
		u.Host = net.JoinHostPort(host, r.Port)
	} else if strings.Contains(host, ":") && instance == "" {
		u.Host = net.JoinHostPort(host, "1433")
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	if instance != "" {
		u.Path = "/" + instance
	}
	if r.User != "" {
		u.User = url.UserPassword(r.User, r.Password)
	}
	q := url.Values{}
	q.Set("database", "master")
	q.Set("app name", "monodb-backup")
	u.RawQuery = q.Encode()
	return u.String()
}

func isLocalHost(host string) (bool, error) {
	name, _ := splitMSSQLHost(host)
	if name == "" {
		return false, errors.New("empty host")
	}
	if isLocalAlias(name) {
		return true, nil
	}
	var ips []net.IP
	if ip := net.ParseIP(name); ip != nil {
		ips = []net.IP{ip}
	} else {
		var err error
		if ips, err = net.LookupIP(name); err != nil {
			return false, err
		}
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false, err
	}
	for _, ip := range ips {
		if ip.IsLoopback() {
			continue
		}
		found := false
		for _, a := range addrs {
			if ipNet, ok := a.(*net.IPNet); ok && ipNet.IP.Equal(ip) {
				found = true
				break
			}
		}
		if !found {
			return false, nil
		}
	}
	return len(ips) > 0, nil
}

// mssqlConnectError drops the *url.Error wrapper, whose text embeds the DSN with the password.
func mssqlConnectError(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return "Couldn't connect to MSSQL - Error: " + err.Error()
}

func mssqlFatal(msg string) {
	notify.SendAlarm(msg, true)
	logger.Fatal(msg)
}

func InitializeMSSQL() {
	if !params.Remote.IsRemote {
		mssqlFatal("Remote should be enabled when backing up MSSQL databases.")
		return
	}
	if port := params.Remote.Port; port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			mssqlFatal("MSSQL remote.port must be a number between 1 and 65535")
			return
		}
	}
	local, err := isLocalHost(params.Remote.Host)
	if err != nil {
		logger.Error("Couldn't resolve MSSQL host " + params.Remote.Host + " - Error: " + err.Error())
	}
	if err != nil || !local {
		mssqlFatal("MSSQL backups must run on the SQL Server host; remote.host " + params.Remote.Host + " is not an address of this machine")
		return
	}
	db, err := sql.Open("sqlserver", mssqlDSN(params.Remote))
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err = db.PingContext(ctx)
	}
	if err != nil {
		mssqlFatal(mssqlConnectError(err))
		return
	}
	if err := initServerDirOwner(params.BackupDestination); err != nil {
		mssqlFatal("Couldn't prepare backup destination for SQL Server - Error: " + err.Error())
		return
	}
	mssqlDB = db
}

func getMSSQLList() []string {
	fail := func(err error) []string {
		mssqlFatal("Couldn't get the list of databases - Error: " + err.Error())
		return nil
	}
	rows, err := mssqlDB.QueryContext(context.Background(), mssqlListQuery)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = rows.Close() }()

	var dbList []string
	for rows.Next() {
		var dbName string
		if err := rows.Scan(&dbName); err != nil {
			return fail(err)
		}
		dbList = append(dbList, dbName)
	}
	if err := rows.Err(); err != nil {
		return fail(err)
	}
	return dbList
}

func mssqlBackupStatement(compress bool) string {
	s := "BACKUP DATABASE @db TO DISK = @path WITH COPY_ONLY, FORMAT, INIT, CHECKSUM"
	if compress {
		s += ", COMPRESSION"
	}
	return s
}

// Express (EngineEdition 4) and Web editions reject COMPRESSION.
func mssqlSupportsCompression(ctx context.Context) (bool, error) {
	const q = "SELECT CASE WHEN CAST(SERVERPROPERTY('EngineEdition') AS int) = 4 OR CAST(SERVERPROPERTY('Edition') AS nvarchar(128)) LIKE 'Web%' THEN 0 ELSE 1 END"
	var ok int
	if err := mssqlDB.QueryRowContext(ctx, q).Scan(&ok); err != nil {
		return false, err
	}
	return ok == 1, nil
}

func checkMSSQLBackupVisible(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("SQL Server reported success but %s is not readable; monodb-backup must run on the SQL Server host: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("backup path %s is not a regular file", path)
	}
	if fi.Size() == 0 {
		return fmt.Errorf("backup file %s is empty; monodb-backup must run on the SQL Server host", path)
	}
	return nil
}

func encryptMSSQLBackup(bakPath string) (string, error) {
	archive := bakPath + ".7z"
	// 7z a updates an existing archive, which fails when it was encrypted with another password.
	_ = os.Remove(archive)
	var stderr bytes.Buffer
	cmd := exec.Command("7z", "a", "-t7z", "-mhe=on", "-mx0", "-p"+params.ArchivePass, archive, bakPath)
	cmd.Stderr = &stderr
	err := cmd.Run()
	_ = os.Remove(bakPath)
	if err != nil {
		_ = os.Remove(archive)
		return "", fmt.Errorf("7z failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return archive, nil
}

// With rotation off the removed dst is the destination itself, which the operator chowned once.
func restoreMSSQLDestination() {
	root, err := filepath.Abs(params.BackupDestination)
	if err == nil {
		if _, statErr := os.Lstat(root); statErr == nil {
			return
		}
		if err = os.MkdirAll(root, 0o770); err == nil {
			err = grantServerAccess(root, root)
		}
	}
	if err != nil {
		logger.Error("Couldn't recreate backup destination " + params.BackupDestination + " - Error: " + err.Error())
	}
}

func dumpMSSQLDB(dbName, dst string) (string, string, error) {
	name := dumpName(dbName, params.Rotation, "") + ".bak"
	dumpPath, err := filepath.Abs(filepath.Join(dst, filepath.FromSlash(name)))
	if err != nil {
		logger.Error("Couldn't resolve backup path under " + dst + " - Error: " + err.Error())
		return "", "", err
	}
	dir := filepath.Dir(dumpPath)
	root, err := filepath.Abs(params.BackupDestination)
	if err != nil {
		logger.Error("Couldn't resolve backup destination " + params.BackupDestination + " - Error: " + err.Error())
		return "", "", err
	}

	if _, err := dirsFrom(root, dir); err != nil {
		logger.Error("Refusing to back up " + dbName + " - Error: " + err.Error())
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o770); err != nil {
		logger.Error("Couldn't create parent directories at backup destination " + dst + ". Name: " + name + " - Error: " + err.Error())
		return "", "", err
	}
	if err := grantServerAccess(root, dir); err != nil {
		logger.Error("Couldn't grant SQL Server access to " + dir + " - Error: " + err.Error())
		return "", "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(params.CtxCancel)*time.Hour)
	defer cancel()

	compress, err := mssqlSupportsCompression(ctx)
	if err != nil {
		logger.Error("Couldn't detect MSSQL edition - Error: " + err.Error())
		return "", "", err
	}
	encrypted := params.ArchivePass != ""
	logger.Info("MSSQL backup started. DB: " + dbName + " - Compression: " + strconv.FormatBool(compress) + " - Encrypted: " + strconv.FormatBool(encrypted))

	_, err = mssqlDB.ExecContext(ctx, mssqlBackupStatement(compress), sql.Named("db", dbName), sql.Named("path", dumpPath))
	if err != nil {
		if rmErr := os.Remove(dumpPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			logger.Error("Couldn't remove partial backup " + dumpPath + " - Error: " + rmErr.Error())
		}
		logger.Error("Couldn't back up database: " + dbName + " - Error: " + err.Error())
		return "", "", err
	}
	if err := checkMSSQLBackupVisible(dumpPath); err != nil {
		_ = os.Remove(dumpPath)
		logger.Error("Couldn't back up database: " + dbName + " - Error: " + err.Error())
		return "", "", err
	}
	if !encrypted {
		return dumpPath, name, nil
	}
	archive, err := encryptMSSQLBackup(dumpPath)
	if err != nil {
		logger.Error("Couldn't encrypt backup of database: " + dbName + " - Error: " + err.Error())
		return "", "", err
	}
	return archive, name + ".7z", nil
}
