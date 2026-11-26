package mailer

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"net/smtp"
	"strings"
	"time"
)

type Config struct {
	SMTPHost string
	SMTPPort string
	From     string
	To       string
	Username string // опціонально (для зовнішніх SMTP)
	Password string // опціонально (для зовнішніх SMTP)
}

type AuditInfo struct {
	CommonName   string
	SerialNumber string
	NotBefore    time.Time
	NotAfter     time.Time
	RemoteAddr   string
}

func wrapBase64(b64 string) string {
	var out strings.Builder
	for i := 0; i < len(b64); i += 76 {
		end := i + 76
		if end > len(b64) {
			end = len(b64)
		}
		out.WriteString(b64[i:end])
		out.WriteString("\r\n")
	}
	return out.String()
}

func SendIncidentAlert(cfg Config, incident string, timestamp time.Time, audit AuditInfo) error {
	boundary := fmt.Sprintf("==ZeroTrustAlertBoundary_%d==", time.Now().UnixNano())
	tsStr := timestamp.Format(time.RFC3339)

	logContent := fmt.Sprintf(
		"=== ZERO TRUST INCIDENT AUDIT LOG ===\n"+
			"Timestamp:         %s\n"+
			"Remote Address:    %s\n"+
			"Client CN:         %s\n"+
			"Certificate Serial:%s\n"+
			"Cert Valid From:   %s\n"+
			"Cert Valid Until:  %s\n"+
			"Incident Payload:  %s\n",
		tsStr,
		audit.RemoteAddr,
		audit.CommonName,
		audit.SerialNumber,
		audit.NotBefore.Format(time.RFC3339),
		audit.NotAfter.Format(time.RFC3339),
		incident,
	)

	encodedAttachment := wrapBase64(base64.StdEncoding.EncodeToString([]byte(logContent)))

	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html lang="uk">
<head><meta charset="UTF-8"></head>
<body style="font-family: Arial, sans-serif; background-color: #f4f6f8; padding: 20px;">
  <div style="max-width: 640px; margin: 0 auto; background: #ffffff; border: 1px solid #e0e0e0; border-radius: 6px; overflow: hidden;">
    <div style="background-color: #c62828; color: #ffffff; padding: 14px 20px; font-weight: bold; font-size: 16px;">
      🚨 КРИТИЧНИЙ ІНЦИДЕНТ БЕЗПЕКИ / АВАРІЯ (ZERO TRUST GATEWAY)
    </div>
    <div style="padding: 20px;">
      <p style="margin-top: 0;">Зафіксовано термінове повідомлення від верифікованого клієнта <strong>[CN=%s]</strong>.</p>
      <table style="width: 100%%; border-collapse: collapse; margin-top: 12px; font-size: 14px;">
        <tr>
          <td style="padding: 8px; border: 1px solid #ddd; background: #fafafa; width: 35%%;"><strong>Повідомлення</strong></td>
          <td style="padding: 8px; border: 1px solid #ddd; color: #b71c1c; font-weight: bold;">%s</td>
        </tr>
        <tr>
          <td style="padding: 8px; border: 1px solid #ddd; background: #fafafa;"><strong>Час реєстрації</strong></td>
          <td style="padding: 8px; border: 1px solid #ddd;">%s</td>
        </tr>
        <tr>
          <td style="padding: 8px; border: 1px solid #ddd; background: #fafafa;"><strong>Джерело (IP:Порт)</strong></td>
          <td style="padding: 8px; border: 1px solid #ddd;">%s</td>
        </tr>
        <tr>
          <td style="padding: 8px; border: 1px solid #ddd; background: #fafafa;"><strong>Subject CN</strong></td>
          <td style="padding: 8px; border: 1px solid #ddd;">%s</td>
        </tr>
        <tr>
          <td style="padding: 8px; border: 1px solid #ddd; background: #fafafa;"><strong>Серійний номер</strong></td>
          <td style="padding: 8px; border: 1px solid #ddd;">%s</td>
        </tr>
        <tr>
          <td style="padding: 8px; border: 1px solid #ddd; background: #fafafa;"><strong>Термін дії сертифіката</strong></td>
          <td style="padding: 8px; border: 1px solid #ddd;">%s — %s</td>
        </tr>
      </table>
      <p style="margin-bottom: 0; margin-top: 16px; font-size: 13px; color: #555;">
        Детальний звіт аудиту прикріплено у файлі <code>incident_report.log</code>.
      </p>
    </div>
  </div>
</body>
</html>`,
		html.EscapeString(audit.CommonName),
		html.EscapeString(incident),
		html.EscapeString(tsStr),
		html.EscapeString(audit.RemoteAddr),
		html.EscapeString(audit.CommonName),
		html.EscapeString(audit.SerialNumber),
		html.EscapeString(audit.NotBefore.Format("2006-01-02")),
		html.EscapeString(audit.NotAfter.Format("2006-01-02")),
	)

	var msg bytes.Buffer
	msg.WriteString(fmt.Sprintf("From: %s\r\n", cfg.From))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", cfg.To))
	msg.WriteString(fmt.Sprintf("Subject: [CRITICAL ALERT] %s\r\n", incident))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary))

	// Секція 1: HTML
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	msg.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	msg.WriteString(htmlBody)
	msg.WriteString("\r\n\r\n")

	// Секція 2: Вкладення incident_report.log у Base64
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/plain; charset=\"UTF-8\"; name=\"incident_report.log\"\r\n")
	msg.WriteString("Content-Transfer-Encoding: base64\r\n")
	msg.WriteString("Content-Disposition: attachment; filename=\"incident_report.log\"\r\n\r\n")
	msg.WriteString(encodedAttachment)
	msg.WriteString("\r\n")

	// Завершальний boundary
	msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	addr := fmt.Sprintf("%s:%s", cfg.SMTPHost, cfg.SMTPPort)
	var auth smtp.Auth
	if cfg.Username != "" && cfg.Password != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.SMTPHost)
	}

	return smtp.SendMail(addr, auth, cfg.From, []string{cfg.To}, msg.Bytes())
}
