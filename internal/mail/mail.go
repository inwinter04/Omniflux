package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	netmail "net/mail"
	"net/smtp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/inwinter04/omniflux/internal/vault"
)

// Sender 屏蔽 SMTP 细节；集成测试使用内存实现，业务代码只依赖消息语义。
type Sender interface {
	Send(ctx context.Context, message Message) error
	Config(ctx context.Context) (StoredSettings, error)
}

type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

type Settings struct {
	Enabled       bool
	SMTPHost      string
	SMTPPort      int
	SMTPUsername  string
	SMTPPassword  string
	FromAddress   string
	FromName      string
	Encryption    string
	PublicBaseURL string
}

type StoredSettings struct {
	Settings
	HasPassword bool
}

type Service struct {
	pool      *pgxpool.Pool
	masterKey []byte
}

var ErrNotConfigured = errors.New("email service is not configured")

func NewService(pool *pgxpool.Pool, masterKey []byte) *Service {
	return &Service{pool: pool, masterKey: masterKey}
}

func (s *Service) Config(ctx context.Context) (StoredSettings, error) {
	var stored StoredSettings
	var ciphertext []byte
	err := s.pool.QueryRow(ctx, `
		SELECT enabled, smtp_host, smtp_port, smtp_username, smtp_password_ciphertext,
		       from_address, from_name, encryption, public_base_url
		FROM email_settings WHERE singleton
	`).Scan(
		&stored.Enabled, &stored.SMTPHost, &stored.SMTPPort, &stored.SMTPUsername, &ciphertext,
		&stored.FromAddress, &stored.FromName, &stored.Encryption, &stored.PublicBaseURL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return stored, ErrNotConfigured
	}
	if err != nil {
		return stored, fmt.Errorf("query email settings: %w", err)
	}
	stored.HasPassword = len(ciphertext) > 0
	if stored.HasPassword {
		plaintext, err := vault.Decrypt(s.masterKey, ciphertext)
		if err != nil {
			return stored, fmt.Errorf("decrypt smtp password: %w", err)
		}
		stored.SMTPPassword = string(plaintext)
	}
	return stored, nil
}

func (s *Service) Send(ctx context.Context, message Message) error {
	settings, err := s.Config(ctx)
	if err != nil {
		return err
	}
	return SendSMTP(ctx, settings.Settings, message)
}

func SendSMTP(ctx context.Context, settings Settings, message Message) error {
	if !settings.Enabled {
		return ErrNotConfigured
	}
	if err := validateSendSettings(settings, message); err != nil {
		return err
	}
	fromAddress, _ := netmail.ParseAddress(settings.FromAddress)
	recipientAddress, _ := netmail.ParseAddress(message.To)
	settings.FromAddress = fromAddress.Address
	message.To = recipientAddress.Address

	address := net.JoinHostPort(settings.SMTPHost, fmt.Sprintf("%d", settings.SMTPPort))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connect smtp server: %w", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return fmt.Errorf("set smtp deadline: %w", err)
	}
	if settings.Encryption == "tls" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: settings.SMTPHost, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return fmt.Errorf("smtp tls handshake: %w", err)
		}
		conn = tlsConn
	}

	client, err := smtp.NewClient(conn, settings.SMTPHost)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("create smtp client: %w", err)
	}
	defer client.Close()

	if settings.Encryption == "starttls" {
		if err := client.StartTLS(&tls.Config{ServerName: settings.SMTPHost, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if settings.SMTPUsername != "" {
		if err := client.Auth(smtp.PlainAuth("", settings.SMTPUsername, settings.SMTPPassword, settings.SMTPHost)); err != nil {
			return fmt.Errorf("smtp authentication: %w", err)
		}
	}
	if err := client.Mail(settings.FromAddress); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(message.To); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open smtp data writer: %w", err)
	}
	if _, err := writer.Write(buildMessage(settings, message)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write smtp message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish smtp message: %w", err)
	}
	return client.Quit()
}

func validateSendSettings(settings Settings, message Message) error {
	if settings.SMTPHost == "" || settings.FromAddress == "" {
		return ErrNotConfigured
	}
	if settings.Encryption != "starttls" && settings.Encryption != "tls" && settings.Encryption != "none" {
		return errors.New("invalid smtp encryption")
	}
	if settings.SMTPUsername != "" && settings.Encryption == "none" {
		return errors.New("smtp authentication requires starttls or tls")
	}
	if _, err := netmail.ParseAddress(settings.FromAddress); err != nil {
		return errors.New("invalid smtp from address")
	}
	if _, err := netmail.ParseAddress(message.To); err != nil {
		return errors.New("invalid message recipient")
	}
	return nil
}
