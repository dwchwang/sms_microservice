package email

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"net/smtp"
	"strings"
	"testing"
)

var errNetwork = errors.New("connection reset by peer")

// fakeClient fails at whichever step is named, so each branch of the send
// sequence can be exercised.
type fakeClient struct {
	failAt   string
	steps    []string
	body     strings.Builder
	quitCall bool
}

func (c *fakeClient) step(name string) error {
	c.steps = append(c.steps, name)
	if c.failAt == name {
		return errNetwork
	}
	return nil
}

func (c *fakeClient) StartTLS(*tls.Config) error { return c.step("starttls") }
func (c *fakeClient) Auth(smtp.Auth) error       { return c.step("auth") }
func (c *fakeClient) Mail(string) error          { return c.step("mail") }
func (c *fakeClient) Rcpt(string) error          { return c.step("rcpt") }
func (c *fakeClient) Close() error               { return nil }

func (c *fakeClient) Quit() error {
	c.quitCall = true
	return c.step("quit")
}

func (c *fakeClient) Data() (writeCloser, error) {
	if err := c.step("data"); err != nil {
		return nil, err
	}
	return &fakeWriter{client: c}, nil
}

type fakeWriter struct {
	client *fakeClient
}

func (w *fakeWriter) Write(p []byte) (int, error) {
	if err := w.client.step("write"); err != nil {
		return 0, err
	}
	w.client.body.Write(p)
	return len(p), nil
}

func (w *fakeWriter) Close() error { return w.client.step("close") }

func newTestSender(failAt, domains string) (*GmailSender, *fakeClient) {
	client := &fakeClient{failAt: failAt}
	s := NewGmailSender("smtp.gmail.com", 587, "u@gmail.com", "pw", "u@gmail.com", domains)
	s.dial = func(string) (smtpClient, error) { return client, nil }
	return s, client
}

func testMessage() Message {
	return Message{To: "ops@example.com", Subject: "Báo cáo VCS-SMS", HTML: "<p>hi</p>"}
}

func TestSend_Success(t *testing.T) {
	s, client := newTestSender("", "")

	id, err := s.Send(testMessage())
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if id == "" || !strings.HasPrefix(id, "<") {
		t.Errorf("Message-ID = %q, want an RFC 5322 id", id)
	}
	want := "starttls,auth,mail,rcpt,data,write,close,quit"
	if got := strings.Join(client.steps, ","); got != want {
		t.Errorf("steps = %q, want %q", got, want)
	}
	if !strings.Contains(client.body.String(), "Message-ID: "+id) {
		t.Error("the Message-ID was not stamped on the message")
	}
}

// Anything that fails before the body is on the wire is a clean rejection:
// the mail definitely did not go out.
func TestSend_FailureBeforeDataIsUnambiguous(t *testing.T) {
	for _, step := range []string{"starttls", "auth", "mail", "rcpt", "data"} {
		s, _ := newTestSender(step, "")

		_, err := s.Send(testMessage())

		if err == nil {
			t.Errorf("%s: expected an error", step)
			continue
		}
		if errors.Is(err, ErrAmbiguousDelivery) {
			t.Errorf("%s: failed before DATA but was reported as ambiguous", step)
		}
	}
}

// Losing the connection while the body is in flight is exactly the case
// delivery_unknown exists for: it may or may not have been accepted.
func TestSend_FailureAfterDataIsAmbiguous(t *testing.T) {
	for _, step := range []string{"write", "close"} {
		s, _ := newTestSender(step, "")

		id, err := s.Send(testMessage())

		if !errors.Is(err, ErrAmbiguousDelivery) {
			t.Errorf("%s: err = %v, want ErrAmbiguousDelivery", step, err)
		}
		// The ID still comes back: it is how an operator checks the Sent folder.
		if id == "" {
			t.Errorf("%s: expected the Message-ID to be returned anyway", step)
		}
	}
}

// The server accepted the message before QUIT, so a failed QUIT does not
// un-send it.
func TestSend_FailedQuitStillCountsAsSent(t *testing.T) {
	s, _ := newTestSender("quit", "")

	if _, err := s.Send(testMessage()); err != nil {
		t.Fatalf("a failed QUIT must not fail the send: %v", err)
	}
}

func TestSend_DialFailureIsUnambiguous(t *testing.T) {
	s := NewGmailSender("smtp.gmail.com", 587, "u", "p", "u@gmail.com", "")
	s.dial = func(string) (smtpClient, error) { return nil, errNetwork }

	_, err := s.Send(testMessage())

	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, ErrAmbiguousDelivery) {
		t.Error("a dial failure means nothing was sent; it is not ambiguous")
	}
}

// The domain allowlist is what stops the service being used as a mail relay.
func TestSend_RejectsDisallowedRecipient(t *testing.T) {
	s, client := newTestSender("", "vcs.com.vn,example.org")

	_, err := s.Send(testMessage()) // ops@example.com

	if !errors.Is(err, ErrRecipientNotAllowed) {
		t.Fatalf("err = %v, want ErrRecipientNotAllowed", err)
	}
	if len(client.steps) != 0 {
		t.Error("connected to SMTP despite a disallowed recipient")
	}
}

func TestAllowRecipient(t *testing.T) {
	s := NewGmailSender("h", 587, "u", "p", "f@g.com", "vcs.com.vn, Example.ORG ")

	cases := map[string]bool{
		"ops@vcs.com.vn":  true,
		"ops@example.org": true,
		"ops@EXAMPLE.org": true,
		"ops@evil.com":    false,
		"no-at-sign":      false,
		"":                false,
	}
	for addr, want := range cases {
		if got := s.AllowRecipient(addr); got != want {
			t.Errorf("AllowRecipient(%q) = %v, want %v", addr, got, want)
		}
	}
}

