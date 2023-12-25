package config

import (
	"flag"
	"os"
	"pgsql-backup/log"

	"github.com/spf13/viper"
)

type Params struct {
	BackupDestination string
	Databases         []string
	Format            string // 7z, gz, default gz(pg_dump -Fc option - no further compression)
	RemoveLocal       bool
	ArchivePass       string
	Notify            struct {
		Email struct {
			Enabled     bool
			OnlyOnError bool
			Info        EmailConfig
			Error       EmailConfig
		}
		Webhook Webhook
	}
	S3 struct {
		Enabled   bool
		Region    string
		Bucket    string
		Path      string
		AccessKey string
		SecretKey string
	}
	Minio struct {
		Enabled            bool
		Endpoint           string
		Bucket             string
		Path               string
		AccessKey          string
		SecretKey          string
		Secure             bool
		InsecureSkipVerify bool
	}
	Log  *log.Params
	Fqdn string
}

type Webhook struct {
	Enabled     bool
	OnlyOnError bool
	Info        []string
	Error       []string
}

type EmailConfig struct {
	SmtpHost string
	SmtpPort string
	From     string
	Username string
	Password string
	To       string
}

func NewParams() (p *Params) {
	filePath := flag.String("config", "/etc/pgsql-backup.yml", "Path of the configuration file in YAML format")
	flag.Parse()

	if _, err := os.Stat(*filePath); os.IsNotExist(err) {
		log.Fatal("Configuration file: %s does not exist, %v\n", *filePath, err)
	}

	viper.SetConfigFile(*filePath)
	viper.SetConfigType("yaml")

	if err := viper.ReadInConfig(); err != nil {
		log.Fatal("Error reading config file, %s\n", err)
	}

	err := viper.Unmarshal(&p)
	if err != nil {
		log.Fatal("Unable to decode into struct, %v\n", err)
	}

	p.Fqdn, _ = os.Hostname()

	return
}
