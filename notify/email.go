package notify

import (
	"net/smtp"
	"pgsql-backup/config"
)

func Email(params *config.Params, subject string, message string) {
	if !params.Notify.Email.Enabled {
		return
	}

	from := params.Notify.Email.From
	password := params.Notify.Email.Password
	to := []string{
		params.Notify.Email.To,
	}

	smtpHost := params.Notify.Email.SmtpHost
	smtpPort := params.Notify.Email.SmtpPort

	auth := smtp.PlainAuth("", from, password, smtpHost)

	msg := []byte("From: " + from + "\r\n" +
		"To: " + params.Notify.Email.To + "\r\n" +
		"Subject: [" + params.Fqdn + "] " + subject + "\r\n\r\n" +
		message + "\r\n")

	_ = smtp.SendMail(smtpHost+":"+smtpPort, auth, from, to, msg)
}
