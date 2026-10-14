package mail

import (
	"strings"
	"testing"
)

// The From value is configured as a header ("Name <addr>"), but SMTP needs a bare
// addr-spec for its envelope. Getting that wrong is invisible in a happy-path test
// that only asserts registration succeeded — it shows up as a 501 from the server
// and a learner who never receives a verification link.

func TestParseFrom(t *testing.T) {
	cases := []struct {
		name         string
		from         string
		wantEnvelope string
		// Header values are asserted by content, not by exact form: net/mail quotes
		// a display name ("hanpath" <addr>), which is Go's formatting choice and not
		// something this package should pin.
		wantHeaderContains []string
		wantErr            bool
	}{
		{
			name:               "display name plus address",
			from:               "hanpath <no-reply@hanpath.local>",
			wantEnvelope:       "no-reply@hanpath.local",
			wantHeaderContains: []string{"hanpath", "no-reply@hanpath.local"},
		},
		{
			name:               "bare address",
			from:               "no-reply@hanpath.local",
			wantEnvelope:       "no-reply@hanpath.local",
			wantHeaderContains: []string{"no-reply@hanpath.local"},
		},
		{
			name:               "surrounding whitespace is trimmed",
			from:               "  hanpath <no-reply@hanpath.local>  ",
			wantEnvelope:       "no-reply@hanpath.local",
			wantHeaderContains: []string{"hanpath", "no-reply@hanpath.local"},
		},
		{
			name:    "empty",
			from:    "   ",
			wantErr: true,
		},
		{
			name:    "not an address at all",
			from:    "hanpath",
			wantErr: true,
		},
		{
			name:    "header injection via a newline",
			from:    "hanpath <no-reply@hanpath.local>\r\nBcc: attacker@example.com",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header, envelope, err := ParseFrom(tc.from)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseFrom(%q) accepted an invalid value: header=%q envelope=%q", tc.from, header, envelope)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFrom(%q): %v", tc.from, err)
			}
			// The envelope must be a bare addr-spec: this is exactly the value
			// Mailpit and Postfix reject with "501 ... invalid FROM parameter".
			if envelope != tc.wantEnvelope {
				t.Errorf("envelope = %q, want %q", envelope, tc.wantEnvelope)
			}
			if strings.ContainsAny(envelope, "<>") {
				t.Errorf("envelope %q carries angle brackets and would be rejected by the server", envelope)
			}
			if strings.ContainsAny(header, "\r\n") {
				t.Errorf("header %q contains a line break", header)
			}
			for _, want := range tc.wantHeaderContains {
				if !strings.Contains(header, want) {
					t.Errorf("header = %q, want it to contain %q", header, want)
				}
			}
		})
	}
}

func TestCheckHeaderValueRejectsInjection(t *testing.T) {
	if err := checkHeaderValue("Subject", "Reset your password\r\nBcc: attacker@example.com"); err == nil {
		t.Error("a newline in a header value must be refused")
	}
	if err := checkHeaderValue("Subject", "Reset your password"); err != nil {
		t.Errorf("a plain subject should be accepted, got %v", err)
	}
	// A display name is user input on this codebase (registration uses it in the
	// email body, not the header, but the rule must hold either way).
	if err := checkHeaderValue("To", "Eve\nX-Injected: yes <eve@example.com>"); err == nil {
		t.Error("a newline in a recipient must be refused")
	}
}

func TestLogSenderIsUsedWhenNoSMTPIsConfigured(t *testing.T) {
	// A missing SMTP configuration must degrade to the log transport, never to a
	// dropped message.
	if _, ok := New("", "", nil).(LogSender); !ok {
		t.Fatal("with no SMTP address the sender must be the log transport")
	}
	if _, ok := New("127.0.0.1:1025", "hanpath <no-reply@hanpath.local>", nil).(SMTPSender); !ok {
		t.Fatal("with an SMTP address the sender must be the SMTP transport")
	}
}
