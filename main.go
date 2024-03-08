package main

import (
	"flag"
	"fmt"
	"monodb-backup/backup"
	"monodb-backup/clog"
	"monodb-backup/config"
)

var Version = "dev"

func main() {
	printVersion := flag.Bool("version", false, "Prints version")
	filePath := flag.String("config", "/etc/monodb-backup.yml", "Path of the configuration file in YAML format")
	flag.Parse()
	if *printVersion {
		fmt.Println("monodb-backup " + Version)
		return
	}

	config.ParseParams(filePath)
	clog.InitializeLogger()
	if config.Parameters.Minio.Enabled {
		backup.InitializeMinioClient()
	}
	if config.Parameters.S3.Enabled {
		backup.InitializeS3Session()
	}
	backup.Backup()
}
