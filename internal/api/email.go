package api

// EmailSender is a minimal interface for sending emails from the admin API.
// Concrete implementation (e.g., Resend) is provided at wiring time.
type EmailSender interface {
    SendReset(to, link string) error
    SendVerification(to, link string) error
    SendWaitlistConfirmation(to string) error
}

