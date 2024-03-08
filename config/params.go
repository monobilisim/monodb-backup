package config

import (
	"log"
	"os"

	"github.com/spf13/viper"
)

type Params struct {
	BackupDestination string
	Database          string
	Databases         []string
	Exclude           []string
	Format            string // 7z, gz, default gz(pg_dump -Fc option - no further compression)
	RemoveLocal       bool
	ArchivePass       string
	Rotation          Rotation
	Remote            Remote
	Cluster           Cluster
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
	SFTP struct {
		Enabled bool
		User    string
		Target  string
		Port    string
	}
	Log  LoggerParams
	Fqdn string
}

type Cluster struct {
	IsCluster bool
	Remote    Remote
}

type Rotation struct {
	Enabled bool
	Period  string // week or month
	Suffix  string // day db-monday.sql.7z - hour db-monday-15.sql.7z - minute db-monday-15-24.sql.7z
}

type Remote struct {
	IsRemote bool
	Host     string
	Port     string
	User     string
	Password string
}

type Webhook struct {
	Enabled          bool
	OnlyOnError      bool
	ServerIdentifier string
	Info             []string
	Error            []string
}

type EmailConfig struct {
	SmtpHost string
	SmtpPort string
	From     string
	Username string
	Password string
	To       string
}

type LoggerParams struct {
	Level      string
	File       string
	MaxSize    int
	MaxBackups int
	MaxAge     int
}

var Parameters Params

func ParseParams(configFile *string) {
	filePath := configFile

	if _, err := os.Stat(*filePath); os.IsNotExist(err) {
		log.Fatalf("Configuration file: %s does not exist, %v\n", *filePath, err)
		return
	}

	viper.SetConfigFile(*filePath)
	viper.SetConfigType("yaml")

	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("Error reading config file, %v\n", err)
		return
	}

	err := viper.Unmarshal(&Parameters)
	if err != nil {
		log.Fatalf("Unable to decode config into struct, %v\n", err)
		return
	}

	Parameters.Fqdn, _ = os.Hostname()

	return
}
