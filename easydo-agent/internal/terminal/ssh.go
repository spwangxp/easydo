package terminal

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	sshpkg "golang.org/x/crypto/ssh"
)

type SSHDialer struct{}

func NewSSHDialer() *SSHDialer {
	return &SSHDialer{}
}

func (d *SSHDialer) Open(ctx context.Context, req OpenRequest) (Session, error) {
	username, authMethods, err := buildSSHAuth(req.Credential)
	if err != nil {
		return nil, err
	}

	clientConfig := &sshpkg.ClientConfig{
		User:            username,
		Auth:            authMethods,
		HostKeyCallback: sshpkg.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	address := normalizeSSHAddress(req.Endpoint)
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("dial ssh endpoint: %w", err)
	}

	sshConn, chans, reqs, err := sshpkg.NewClientConn(conn, address, clientConfig)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open ssh connection: %w", err)
	}
	client := sshpkg.NewClient(sshConn, chans, reqs)

	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("create ssh session: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, fmt.Errorf("open stdout pipe: %w", err)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, fmt.Errorf("open stderr pipe: %w", err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, fmt.Errorf("open stdin pipe: %w", err)
	}

	if err := session.RequestPty("xterm-256color", req.Rows, req.Cols, sshpkg.TerminalModes{
		sshpkg.ECHO:          1,
		sshpkg.TTY_OP_ISPEED: 14400,
		sshpkg.TTY_OP_OSPEED: 14400,
	}); err != nil {
		_ = stdin.Close()
		_ = session.Close()
		_ = client.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}

	if err := session.Shell(); err != nil {
		_ = stdin.Close()
		_ = session.Close()
		_ = client.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}

	return &sshSession{client: client, session: session, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

type sshSession struct {
	client  *sshpkg.Client
	session *sshpkg.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	stderr  io.Reader
}

func (s *sshSession) Stdout() io.Reader { return s.stdout }

func (s *sshSession) Stderr() io.Reader { return s.stderr }

func (s *sshSession) Write(data []byte) (int, error) {
	return s.stdin.Write(data)
}

func (s *sshSession) Resize(cols, rows int) error {
	return s.session.WindowChange(rows, cols)
}

func (s *sshSession) Close() error {
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.session != nil {
		_ = s.session.Close()
	}
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *sshSession) Wait() error {
	return s.session.Wait()
}

func buildSSHAuth(credential Credential) (string, []sshpkg.AuthMethod, error) {
	username := strings.TrimSpace(stringValue(credential.Payload["username"]))
	if username == "" {
		return "", nil, fmt.Errorf("terminal credential requires username")
	}

	switch strings.ToUpper(strings.TrimSpace(credential.Type)) {
	case "PASSWORD":
		password := stringValue(credential.Payload["password"])
		if password == "" {
			return "", nil, fmt.Errorf("password credential requires password")
		}
		return username, []sshpkg.AuthMethod{sshpkg.Password(password)}, nil
	case "SSH_KEY":
		privateKey := stringValue(credential.Payload["private_key"])
		if privateKey == "" {
			return "", nil, fmt.Errorf("ssh key credential requires private_key")
		}
		passphrase := stringValue(credential.Payload["passphrase"])
		signer, err := parseSigner(privateKey, passphrase)
		if err != nil {
			return "", nil, err
		}
		return username, []sshpkg.AuthMethod{sshpkg.PublicKeys(signer)}, nil
	default:
		return "", nil, fmt.Errorf("unsupported terminal credential type: %s", credential.Type)
	}
}

func parseSigner(privateKey, passphrase string) (sshpkg.Signer, error) {
	if passphrase != "" {
		signer, err := sshpkg.ParsePrivateKeyWithPassphrase([]byte(privateKey), []byte(passphrase))
		if err != nil {
			return nil, fmt.Errorf("parse encrypted ssh private key: %w", err)
		}
		return signer, nil
	}
	signer, err := sshpkg.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		return nil, fmt.Errorf("parse ssh private key: %w", err)
	}
	return signer, nil
}

func normalizeSSHAddress(endpoint string) string {
	trimmed := strings.TrimSpace(endpoint)
	trimmed = strings.TrimPrefix(trimmed, "ssh://")
	if _, _, err := net.SplitHostPort(trimmed); err == nil {
		return trimmed
	}
	return net.JoinHostPort(trimmed, "22")
}

func stringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}
