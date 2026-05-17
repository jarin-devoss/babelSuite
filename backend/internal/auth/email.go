package auth

import (
	"fmt"
	"net/smtp"
	"strings"
)

func sendPasswordResetEmail(cfg SMTPConfig, toEmail, resetLink string) error {
	port := cfg.Port
	if port == 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	msg := fmt.Sprintf(
		"To: %s\r\nFrom: %s\r\nSubject: Reset your BabelSuite password\r\n\r\n"+
			"Click the link below to reset your password. It expires in 15 minutes.\r\n\r\n%s\r\n\r\n"+
			"If you did not request a password reset, you can ignore this email.",
		toEmail, cfg.From, resetLink,
	)

	var auth smtp.Auth
	if strings.TrimSpace(cfg.Username) != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}

	return smtp.SendMail(addr, auth, cfg.From, []string{toEmail}, []byte(msg))
}
