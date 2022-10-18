package config

import (
	"flag"
	"github.com/spf13/viper"
	log2 "log"
	"os"
	"pgsql-backup/log"
)

type Params struct {
	BackupDestination string
	Databases         []string
	RemoveLocal       bool
	Notify            struct {
		OnlyOnError bool
		Email       struct {
			Enabled  bool
			SmtpHost string
			SmtpPort string
			From     string
			Password string
			To       string
		}
		Mattermost struct {
			Enabled   bool
			Url       string
			ChannelId string
			ApiToken  string
		}
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

func NewParams() (p *Params) {
	filePath := flag.String("config", "/etc/pgsql-backup.yml", "Path of the configuration file in YAML format")
	flag.Parse()

	if _, err := os.Stat(*filePath); os.IsNotExist(err) {
		log2.Fatalf("Configuration file: %s does not exist, %v\n", *filePath, err)
	}

	viper.SetConfigFile(*filePath)
	viper.SetConfigType("yaml")

	if err := viper.ReadInConfig(); err != nil {
		log2.Fatalf("Error reading config file, %s", err)
	}

	err := viper.Unmarshal(&p)
	if err != nil {
		log2.Fatalf("Unable to decode into struct, %v\n", err)
	}

	p.Fqdn, _ = os.Hostname()

	return
}
