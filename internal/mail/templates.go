package mail

import (
	"fmt"
	"html"
	"net/url"
)

func linkMessage(to, subject, title, body, actionLabel, link string) Message {
	escapedLink := html.EscapeString(link)
	return Message{
		To:      to,
		Subject: subject,
		Text:    fmt.Sprintf("%s\n\n%s\n\n%s:\n%s\n\nThis link expires soon. / 该链接即将过期。\n", title, body, actionLabel, link),
		HTML:    fmt.Sprintf(`<!doctype html><html><body style="font-family:Arial,sans-serif;color:#202124"><h2>%s</h2><p>%s</p><p><a href="%s" style="display:inline-block;padding:10px 16px;background:#2563eb;color:#fff;border-radius:6px;text-decoration:none">%s</a></p><p style="font-size:12px;color:#5f6368;word-break:break-all">%s</p><p style="font-size:12px;color:#5f6368">This link expires soon. / 该链接即将过期。</p></body></html>`, html.EscapeString(title), html.EscapeString(body), escapedLink, html.EscapeString(actionLabel), escapedLink),
	}
}

func VerificationMessage(to, token string, baseURL string) Message {
	link := baseURL + "/admin/verify-email?token=" + url.QueryEscape(token)
	return linkMessage(
		to,
		"Omniflux email verification / Omniflux 邮箱验证",
		"Verify your Omniflux email / 验证 Omniflux 邮箱",
		"Use the button to finish creating your account. / 点击按钮完成账号创建。",
		"Verify email / 验证邮箱",
		link,
	)
}

func InvitationMessage(to, token string, baseURL string) Message {
	link := baseURL + "/admin/accept-invitation?token=" + url.QueryEscape(token)
	return linkMessage(
		to,
		"Omniflux invitation / Omniflux 邀请",
		"Join Omniflux / 加入 Omniflux",
		"An administrator invited you to Omniflux. Set your password with the button. / 管理员邀请你加入 Omniflux，请点击按钮设置密码。",
		"Accept invitation / 接受邀请",
		link,
	)
}

func PasswordResetMessage(to, token string, baseURL string) Message {
	link := baseURL + "/admin/reset-password?token=" + url.QueryEscape(token)
	return linkMessage(
		to,
		"Omniflux password reset / Omniflux 密码重置",
		"Reset your Omniflux password / 重置 Omniflux 密码",
		"Use the button to choose a new password. If you did not request this, ignore this email. / 点击按钮设置新密码；如非本人操作，请忽略。",
		"Reset password / 重置密码",
		link,
	)
}

func TestMessage(to string) Message {
	return Message{
		To:      to,
		Subject: "Omniflux SMTP test / Omniflux SMTP 测试",
		Text:    "Omniflux sent this test email successfully. / Omniflux 测试邮件发送成功。\n",
		HTML:    `<!doctype html><html><body style="font-family:Arial,sans-serif;color:#202124"><p>Omniflux sent this test email successfully. / Omniflux 测试邮件发送成功。</p></body></html>`,
	}
}
