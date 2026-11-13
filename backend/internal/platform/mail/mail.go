// Package mail sends transactional email. In development it delivers to Mailpit over SMTP.
package mail

import (
	"bytes"
	"context"
	"fmt"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"sync"
	"time"
)

type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

type Sender interface {
	Send(ctx context.Context, m Message) error
}

// SMTP sends without authentication or TLS, which is what Mailpit expects locally.
type SMTP struct {
	Host string
	Port int
	From string
}

func (s SMTP) Send(_ context.Context, m Message) error {
	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return fmt.Errorf("parse from address: %w", err)
	}
	msg, err := build(s.From, m)
	if err != nil {
		return err
	}
	addr := s.Host + ":" + strconv.Itoa(s.Port)
	return smtp.SendMail(addr, nil, from.Address, []string{m.To}, msg)
}

func build(from string, m Message) ([]byte, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	for _, part := range []struct{ ctype, content string }{
		{"text/plain; charset=utf-8", m.Text},
		{"text/html; charset=utf-8", m.HTML},
	} {
		if part.content == "" {
			continue
		}
		w, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.ctype},
			"Content-Transfer-Encoding": {"8bit"},
		})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(part.content)); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	fmt.Fprintf(&out, "From: %s\r\n", from)
	fmt.Fprintf(&out, "To: %s\r\n", m.To)
	fmt.Fprintf(&out, "Subject: %s\r\n", mimeHeader(m.Subject))
	fmt.Fprintf(&out, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&out, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&out, "Content-Type: multipart/alternative; boundary=%s\r\n\r\n", mw.Boundary())
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

func mimeHeader(s string) string {
	return mime.QEncoding.Encode("utf-8", s)
}

// Recorder captures messages instead of sending them. Used in tests.
type Recorder struct {
	mu   sync.Mutex
	Sent []Message
}

func (r *Recorder) Send(_ context.Context, m Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Sent = append(r.Sent, m)
	return nil
}

// Last returns the most recent message sent to the address.
func (r *Recorder) Last(to string) (Message, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.Sent) - 1; i >= 0; i-- {
		if r.Sent[i].To == to {
			return r.Sent[i], true
		}
	}
	return Message{}, false
}
