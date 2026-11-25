package backup

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/url"
	"os"
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

func mssqlDSN(r config.Remote) string {
	host, instance := splitMSSQLHost(r.Host)
	u := url.URL{Scheme: "sqlserver"}
	if r.Port != "" {
		u.Host = net.JoinHostPort(host, r.Port)
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
			return true, nil
		}
		for _, a := range addrs {
			if ipNet, ok := a.(*net.IPNet); ok && ipNet.IP.Equal(ip) {
				return true, nil
			}
		}
	}
	return false, nil
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
	if local, err := isLocalHost(params.Remote.Host); err != nil || !local {
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
		mssqlFatal("Couldn't connect to MSSQL - Error: " + err.Error())
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

func dumpMSSQLDB(dbName, dst string) (string, string, error) {
	var name string
	encrypted := params.ArchivePass != ""

	logger.Info("MSSQL backup started. DB: " + dbName + " - Encrypted: " + strconv.FormatBool(encrypted))
	name = dumpName(dbName, params.Rotation, "") + ".bak"
	dumpPath := dst + filepath.FromSlash(name)

	if err := os.MkdirAll(filepath.Dir(dumpPath), 0770); err != nil {
		logger.Error("Couldn't create parent directories at backup destination " + dst + ". Name: " + name + " - Error: " + err.Error())
		return "", "", err
	}
	query := "BACKUP DATABASE [" + dbName + "]" +
		" TO DISK = '" + dumpPath + "'" +
		" WITH COMPRESSION;"

	ctx := context.Background()
	_, err := mssqlDB.ExecContext(ctx, query)
	if err != nil {
		logger.Error("Couldn't back up database: " + dbName + " - Error: " + err.Error())
		return "", "", err
	}
	return dumpPath, name, nil
}
