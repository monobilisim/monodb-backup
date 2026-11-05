package main

import (
	"flag"
	"fmt"
	"runtime"
	"time"

	"monodb-backup/backup"
	"monodb-backup/clog"
	"monodb-backup/config"
	"monodb-backup/notify"

	"github.com/robfig/cron"
)

var Version = "dev"

func main() {
	var configPath string
	if runtime.GOOS == "windows" {
		configPath = "C:\\ProgramData\\monodb-backup\\monodb-backup.yml"
	} else {
		configPath = "/etc/monodb-backup.yml"
	}
	printVersion := flag.Bool("version", false, "Prints version")
	filePath := flag.String("config", configPath, "Path of the configuration file in YAML format")
	flag.Parse()
	if *printVersion {
		fmt.Println("monodb-backup " + Version)
		return
	}

	config.ParseParams(filePath)
	clog.InitializeLogger()

	logger := &clog.Logger

	if config.Parameters.Database == "mssql" {
		backup.InitializeMSSQL()
	}

	logger.Info("monodb-backup started.")

	if config.Parameters.Notify.UptimeAlarm {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		go func() {
			for range ticker.C {
				backup.SendHourlyUptimeStatus()
			}
		}()

	}

	if config.Parameters.RunEveryCron != "" {
		c := cron.New()
		if err := c.AddFunc(config.Parameters.RunEveryCron, initBackup); err != nil {
			logger.Fatal("Couldn't schedule backup job: " + err.Error())
		}
		c.Start()
		select {}
	} else {
		// backwards compatibility
		initBackup()
	}
	logger.Info("monodb-backup job finished.")
	// notify.SendAlarm("monodb-backup job finished.", false)
}

func initBackup() {
	if config.Parameters.BackupType.Type == "minio" || config.Parameters.BackupType.Type == "s3" {
		backup.InitializeS3Session()
	}
	backup.Backup()
	if len(notify.FailedDBList) > 0 && config.Parameters.Retry {
		backup.Retrying = true
		backup.Backup()
	}
	notify.SendSingleEntityAlarm()
}