// An empty allowlist is the local/dev default and permits anything.
func TestAllowRecipient_EmptyAllowlistPermitsAll(t *testing.T) {
	s := NewGmailSender("h", 587, "u", "p", "f@g.com", "")

	if !s.AllowRecipient("anyone@anywhere.com") {
		t.Error("an empty allowlist should permit any recipient")
	}
}

// A Vietnamese subject must not go out as raw 8-bit bytes.
func TestCompose_EncodesNonASCIISubject(t *testing.T) {
	s, client := newTestSender("", "")

	if _, err := s.Send(testMessage()); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	body := client.body.String()
	if strings.Contains(body, "Subject: Báo cáo") {
		t.Error("the non-ASCII subject was not MIME-encoded")
	}
	if !strings.Contains(body, "Subject: =?UTF-8?") {
		t.Errorf("expected an encoded-word subject, got: %q", firstLine(body, "Subject:"))
	}
}

func TestCompose_ASCIISubjectStaysPlain(t *testing.T) {
	s, client := newTestSender("", "")
	msg := testMessage()
	msg.Subject = "Daily report"

	if _, err := s.Send(msg); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if !strings.Contains(client.body.String(), "Subject: Daily report\r\n") {
		t.Error("an ASCII subject should not be encoded")
	}
}

func TestNewMessageID_IsUnique(t *testing.T) {
	s := NewGmailSender("h", 587, "u", "p", "reports@vcs.com.vn", "")

	first, second := s.newMessageID(), s.newMessageID()

	if first == second {
		t.Error("Message-IDs must be unique")
	}
	if !strings.HasSuffix(first, "@vcs.com.vn>") {
		t.Errorf("Message-ID = %q, want it scoped to the From domain", first)
	}
}

func firstLine(body, prefix string) string {
	for line := range strings.SplitSeq(body, "\r\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

// SMTP_FROM may carry a display name, but MAIL FROM only accepts a bare
// address and the Message-ID domain must not inherit the trailing bracket.
func TestSend_HandlesFromWithDisplayName(t *testing.T) {
	client := &fakeClient{}
	s := NewGmailSender("smtp.gmail.com", 587, "u@gmail.com", "pw",
		"VCS-SMS <reports@vcs.com.vn>", "")
	s.dial = func(string) (smtpClient, error) { return client, nil }

	id, err := s.Send(testMessage())
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if s.fromAddr != "reports@vcs.com.vn" {
		t.Errorf("MAIL FROM would use %q, want the bare address", s.fromAddr)
	}
	if !strings.HasSuffix(id, "@vcs.com.vn>") {
		t.Errorf("Message-ID = %q, want it to end @vcs.com.vn>", id)
	}
	// The header keeps the display name; only the envelope is stripped.
	if !strings.Contains(client.body.String(), "From: VCS-SMS <reports@vcs.com.vn>") {
		t.Error("the From header should keep the display name")
	}
}

// An attachment turns the message into multipart/mixed and rides along as
// base64 that must decode back to the original bytes.
func TestCompose_WithAttachment(t *testing.T) {
	s, client := newTestSender("", "")
	msg := testMessage()
	payload := []byte("PK\x03\x04 fake-xlsx-bytes with =formula and \n newline")
	msg.Attachment = &Attachment{
		Filename:    "uptime_2026-07-17.xlsx",
		ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		Content:     payload,
	}

	if _, err := s.Send(msg); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	body := client.body.String()
	if !strings.Contains(body, "Content-Type: multipart/mixed; boundary=") {
		t.Error("message is not multipart/mixed")
	}
	if !strings.Contains(body, "Content-Type: text/html; charset=UTF-8") {
		t.Error("html part missing")
	}
	if !strings.Contains(body, `Content-Disposition: attachment; filename="uptime_2026-07-17.xlsx"`) {
		t.Error("attachment disposition missing or wrong filename")
	}

	// The base64 blob after the attachment headers must decode to the payload.
	marker := "Content-Transfer-Encoding: base64\r\n"
	idx := strings.Index(body, marker)
	if idx < 0 {
		t.Fatal("no base64 attachment part")
	}
	rest := body[idx+len(marker):]
	rest = strings.SplitN(rest, "\r\n\r\n", 2)[1]         // skip to blank line after headers
	rest = strings.SplitN(rest, "\r\n--", 2)[0]           // stop at the closing boundary
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(rest, "\r\n", ""))
	if err != nil {
		t.Fatalf("attachment is not valid base64: %v", err)
	}
	if !bytes.Equal(decoded, payload) {
		t.Errorf("decoded attachment = %q, want %q", decoded, payload)
	}
}

// Without an attachment the message stays a single text/html part.
func TestCompose_NoAttachmentStaysSinglePart(t *testing.T) {
	s, client := newTestSender("", "")

	if _, err := s.Send(testMessage()); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if strings.Contains(client.body.String(), "multipart") {
		t.Error("a message with no attachment should not be multipart")
	}
}

func TestBareAddress(t *testing.T) {
	cases := map[string]string{
		"VCS-SMS <a@b.com>": "a@b.com",
		"<a@b.com>":         "a@b.com",
		"a@b.com":           "a@b.com",
		"  a@b.com  ":       "a@b.com",
	}
	for in, want := range cases {
		if got := bareAddress(in); got != want {
			t.Errorf("bareAddress(%q) = %q, want %q", in, got, want)
		}
	}
}
