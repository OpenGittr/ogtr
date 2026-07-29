// Package email sends ogtr's transactional emails (welcome, org invites, ...)
// over SMTP — AWS SES SMTP in production, any SMTP server for self-hosters.
//
// It is CONFIG-GATED: with no SMTP configured (or a nil Sender) every method is
// a no-op, so local dev and self-hosted deployments without mail still work.
// Sends run in the BACKGROUND and are BEST-EFFORT — a mail failure is logged
// and never blocks or fails the user action that triggered it.
package email

import (
	"fmt"
	"html"
	"log"
	"net/smtp"
	"strings"
)

// Sender holds SMTP config and builds/sends the branded emails. The zero value
// (and a nil *Sender) are disabled no-ops.
type Sender struct {
	enabled bool
	addr    string // host:port
	auth    smtp.Auth
	from    string // bare address, e.g. noreply@ogtr.in
	appURL  string // dashboard base URL for call-to-action links
}

// New builds a Sender from SMTP config. An empty host, username or from-address
// leaves it disabled (a logged no-op) — the app runs fine without mail.
func New(host, port, username, password, from, appURL string) *Sender {
	if host == "" || username == "" || from == "" {
		return &Sender{}
	}

	if port == "" {
		port = "587"
	}

	return &Sender{
		enabled: true,
		addr:    host + ":" + port,
		auth:    smtp.PlainAuth("", username, password, host),
		from:    from,
		appURL:  appURL,
	}
}

// Enabled reports whether mail is configured (nil-safe). Callers use it to skip
// building email inputs (e.g. name lookups) when nothing would be sent.
func (s *Sender) Enabled() bool { return s != nil && s.enabled }

// SendWelcome greets a newly-created user (called on first sign-in).
func (s *Sender) SendWelcome(toEmail, name string) {
	if s == nil || !s.enabled {
		return
	}

	greeting := "there"
	if n := firstName(strings.TrimSpace(name)); n != "" {
		greeting = n
	}

	body := fmt.Sprintf(
		`<p style="margin:0 0 12px;">Hi %s, thanks for signing up. ogtr turns long URLs into short, `+
			`branded links — with QR codes, campaign analytics, device &amp; location targeting and mobile `+
			`deep links, all on every plan.</p>`+
			`<p style="margin:0;">Create your first link and it's instantly ready to share and track.</p>`,
		html.EscapeString(greeting))

	s.sendAsync(toEmail, "Welcome to ogtr.in",
		render("Welcome to ogtr", body, &cta{Label: "Open your dashboard", URL: s.appURL}))
}

// SendInvite tells someone they've been invited to an org. The invite is
// accepted simply by signing in with the invited email (auto-join), so the CTA
// is the dashboard.
func (s *Sender) SendInvite(toEmail, orgName, inviterName string) {
	if s == nil || !s.enabled {
		return
	}

	who := strings.TrimSpace(inviterName)
	if who == "" {
		who = "Someone"
	}

	if strings.TrimSpace(orgName) == "" {
		orgName = "an organization"
	}

	body := fmt.Sprintf(
		`<p style="margin:0 0 12px;"><strong>%s</strong> invited you to join <strong>%s</strong> on ogtr.in.</p>`+
			`<p style="margin:0;">Sign in with this email address (%s) to accept and start creating and `+
			`sharing short links together.</p>`,
		html.EscapeString(who), html.EscapeString(orgName), html.EscapeString(toEmail))

	s.sendAsync(toEmail, fmt.Sprintf("You're invited to %s on ogtr.in", orgName),
		render("You've been invited", body, &cta{Label: "Accept your invite", URL: s.appURL}))
}

func (s *Sender) sendAsync(to, subject, htmlBody string) {
	go func() {
		if err := s.send(to, subject, htmlBody); err != nil {
			log.Printf("email: sending %q to %s failed: %v", subject, to, err)
		}
	}()
}

func (s *Sender) send(to, subject, htmlBody string) error {
	msg := "From: ogtr.in <" + s.from + ">\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=\"utf-8\"\r\n" +
		"\r\n" + htmlBody

	return smtp.SendMail(s.addr, s.auth, s.from, []string{to}, []byte(msg))
}

func firstName(name string) string {
	if i := strings.IndexByte(name, ' '); i > 0 {
		return name[:i]
	}

	return name
}
