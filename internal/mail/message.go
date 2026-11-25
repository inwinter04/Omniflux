package mail

import (
	"mime"
	"strings"
	"time"
)

func buildMessage(settings Settings, message Message) []byte {
	from := settings.FromAddress
	if settings.FromName != "" {
		from = mime.QEncoding.Encode("phrase", settings.FromName) + " <" + settings.FromAddress + ">"
	}
	headers := []string{
		"From: " + from,
		"To: " + message.To,
		"Subject: " + mime.QEncoding.Encode("subject", message.Subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		`Content-Type: multipart/alternative; boundary="omniflux-email"`,
	}
	var body strings.Builder
	for _, header := range headers {
		body.WriteString(header)
		body.WriteString("\r\n")
	}
	body.WriteString("\r\n--omniflux-email\r\n")
	body.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	body.WriteString(strings.ReplaceAll(message.Text, "\n", "\r\n"))
	body.WriteString("\r\n\r\n--omniflux-email\r\n")
	body.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
	body.WriteString(message.HTML)
	body.WriteString("\r\n\r\n--omniflux-email--\r\n")
	return []byte(body.String())
}
