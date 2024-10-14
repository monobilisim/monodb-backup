//go:build windows

package backup

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"monodb-backup/notify"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/windows"

	_ "github.com/denisenkom/go-mssqldb"
	"github.com/hectane/go-acl"
)

var mssqlDB *sql.DB

func InitializeMSSQL() {
	var host, port, user, password string
	var err error

	if params.Remote.IsRemote == true {
		host = params.Remote.Host
		port = params.Remote.Port
		user = params.Remote.User
		password = params.Remote.Password
	} else {
		notify.SendAlarm("Remote should be enabled when backing up MSSQL databases.", true)
		logger.Fatal("Remote should be enabled when backing up MSSQL databases.")
		return
	}
	connString := fmt.Sprintf("server=%s;user id=%s;password=%s;port=%s;encrypt=disable;trustServerCertificate=true",
		host, user, password, port)

	// Create connection pool
	mssqlDB, err = sql.Open("sqlserver", connString)
	if err != nil {
		log.Fatalf("Error creating connection pool: %v", err)
	}

}

func getMSSQLList() []string {
	ctx := context.Background()
	var dbList []string

	// Execute the query to get the list of databases
	rows, err := mssqlDB.QueryContext(ctx, "SELECT name FROM master.dbo.sysdatabases WHERE dbid > 4;")
	if err != nil {
		mssqlDB.Close()
		logger.Error(err.Error())
		return make([]string, 0)
	}
	defer rows.Close()

	for rows.Next() {
		var dbName string
		if err := rows.Scan(&dbName); err != nil {
			mssqlDB.Close()
			logger.Error(err.Error())
			return make([]string, 0)
		}
		dbList = append(dbList, dbName)
	}

	if err := rows.Err(); err != nil {
		mssqlDB.Close()
		logger.Error(err.Error())
		return make([]string, 0)
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
	if err := acl.Apply(
		filepath.Dir(dumpPath),
		false,
		false,
		acl.GrantName(windows.GENERIC_READ, "NT SERVICE\\MSSQLSERVER"),
		acl.GrantName(windows.GENERIC_WRITE, "NT SERVICE\\MSSQLSERVER"),
	); err != nil {
		logger.Fatal(err)
	}

	ctx := context.Background()
	_, err := mssqlDB.ExecContext(ctx,
		"BACKUP DATABASE "+dbName+
			" TO DISK = '"+dumpPath+"'"+
			" WITH COMPRESSION;") /* `
	    BACKUP DATABASE `+dbName+`
	    TO DISK = '`+dumpPath+`'
	    WITH FORMAT, INIT, NAME = 'Full Backup of `+dbName+`';
	`, `
	    BACKUP DATABASE AdventureWorks2012
	    TO DISK = 'C:\Users\Administrator\Desktop\backups\AdventureWorks2012-2024-05-23-144157.bak'
	    WITH FORMAT, INIT, NAME = 'Full Backup of AdventureWorks2012';
	`, )*/
	if err != nil {
		logger.Error("Couldn't back up database: " + dbName + " - Error: " + err.Error())
		return "", "", err
	}
	return dumpPath, name, nil
}
