package email

import (
	"crypto/tls"
	"mime"
	"net/smtp"
)

// realClient adapts *smtp.Client to the smtpClient interface.
type realClient struct {
	c *smtp.Client
}

func (r *realClient) StartTLS(cfg *tls.Config) error { return r.c.StartTLS(cfg) }
func (r *realClient) Auth(a smtp.Auth) error         { return r.c.Auth(a) }
func (r *realClient) Mail(from string) error         { return r.c.Mail(from) }
func (r *realClient) Rcpt(to string) error           { return r.c.Rcpt(to) }
func (r *realClient) Quit() error                    { return r.c.Quit() }
func (r *realClient) Close() error                   { return r.c.Close() }

func (r *realClient) Data() (writeCloser, error) {
	w, err := r.c.Data()
	if err != nil {
		return nil, err
	}
	return w, nil
}

func dialSMTP(addr string) (smtpClient, error) {
	c, err := smtp.Dial(addr)
	if err != nil {
		return nil, err
	}
	return &realClient{c: c}, nil
}

func mimeEncode(s string) string {
	return mime.QEncoding.Encode("UTF-8", s)
}
