package smtpclient

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

const (
	TLSModeAuto     = "auto"
	TLSModePlain    = "plain"
	TLSModeSTARTTLS = "starttls"
	TLSModeTLS      = "tls"
)

type SendRequest struct {
	Host     string
	Port     int
	TLSMode  string
	Username string
	Password string
	From     string
	To       []string
	Message  []byte
	Timeout  time.Duration
}

func Send(req SendRequest) error {
	req.Host = strings.TrimSpace(req.Host)
	req.TLSMode = normalizeTLSMode(req.TLSMode)
	if req.Host == "" {
		return fmt.Errorf("smtp host is required")
	}
	if req.Port <= 0 {
		return fmt.Errorf("smtp port is required")
	}
	if strings.TrimSpace(req.From) == "" {
		return fmt.Errorf("smtp from address is required")
	}
	if len(req.To) == 0 {
		return fmt.Errorf("smtp recipients are required")
	}
	if len(req.Message) == 0 {
		return fmt.Errorf("smtp message is required")
	}

	address := fmt.Sprintf("%s:%d", req.Host, req.Port)
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	tlsConfig := &tls.Config{ServerName: req.Host, MinVersion: tls.VersionTLS12}
	var (
		conn net.Conn
		err  error
	)
	dialer := &net.Dialer{Timeout: timeout}
	if req.TLSMode == TLSModeTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, tlsConfig)
	} else {
		conn, err = dialer.Dial("tcp", address)
	}
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, req.Host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer client.Close()

	if err := client.Hello("localhost"); err != nil {
		return err
	}
	if req.TLSMode == TLSModeSTARTTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("smtp server does not support STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return err
		}
	} else if req.TLSMode == TLSModeAuto {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return err
			}
		}
	}

	if err := authenticate(client, req); err != nil {
		return err
	}
	if err := client.Mail(strings.TrimSpace(req.From)); err != nil {
		return err
	}
	for _, recipient := range req.To {
		recipient = strings.TrimSpace(recipient)
		if recipient == "" {
			continue
		}
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(req.Message); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func normalizeTLSMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", TLSModeAuto:
		return TLSModeAuto
	case TLSModeSTARTTLS:
		return TLSModeSTARTTLS
	case TLSModeTLS:
		return TLSModeTLS
	case TLSModePlain:
		return TLSModePlain
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

func authenticate(client *smtp.Client, req SendRequest) error {
	username := strings.TrimSpace(req.Username)
	if username == "" {
		return nil
	}
	password := req.Password
	mechanisms := smtpAuthMechanisms(client)
	allowWithoutReportedTLS := req.TLSMode == TLSModeTLS || isLocalSMTPHost(req.Host)
	if mechanisms["LOGIN"] && !mechanisms["PLAIN"] {
		return client.Auth(newLoginAuth(username, password, allowWithoutReportedTLS))
	}
	plainErr := client.Auth(newPlainAuth(username, password, allowWithoutReportedTLS))
	if plainErr == nil {
		return nil
	}
	if mechanisms["LOGIN"] || shouldTryLoginAuth(plainErr) {
		loginErr := client.Auth(newLoginAuth(username, password, allowWithoutReportedTLS))
		if loginErr == nil {
			return nil
		}
		return fmt.Errorf("%w; LOGIN auth fallback failed: %v", plainErr, loginErr)
	}
	return plainErr
}

func smtpAuthMechanisms(client *smtp.Client) map[string]bool {
	mechanisms := map[string]bool{}
	if client == nil {
		return mechanisms
	}
	_, authLine := client.Extension("AUTH")
	for _, item := range strings.Fields(strings.ToUpper(authLine)) {
		mechanisms[item] = true
	}
	return mechanisms
}

func shouldTryLoginAuth(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "504") ||
		strings.Contains(message, "unrecognized authentication type") ||
		strings.Contains(message, "unsupported authentication") ||
		strings.Contains(message, "auth plain")
}

type plainAuth struct {
	username                string
	password                string
	allowWithoutReportedTLS bool
}

func newPlainAuth(username, password string, allowWithoutReportedTLS bool) smtp.Auth {
	return &plainAuth{username: username, password: password, allowWithoutReportedTLS: allowWithoutReportedTLS}
}

func (a *plainAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !a.allowAuth(server) {
		return "", nil, errors.New("unencrypted connection")
	}
	return "PLAIN", []byte("\x00" + a.username + "\x00" + a.password), nil
}

func (a *plainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("unexpected server challenge")
	}
	return nil, nil
}

func (a *plainAuth) allowAuth(server *smtp.ServerInfo) bool {
	return a.allowWithoutReportedTLS || (server != nil && (server.TLS || isLocalSMTPHost(server.Name)))
}

type loginAuth struct {
	username                string
	password                string
	allowWithoutReportedTLS bool
	step                    int
}

func newLoginAuth(username, password string, allowWithoutReportedTLS bool) smtp.Auth {
	return &loginAuth{username: username, password: password, allowWithoutReportedTLS: allowWithoutReportedTLS}
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !a.allowAuth(server) {
		return "", nil, errors.New("unencrypted connection")
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	prompt := strings.ToLower(string(fromServer))
	if strings.Contains(prompt, "username") {
		a.step = 1
		return []byte(a.username), nil
	}
	if strings.Contains(prompt, "password") {
		a.step = 2
		return []byte(a.password), nil
	}
	if a.step == 0 {
		a.step = 1
		return []byte(a.username), nil
	}
	if a.step == 1 {
		a.step = 2
		return []byte(a.password), nil
	}
	return nil, errors.New("unexpected LOGIN auth challenge")
}

func (a *loginAuth) allowAuth(server *smtp.ServerInfo) bool {
	return a.allowWithoutReportedTLS || (server != nil && (server.TLS || isLocalSMTPHost(server.Name)))
}

func isLocalSMTPHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
