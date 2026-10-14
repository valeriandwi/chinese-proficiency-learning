// Package mail sends transactional email. Development uses the log transport
// unless SMTP_ADDR is configured (Mailpit in compose accepts everything).
package mail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	netmail "net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Message is a plain-text transactional email.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender delivers messages.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// LogSender writes messages to the application log. It exists so a developer
// environment works with no mail server, and so a missing SMTP configuration can
// never silently discard a password reset.
type LogSender struct{ Log *slog.Logger }

func (l LogSender) Send(_ context.Context, m Message) error {
	l.Log.Info("mail (log transport)", "to", m.To, "subject", m.Subject, "body", m.Body)
	return nil
}

// SMTPSender delivers over SMTP.
type SMTPSender struct {
	Addr string
	From string
	Log  *slog.Logger
}

// Send formats and delivers one message.
//
// The configured From value is a *header* value that may carry a display name
// ("hanpath <no-reply@hanpath.local>"), while SMTP wants a bare addr-spec for its
// envelope. Handing the display form to the envelope is rejected by many servers
// with "501 5.5.4 ... invalid FROM parameter" — and because a failed send happens
// after the account is created, registration still returns 201 and the learner
// simply never receives the link. The two forms are resolved separately.
func (s SMTPSender) Send(_ context.Context, m Message) error {
	if err := checkHeaderValue("To", m.To); err != nil {
		return err
	}
	if err := checkHeaderValue("Subject", m.Subject); err != nil {
		return err
	}
	headerFrom, envelopeFrom, err := ParseFrom(s.From)
	if err != nil {
		return err
	}
	envelopeTo, err := bareAddress(m.To)
	if err != nil {
		return err
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n",
		headerFrom, envelopeTo, m.Subject, time.Now().Format(time.RFC1123Z),
		strings.ReplaceAll(m.Body, "\n", "\r\n"))

	// Dev compose uses Mailpit, which accepts unauthenticated mail and needs no TLS.
	if err := smtp.SendMail(s.Addr, nil, envelopeFrom, []string{envelopeTo}, []byte(msg)); err != nil {
		return fmt.Errorf("mail: sending to %s: %w", envelopeTo, err)
	}
	return nil
}

// ParseFrom splits a configured From value into the header form and the bare
// envelope address, rejecting anything that could inject a header.
func ParseFrom(from string) (header string, envelope string, err error) {
	from = strings.TrimSpace(from)
	if from == "" {
		return "", "", errors.New("mail: SMTP_FROM is empty")
	}
	if err := checkHeaderValue("SMTP_FROM", from); err != nil {
		return "", "", err
	}
	addr, err := netmail.ParseAddress(from)
	if err != nil {
		return "", "", fmt.Errorf("mail: SMTP_FROM %q is not a valid address: %w", from, err)
	}
	return addr.String(), addr.Address, nil
}

// bareAddress returns the addr-spec of a recipient.
func bareAddress(to string) (string, error) {
	to = strings.TrimSpace(to)
	if to == "" {
		return "", errors.New("mail: recipient is empty")
	}
	addr, err := netmail.ParseAddress(to)
	if err != nil {
		return "", fmt.Errorf("mail: recipient %q is not a valid address: %w", to, err)
	}
	return addr.Address, nil
}

// checkHeaderValue refuses anything that could inject an extra header. A newline
// in a value that reaches a raw SMTP session is a header-injection primitive, and
// these values come from user input.
func checkHeaderValue(name, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("mail: %s must not contain line breaks", name)
	}
	return nil
}

// New picks the SMTP transport when an address is configured.
func New(addr, from string, log *slog.Logger) Sender {
	if addr == "" {
		return LogSender{Log: log}
	}
	return SMTPSender{Addr: addr, From: from, Log: log}
}
