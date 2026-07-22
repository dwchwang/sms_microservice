package email

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// ErrAmbiguousDelivery means the SMTP exchange failed after the message body
// was handed over, so whether the server accepted it is unknown. It maps to
// the delivery_unknown state and is never retried blindly.
var ErrAmbiguousDelivery = errors.New("delivery outcome unknown")

// ErrRecipientNotAllowed rejects a recipient outside the allowed domains,
// which is what stops the service being used as a mail relay.
var ErrRecipientNotAllowed = errors.New("recipient domain is not allowed")

// Attachment is a file attached to a report email.
type Attachment struct {
	Filename    string
	ContentType string
	Content     []byte
}

// Message is one outgoing report email.
type Message struct {
	To      string
	Subject string
	HTML    string
	// Attachment is optional; when nil the message stays single-part.
	Attachment *Attachment
}

// Sender delivers report emails.
type Sender interface {
	// Send returns the Message-ID it stamped on the message, so a delivery
	// whose outcome is unknown can still be traced in the Sent folder.
	Send(msg Message) (messageID string, err error)
}

// GmailSender sends through Gmail SMTP with STARTTLS.
type GmailSender struct {
	host     string
	port     int
	username string
	password string
	from     string
	// fromAddr is `from` without any display name.
	fromAddr       string
	allowedDomains []string
	dial           func(addr string) (smtpClient, error)
}

// smtpClient is the part of smtp.Client used here, kept small so the send
// sequence can be tested without a real server.
type smtpClient interface {
	StartTLS(*tls.Config) error
	Auth(smtp.Auth) error
	Mail(string) error
	Rcpt(string) error
	Data() (writeCloser, error)
	Quit() error
	Close() error
}

type writeCloser interface {
	Write([]byte) (int, error)
	Close() error
}

// NewGmailSender creates a GmailSender. allowedDomains is comma-separated;
// empty allows any recipient. from may carry a display name.
func NewGmailSender(host string, port int, username, password, from, allowedDomains string) *GmailSender {
	var domains []string
	for _, d := range strings.Split(allowedDomains, ",") {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			domains = append(domains, d)
		}
	}
	return &GmailSender{
		host:           host,
		port:           port,
		username:       username,
		password:       password,
		from:           from,
		fromAddr:       bareAddress(from),
		allowedDomains: domains,
		dial:           dialSMTP,
	}
}

// bareAddress strips any display name: "VCS-SMS <a@b.com>" becomes "a@b.com".
// MAIL FROM only accepts the bare address, and the Message-ID domain is derived
// from it.
func bareAddress(from string) string {
	if addr, err := mail.ParseAddress(from); err == nil {
		return addr.Address
	}
	return strings.TrimSpace(from)
}

// AllowRecipient reports whether the address may be mailed.
func (s *GmailSender) AllowRecipient(to string) bool {
	if len(s.allowedDomains) == 0 {
		return true
	}
	at := strings.LastIndex(to, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(to[at+1:])
	for _, d := range s.allowedDomains {
		if domain == d {
			return true
		}
	}
	return false
}

// Send performs the SMTP exchange. Failures before DATA are unambiguous
// rejections; a failure once the body is in flight is not, and is reported as
// ErrAmbiguousDelivery so the caller can record delivery_unknown.
func (s *GmailSender) Send(msg Message) (string, error) {
	if !s.AllowRecipient(msg.To) {
		return "", ErrRecipientNotAllowed
	}

	messageID := s.newMessageID()
	body := s.compose(msg, messageID)

	client, err := s.dial(fmt.Sprintf("%s:%d", s.host, s.port))
	if err != nil {
		return "", fmt.Errorf("failed to connect to SMTP: %w", err)
	}
	defer client.Close()

	if err := client.StartTLS(&tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}); err != nil {
		return "", fmt.Errorf("STARTTLS failed: %w", err)
	}
	auth := smtp.PlainAuth("", s.username, s.password, s.host)
	if err := client.Auth(auth); err != nil {
		return "", fmt.Errorf("SMTP auth failed: %w", err)
	}
	if err := client.Mail(s.fromAddr); err != nil {
		return "", fmt.Errorf("MAIL FROM failed: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return "", fmt.Errorf("RCPT TO failed: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return "", fmt.Errorf("DATA failed: %w", err)
	}

	// Past this point the body is on the wire and the outcome is no longer
	// certain from our side.
	if _, err := w.Write([]byte(body)); err != nil {
		return messageID, fmt.Errorf("%w: writing body failed: %v", ErrAmbiguousDelivery, err)
	}
	// Close sends the terminating dot and reads the server's reply; losing the
	// connection here is exactly the ambiguous case.
	if err := w.Close(); err != nil {
		return messageID, fmt.Errorf("%w: %v", ErrAmbiguousDelivery, err)
	}

	// The server already accepted the message, so a failed QUIT does not
	// un-send it.
	_ = client.Quit()
	return messageID, nil
}

// compose builds an RFC 5322 message. The Message-ID is generated here rather
// than parsed from Gmail's reply: Gmail's 250 line carries its queue ID, not a
// Message-ID, and a sender-supplied one is what shows up in the Sent folder.
func (s *GmailSender) compose(msg Message, messageID string) string {
	var b strings.Builder
	b.WriteString("From: " + s.from + "\r\n")
	b.WriteString("To: " + msg.To + "\r\n")
	b.WriteString("Subject: " + encodeHeader(msg.Subject) + "\r\n")
	b.WriteString("Message-ID: " + messageID + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")

	if msg.Attachment == nil {
		b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		b.WriteString("\r\n")
		b.WriteString(msg.HTML)
		return b.String()
	}

	boundary := newBoundary()
	b.WriteString("Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n")
	b.WriteString("\r\n")

	// HTML body part.
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(msg.HTML)
	b.WriteString("\r\n")

	// Attachment part, base64 with the RFC 2045 line length.
	att := msg.Attachment
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: " + att.ContentType + "; name=\"" + att.Filename + "\"\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString("Content-Disposition: attachment; filename=\"" + att.Filename + "\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(wrapBase64(att.Content))
	b.WriteString("\r\n")

	b.WriteString("--" + boundary + "--\r\n")
	return b.String()
}

// newBoundary returns a random MIME multipart boundary.
func newBoundary() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return "vcs-boundary-" + hex.EncodeToString(buf)
}

// wrapBase64 encodes data and wraps it at 76 characters per line.
func wrapBase64(data []byte) string {
	const lineLen = 76
	enc := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	for len(enc) > lineLen {
		b.WriteString(enc[:lineLen])
		b.WriteString("\r\n")
		enc = enc[lineLen:]
	}
	b.WriteString(enc)
	return b.String()
}

func (s *GmailSender) newMessageID() string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	domain := s.host
	if at := strings.LastIndex(s.fromAddr, "@"); at >= 0 {
		domain = s.fromAddr[at+1:]
	}
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), hex.EncodeToString(buf), domain)
}

// encodeHeader base64-encodes a header so Vietnamese subjects survive transit.
func encodeHeader(s string) string {
	for _, r := range s {
		if r > 127 {
			return mimeEncode(s)
		}
	}
	return s
}
