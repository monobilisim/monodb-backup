package clog

import (
	"monodb-backup/config"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/snowzach/rotatefilehook"
)

var params *config.LoggerParams = &config.Parameters.Log

type CustomLogger struct {
	*config.LoggerParams
	*logrus.Logger
}

var Logger CustomLogger

func InitializeLogger() {
	if params.MaxSize == 0 {
		params.MaxSize = 50
	}
	if params.MaxBackups == 0 {
		params.MaxBackups = 3
	}
	if params.MaxAge == 0 {
		params.MaxAge = 30
	}

	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		ForceColors:     true,
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
		CallerPrettyfier: func(f *runtime.Frame) (string, string) {
			slash := strings.LastIndex(f.File, "/")
			filename := f.File[slash+1:]
			return "", "[" + filename + ":" + strconv.Itoa(f.Line) + "]"
		},
	})
	logger.SetReportCaller(true)

	Logger = CustomLogger{
		LoggerParams: params,
		Logger:       logger,
	}

	var level logrus.Level

	switch params.Level {
	case "info":
		level = logrus.InfoLevel
	case "debug":
		level = logrus.DebugLevel
	case "warn":
		level = logrus.WarnLevel
	case "error":
		level = logrus.ErrorLevel
	case "fatal":
		level = logrus.FatalLevel
	default:
		level = logrus.InfoLevel
	}

	Logger.SetLevel(level)

	if params.File != "" {
		_, err := os.OpenFile(params.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			logrus.Error(err)
			return
		}

		rotateFileHook, err := rotatefilehook.NewRotateFileHook(rotatefilehook.RotateFileConfig{
			Filename:   params.File,
			MaxSize:    params.MaxSize,
			MaxBackups: params.MaxBackups,
			MaxAge:     params.MaxAge, //days
			Level:      level,
			Formatter: &logrus.JSONFormatter{
				TimestampFormat: time.RFC3339,
			},
		})
		if err != nil {
			logrus.Error(err)
			return
		}

		Logger.AddHook(rotateFileHook)
	}

	return
}
