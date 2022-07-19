package main

import (
	"pgsql-backup/backup"
	"pgsql-backup/config"
)

func main()  {
	p := config.NewParams()
	d := backup.NewDumper(p)
	d.Dump()
}
