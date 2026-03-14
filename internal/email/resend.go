package email

import (
    "fmt"

    resend "github.com/resend/resend-go/v3"
)

type ResendSender struct {
    client *resend.Client
    from   string
}

func NewResendSender(apiKey, from string) *ResendSender {
    if apiKey == "" || from == "" {
        return nil
    }
    return &ResendSender{client: resend.NewClient(apiKey), from: from}
}

// Shared email styles matching the majordomo-web warm terracotta palette.
const emailStyles = `
      .container{max-width:560px;margin:24px auto;padding:0 16px;font-family:"Inter",ui-sans-serif,system-ui,-apple-system,Segoe UI,Roboto,Ubuntu,Cantarell,Noto Sans,"Helvetica Neue",Arial,"Apple Color Emoji","Segoe UI Emoji";color:#2C2520}
      .card{border:1px solid #E5E0DB;border-radius:12px;padding:24px;background:#ffffff}
      .brand{margin-bottom:16px}
      .badge{display:inline-block;border:1px solid #F0D5C5;background:#FDF5F0;color:#9B4A28;border-radius:999px;padding:2px 8px;font-size:12px;font-weight:600;margin-bottom:8px}
      .title{font-size:20px;line-height:28px;margin:6px 0 12px 0}
      .muted{color:#6E6560;font-size:14px;margin-top:4px}
      .btn{display:inline-block;margin-top:16px;background:#9B4A28;color:#ffffff !important;text-decoration:none;padding:10px 16px;border-radius:8px;font-weight:600}
      .link{font-size:12px;color:#7A3B20;word-break:break-all;margin-top:10px}
      .footer{color:#6E6560;font-size:12px;margin-top:16px}
`

// Shared brand header — plain text wordmark, no icon.
const emailBrandHeader = `<div class="brand"><table cellpadding="0" cellspacing="0" border="0"><tr><td style="width:40px;height:40px;border-radius:12px;background:#9B4A28;color:#fff;font-size:18px;font-weight:700;text-align:center;line-height:40px;vertical-align:middle">M</td><td style="padding-left:14px;font-weight:600;font-size:18px;color:#2C2520;vertical-align:middle;letter-spacing:-0.01em">Majordomo</td></tr></table></div>`

func (s *ResendSender) SendReset(to, link string) error {
    if s == nil || s.client == nil || to == "" || link == "" {
        return nil
    }

    subject := "Reset your Majordomo password"
    text := fmt.Sprintf("Reset your Majordomo password: %s\nThis link expires in 1 hour.", link)
    html := fmt.Sprintf(`<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>%[1]s</title>
    <style>%[3]s</style>
  </head>
  <body>
    <div class="container">
      %[4]s
      <div class="card">
        <div class="badge">Account Security</div>
        <div class="title">Reset your password</div>
        <div class="muted">Click the button below to set a new password. This link will expire in 1 hour.</div>
        <a class="btn" href="%[2]s" target="_blank" rel="noopener" style="color:#ffffff">Reset Password</a>
        <div class="link">If the button doesn't work, copy and paste this URL into your browser:<br />%[2]s</div>
        <div class="footer">If you didn't request this, you can safely ignore this email.</div>
      </div>
    </div>
  </body>
  </html>`, subject, link, emailStyles, emailBrandHeader)

    params := &resend.SendEmailRequest{
        From:    s.from,
        To:      []string{to},
        Subject: subject,
        Html:    html,
        Text:    text,
    }
    _, err := s.client.Emails.Send(params)
    return err
}

func (s *ResendSender) SendVerification(to, link string) error {
    if s == nil || s.client == nil || to == "" || link == "" {
        return nil
    }

    subject := "Verify your Majordomo email"
    text := fmt.Sprintf("Verify your Majordomo email: %s\nThis link expires in 24 hours.", link)
    html := fmt.Sprintf(`<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>%[1]s</title>
    <style>%[3]s</style>
  </head>
  <body>
    <div class="container">
      %[4]s
      <div class="card">
        <div class="badge">Email Verification</div>
        <div class="title">Verify your email</div>
        <div class="muted">Click the button below to verify your email address. This link will expire in 24 hours.</div>
        <a class="btn" href="%[2]s" target="_blank" rel="noopener" style="color:#ffffff">Verify Email</a>
        <div class="link">If the button doesn't work, copy and paste this URL into your browser:<br />%[2]s</div>
        <div class="footer">If you didn't create this account, you can safely ignore this email.</div>
      </div>
    </div>
  </body>
  </html>`, subject, link, emailStyles, emailBrandHeader)

    params := &resend.SendEmailRequest{
        From:    s.from,
        To:      []string{to},
        Subject: subject,
        Html:    html,
        Text:    text,
    }
    _, err := s.client.Emails.Send(params)
    return err
}
