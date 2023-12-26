package notify

import (
	"bytes"
	"encoding/json"
	"monodb-backup/config"
	"net/http"
)

type Logger interface {
	Debug(args ...interface{})
	Info(args ...interface{})
	Warn(args ...interface{})
	Error(args ...interface{})
	Panic(args ...interface{})
	Fatal(args ...interface{})
	DebugWithFields(fields map[string]interface{}, args ...interface{})
	InfoWithFields(fields map[string]interface{}, args ...interface{})
	WarnWithFields(fields map[string]interface{}, args ...interface{})
	ErrorWithFields(fields map[string]interface{}, args ...interface{})
	PanicWithFields(fields map[string]interface{}, args ...interface{})
	FatalWithFields(fields map[string]interface{}, args ...interface{})
}

var webhookStruct *config.Webhook
var logger Logger

func InitializeWebhook(params *config.Webhook, loggerInfo Logger) {
	webhookStruct = params
	logger = loggerInfo
}

func SendAlarm(message string, isError bool) {
	if !webhookStruct.Enabled || (webhookStruct.OnlyOnError && !isError) {
		return
	}

	if isError {
		for _, hook := range webhookStruct.Error {
			sendAlarm(hook, message)
		}
	} else {
		for _, hook := range webhookStruct.Info {
			sendAlarm(hook, message)
		}
	}
}

func sendAlarm(webhook, message string) {
	values := map[string]string{"text": message}
	jsonData, err := json.Marshal(values)

	if err != nil {
		logger.Error("Couldn't parse to json\n" + err.Error())
		return
	}

	resp, err := http.Post(webhook, "application/json",
		bytes.NewBuffer(jsonData))

	if err != nil {
		logger.Error("Couldn't send message to " + "webhook" + "\n" + err.Error())
		return
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	var res map[string]interface{}

	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		logger.Error("Couldn't parse response from json\n" + err.Error())
		return
	}

	return
}
