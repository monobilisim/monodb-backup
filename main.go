package main

import (
	"monodb-backup/backup"
	"monodb-backup/config"
	"monodb-backup/log"
	"monodb-backup/notify"
)

func main() {
	p := config.NewParams()
	l := log.NewLogger(p.Log)
	d := backup.NewDumper(p, l)
	notify.InitializeWebhook(&p.Notify.Webhook, l)
	d.Dump()
}
