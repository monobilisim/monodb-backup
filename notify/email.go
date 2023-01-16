package notify

import (
	"net/smtp"
	"pgsql-backup/config"
)

func Email(params *config.Params, subject string, message string, isError bool) {
	if !params.Notify.Email.Enabled {
		return
	}

	var smtpHost, smtpPort, from, password, to string

	if isError {
		smtpHost = params.Notify.Email.Error.SmtpHost
		smtpPort = params.Notify.Email.Error.SmtpPort
		from = params.Notify.Email.Error.From
		username = params.Notify.Email.Error.Username
		password = params.Notify.Email.Error.Password
		to = params.Notify.Email.Error.To
	} else {
		smtpHost = params.Notify.Email.Info.SmtpHost
		smtpPort = params.Notify.Email.Info.SmtpPort
		from = params.Notify.Email.Info.From
		username = params.Notify.Email.Info.Username
		password = params.Notify.Email.Info.Password
		to = params.Notify.Email.Info.To
	}

	auth := smtp.PlainAuth("", username, password, smtpHost)

	msg := []byte("From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: [" + params.Fqdn + "] " + subject + "\r\n\r\n" +
		message + "\r\n")

	_ = smtp.SendMail(smtpHost+":"+smtpPort, auth, from, []string{to}, msg)
}
