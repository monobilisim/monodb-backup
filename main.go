package main

import (
	"pgsql-backup/backup"
	"pgsql-backup/config"
	"pgsql-backup/log"
)

func main()  {
	p := config.NewParams()
	l := log.NewLogger(p.Log)
	d := backup.NewDumper(p, l)
	d.Dump()
}
