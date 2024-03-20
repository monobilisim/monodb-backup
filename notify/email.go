package notify

import (
	"crypto/tls"
	"monodb-backup/config"
	"strconv"
	"strings"

	"gopkg.in/gomail.v2"
)

var emailStruct = &config.Parameters.Notify.Email

func Email(subject string, message string, isError bool) error {
	if !emailStruct.Enabled {
		return nil
	}

	if emailStruct.OnlyOnError && !isError {
		return nil
	}

	var smtpHost, smtpPort, from, username, password string
	var to []string

	if isError {
		smtpHost = emailStruct.Error.SmtpHost
		smtpPort = emailStruct.Error.SmtpPort
		from = emailStruct.Error.From
		username = emailStruct.Error.Username
		password = emailStruct.Error.Password
		to = strings.Split(emailStruct.Error.To, ",")
	} else {
		smtpHost = emailStruct.Info.SmtpHost
		smtpPort = emailStruct.Info.SmtpPort
		from = emailStruct.Info.From
		username = emailStruct.Info.Username
		password = emailStruct.Info.Password
		to = strings.Split(emailStruct.Info.To, ",")
	}
	port, _ := strconv.Atoi(smtpPort)

	d := gomail.NewDialer(smtpHost, port, username, password)
	if emailStruct.InsecureSkipVerify {
		d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}

	m := gomail.NewMessage()
	m.SetHeader("From", from)
	m.SetHeader("To", to...)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", message)

	return d.DialAndSend(m)

	// var auth smtp.Auth
	// if username != "" || password != "" {
	// 	auth = smtp.CRAMMD5Auth(username, password)
	// } else {
	// 	auth = smtp.PlainAuth("", username, password, smtpHost)
	// }
	// msg := []byte("From: " + from + "\r\n" +
	// 	"To: " + to + "\r\n" +
	// 	"Subject: [" + config.Parameters.Fqdn + "] " + subject + "\r\n\r\n" +
	// 	message + "\r\n")

	// return smtp.SendMail(smtpHost+":"+smtpPort, auth, from, []string{to}, msg)
}
