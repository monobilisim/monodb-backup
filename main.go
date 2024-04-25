package main

import (
	"flag"
	"fmt"
	"github.com/robfig/cron"
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

	var logger *clog.CustomLogger = &clog.Logger

	logger.Info("monodb-backup started.")

	if config.Parameters.RunEveryCron != "" {
		c := cron.New()
		c.AddFunc(config.Parameters.RunEveryCron, initBackup)
		c.Start()
		select {}
	} else {
		// backwards compatibility
		initBackup()
	}
}

func initBackup() {
	if config.Parameters.Minio.Enabled {
		backup.InitializeMinioClient()
	}

	if config.Parameters.S3.Enabled {
		backup.InitializeS3Session()
	}

	backup.Backup()
}
