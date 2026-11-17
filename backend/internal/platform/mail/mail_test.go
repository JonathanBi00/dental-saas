package mail_test

import (
	"bufio"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net"
	netmail "net/mail"
	"strings"
	"testing"

	"dentalsaas/internal/platform/mail"
)

// received is one message as the fake server saw it.
type received struct {
	From string
	To   []string
	Data string
}

// fakeSMTP is a minimal SMTP server (no TLS, no auth, like Mailpit) that records messages.
func fakeSMTP(t *testing.T) (host string, port int, messages <-chan received) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	out := make(chan received, 8)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				say := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }
				say("220 fake.test ESMTP")
				var msg received
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					line = strings.TrimRight(line, "\r\n")
					cmd := strings.ToUpper(line)
					switch {
					case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
						say("250 fake.test")
					case strings.HasPrefix(cmd, "MAIL FROM:"):
						msg = received{From: strings.Trim(strings.Fields(line[len("MAIL FROM:"):])[0], "<>")}
						say("250 ok")
					case strings.HasPrefix(cmd, "RCPT TO:"):
						msg.To = append(msg.To, strings.Trim(strings.TrimSpace(line[len("RCPT TO:"):]), "<>"))
						say("250 ok")
					case cmd == "DATA":
						say("354 go ahead")
						var data strings.Builder
						for {
							l, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if l == ".\r\n" {
								break
							}
							data.WriteString(strings.TrimPrefix(l, ".")) // undo dot-stuffing
						}
						msg.Data = data.String()
						out <- msg
						say("250 queued")
					case cmd == "QUIT":
						say("221 bye")
						return
					default:
						say("502 not implemented")
					}
				}
			}()
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port, out
}

// Task 4.6: a message sent over SMTP arrives with the right envelope, headers and both the
// plain-text and HTML alternatives.
func TestSMTPSendDeliversTextAndHTML(t *testing.T) {
	host, port, messages := fakeSMTP(t)
	sender := mail.SMTP{Host: host, Port: port, From: "Dental SaaS <no-reply@dental.test>"}

	text := "Hi,\n\nAccept the invitation: http://app.test/invite/abc123\n.\nA line that starts with a dot above.\n"
	html := `<p>Accept: <a href="http://app.test/invite/abc123">Accept invitation</a></p>`
	err := sender.Send(context.Background(), mail.Message{
		To: "new@clinic.test", Subject: "You're invited to join Café Dental — Sydney", Text: text, HTML: html,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	var got received
	select {
	case got = <-messages:
	default:
		t.Fatal("the server received no message")
	}
	if got.From != "no-reply@dental.test" {
		t.Errorf("envelope sender = %q, want the bare From address", got.From)
	}
	if len(got.To) != 1 || got.To[0] != "new@clinic.test" {
		t.Errorf("envelope recipients = %v", got.To)
	}

	m, err := netmail.ReadMessage(strings.NewReader(got.Data))
	if err != nil {
		t.Fatalf("message is not parseable: %v\n%s", err, got.Data)
	}
	if from, err := m.Header.AddressList("From"); err != nil || len(from) != 1 || from[0].Address != "no-reply@dental.test" || from[0].Name != "Dental SaaS" {
		t.Errorf("From = %q (%v)", m.Header.Get("From"), err)
	}
	if to := m.Header.Get("To"); to != "new@clinic.test" {
		t.Errorf("To = %q", to)
	}
	if _, err := m.Header.Date(); err != nil {
		t.Errorf("Date header %q: %v", m.Header.Get("Date"), err)
	}
	// Non-ASCII subjects are encoded on the wire and decode back to the original.
	rawSubject := m.Header.Get("Subject")
	for _, r := range rawSubject {
		if r > 127 {
			t.Errorf("Subject header is not ASCII-safe: %q", rawSubject)
			break
		}
	}
	if subject, err := new(mime.WordDecoder).DecodeHeader(rawSubject); err != nil || subject != "You're invited to join Café Dental — Sydney" {
		t.Errorf("Subject = %q (%v)", subject, err)
	}

	mediaType, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("Content-Type = %q (%v), want multipart/alternative", m.Header.Get("Content-Type"), err)
	}
	parts := map[string]string{}
	var order []string
	mr := multipart.NewReader(m.Body, params["boundary"])
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read part: %v", err)
		}
		ct, ctParams, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		if !strings.EqualFold(ctParams["charset"], "utf-8") {
			t.Errorf("%s part charset = %q, want utf-8", ct, ctParams["charset"])
		}
		body, _ := io.ReadAll(p)
		parts[ct] = strings.ReplaceAll(string(body), "\r\n", "\n")
		order = append(order, ct)
	}
	// Plain text first: clients show the last alternative they can render.
	if len(order) != 2 || order[0] != "text/plain" || order[1] != "text/html" {
		t.Fatalf("parts = %v, want text/plain then text/html", order)
	}
	if strings.TrimRight(parts["text/plain"], "\n") != strings.TrimRight(text, "\n") {
		t.Errorf("text part = %q, want %q", parts["text/plain"], text)
	}
	if strings.TrimRight(parts["text/html"], "\n") != html {
		t.Errorf("html part = %q, want %q", parts["text/html"], html)
	}
}

func TestSMTPSendTextOnly(t *testing.T) {
	host, port, messages := fakeSMTP(t)
	sender := mail.SMTP{Host: host, Port: port, From: "no-reply@dental.test"}
	if err := sender.Send(context.Background(), mail.Message{To: "a@clinic.test", Subject: "Plain", Text: "Just text."}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got := <-messages
	if !strings.Contains(got.Data, "Just text.") || strings.Contains(got.Data, "text/html") {
		t.Errorf("message should have only a text part:\n%s", got.Data)
	}
}

func TestSMTPSendErrors(t *testing.T) {
	// Nothing listening: the error is returned to the caller, not swallowed.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	down := mail.SMTP{Host: "127.0.0.1", Port: port, From: "no-reply@dental.test"}
	if err := down.Send(context.Background(), mail.Message{To: "a@clinic.test", Subject: "x", Text: "x"}); err == nil {
		t.Error("Send to a closed port returned nil")
	}

	bad := mail.SMTP{Host: "127.0.0.1", Port: port, From: "not an address"}
	if err := bad.Send(context.Background(), mail.Message{To: "a@clinic.test", Subject: "x", Text: "x"}); err == nil {
		t.Error("Send with an invalid From address returned nil")
	}
}

func TestRecorderLast(t *testing.T) {
	var r mail.Recorder
	ctx := context.Background()
	if _, ok := r.Last("a@clinic.test"); ok {
		t.Error("Last on an empty recorder reported a message")
	}
	_ = r.Send(ctx, mail.Message{To: "a@clinic.test", Subject: "first"})
	_ = r.Send(ctx, mail.Message{To: "b@clinic.test", Subject: "other"})
	_ = r.Send(ctx, mail.Message{To: "a@clinic.test", Subject: "second"})
	if m, ok := r.Last("a@clinic.test"); !ok || m.Subject != "second" {
		t.Errorf("Last = %+v, %v; want the second message to a@", m, ok)
	}
	if len(r.Sent) != 3 {
		t.Errorf("Sent = %d messages, want 3", len(r.Sent))
	}
}
