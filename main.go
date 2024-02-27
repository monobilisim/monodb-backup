package main

import (
	"flag"
	"fmt"
	"monodb-backup/backup"
	"monodb-backup/config"
	"monodb-backup/log"
	"monodb-backup/notify"
)

var Version = "dev"

func main() {
	printVersion := flag.Bool("version", false, "Prints version")
	flag.Parse()
	if *printVersion {
		fmt.Println("monodb-backup " + Version)
		return
	}

	p := config.NewParams()
	l := log.NewLogger(p.Log)
	d := backup.NewDumper(p, l)
	notify.InitializeWebhook(&p.Notify.Webhook, l, p.Database)
	d.Dump()
}
