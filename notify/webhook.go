package notify

import (
	"bytes"
	"encoding/json"
	"monodb-backup/clog"
	"monodb-backup/config"
	"net/http"
)

var webhookStruct *config.Webhook = &config.Parameters.Notify.Webhook
var logger *clog.CustomLogger = &clog.Logger

func SendAlarm(message string, isError bool) {
	var db string = func() string {
		switch config.Parameters.Database {
		case "postgresql":
			return "PostgreSQL"
		case "mysql":
			return "MySQL"
		default:
			return "PostgreSQL"
		}
	}()
	var subject string
	if isError {
		subject = "Error"
	} else {
		subject = "Success"
	}
	err := Email("Database Backup "+subject, message, isError)
	if err != nil {
		logger.Error("Couldn't send mail. Error: " + err.Error())
	}
	if !webhookStruct.Enabled || (webhookStruct.OnlyOnError && !isError) {
		return
	}

	identifier := "[ " + db + " - " + webhookStruct.ServerIdentifier + " ] "

	if isError {
		message = identifier + "[:red_circle:] " + message
		for _, hook := range webhookStruct.Error {
			sendAlarm(hook, message)
		}
	} else {
		message = identifier + "[:check:] " + message
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
}
