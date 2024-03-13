package notify

import (
	"monodb-backup/config"
	"net/smtp"
)

var emailStruct = &config.Parameters.Notify.Email

func Email(subject string, message string, isError bool) error {
	if !emailStruct.Enabled {
		return nil
	}

	if emailStruct.OnlyOnError && !isError {
		return nil
	}

	var smtpHost, smtpPort, from, username, password, to string

	if isError {
		smtpHost = emailStruct.Error.SmtpHost
		smtpPort = emailStruct.Error.SmtpPort
		from = emailStruct.Error.From
		username = emailStruct.Error.Username
		password = emailStruct.Error.Password
		to = emailStruct.Error.To
	} else {
		smtpHost = emailStruct.Info.SmtpHost
		smtpPort = emailStruct.Info.SmtpPort
		from = emailStruct.Info.From
		username = emailStruct.Info.Username
		password = emailStruct.Info.Password
		to = emailStruct.Info.To
	}

	auth := smtp.CRAMMD5Auth(username, password)

	msg := []byte("From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: [" + config.Parameters.Fqdn + "] " + subject + "\r\n\r\n" +
		message + "\r\n")

	return smtp.SendMail(smtpHost+":"+smtpPort, auth, from, []string{to}, msg)
}
