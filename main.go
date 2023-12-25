package main

import (
	"pgsql-backup/backup"
	"pgsql-backup/config"
	"pgsql-backup/log"
	"pgsql-backup/notify"
)

func main() {
	p := config.NewParams()
	l := log.NewLogger(p.Log)
	d := backup.NewDumper(p, l)
	notify.InitializeWebhook(&p.Notify.Webhook, l)
	d.Dump()
}
