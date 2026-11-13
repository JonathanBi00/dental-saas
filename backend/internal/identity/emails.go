package identity

import (
	"bytes"
	"context"
	"html/template"
	"log/slog"
	"strings"
	textt "text/template"

	"dentalsaas/internal/platform/mail"
)

type emailTemplate struct {
	subject *textt.Template
	text    *textt.Template
	html    *template.Template
}

func newEmailTemplate(subject, text, html string) emailTemplate {
	return emailTemplate{
		subject: textt.Must(textt.New("s").Parse(subject)),
		text:    textt.Must(textt.New("t").Parse(text)),
		html:    template.Must(template.New("h").Parse(html)),
	}
}

func (t emailTemplate) render(to string, data any) (mail.Message, error) {
	var s, txt, h bytes.Buffer
	if err := t.subject.Execute(&s, data); err != nil {
		return mail.Message{}, err
	}
	if err := t.text.Execute(&txt, data); err != nil {
		return mail.Message{}, err
	}
	if err := t.html.Execute(&h, data); err != nil {
		return mail.Message{}, err
	}
	return mail.Message{To: to, Subject: strings.TrimSpace(s.String()), Text: txt.String(), HTML: h.String()}, nil
}

const htmlLayoutStart = `<div style="font-family:system-ui,sans-serif;max-width:520px;margin:auto;color:#1f2937">`
const htmlLayoutEnd = `<p style="color:#6b7280;font-size:13px">If you weren't expecting this email, you can ignore it.</p></div>`

var invitationEmail = newEmailTemplate(
	`You're invited to join {{.OrgName}}`,
	`Hi,

You've been invited to join {{.OrgName}} on Dental SaaS as {{.Role}}.

Accept the invitation: {{.Link}}

This link expires in 7 days and can be used once.
`,
	htmlLayoutStart+`
<p>Hi,</p>
<p>You've been invited to join <strong>{{.OrgName}}</strong> on Dental SaaS as <strong>{{.Role}}</strong>.</p>
<p><a href="{{.Link}}" style="display:inline-block;background:#0f766e;color:#fff;padding:10px 16px;border-radius:6px;text-decoration:none">Accept invitation</a></p>
<p style="font-size:13px">Or paste this link into your browser: {{.Link}}<br>It expires in 7 days and can be used once.</p>
`+htmlLayoutEnd)

var resetEmail = newEmailTemplate(
	`Reset your Dental SaaS password`,
	`Hi {{.Name}},

Someone asked to reset the password for your Dental SaaS account.

Choose a new password: {{.Link}}

This link expires in 60 minutes and can be used once. Resetting signs you out everywhere.
`,
	htmlLayoutStart+`
<p>Hi {{.Name}},</p>
<p>Someone asked to reset the password for your Dental SaaS account.</p>
<p><a href="{{.Link}}" style="display:inline-block;background:#0f766e;color:#fff;padding:10px 16px;border-radius:6px;text-decoration:none">Choose a new password</a></p>
<p style="font-size:13px">Or paste this link into your browser: {{.Link}}<br>It expires in 60 minutes and can be used once. Resetting signs you out everywhere.</p>
`+htmlLayoutEnd)

// send renders and sends an email after the transaction that created it has committed.
// Delivery failures are logged, not returned: the record already exists and the action
// can be repeated (re-invite / request another reset).
func (s *Service) send(ctx context.Context, t emailTemplate, to string, data any) {
	msg, err := t.render(to, data)
	if err == nil {
		err = s.Mail.Send(ctx, msg)
	}
	if err != nil {
		slog.ErrorContext(ctx, "send email failed", "subject", msg.Subject, "error", err)
	}
}
