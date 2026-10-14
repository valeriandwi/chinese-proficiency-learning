package httpapi

import "fmt"

// Plain-text transactional email bodies. Kept as functions so the wording lives
// in one place and can be moved to templates without hunting through handlers.

func verificationBody(name, link string) string {
	return fmt.Sprintf(`你好 %s,

Welcome to hanpath. Confirm your email address so we can keep your streak and
send you study reminders:

%s

The link is valid for 48 hours. If you did not create this account you can
ignore this message.

— hanpath`, name, link)
}

func resetBody(name, link string) string {
	return fmt.Sprintf(`你好 %s,

Someone (hopefully you) asked to reset the password on your hanpath account.

%s

The link is valid for 2 hours and can only be used once. Setting a new password
signs you out of every other device.

— hanpath`, name, link)
}
