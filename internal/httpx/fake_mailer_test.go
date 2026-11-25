package httpx

import (
	"context"

	"github.com/inwinter04/omniflux/internal/mail"
)

type fakeMailer struct {
	messages []mail.Message
	settings mail.StoredSettings
}

func (m *fakeMailer) Send(_ context.Context, message mail.Message) error {
	m.messages = append(m.messages, message)
	return nil
}

func (m *fakeMailer) Config(_ context.Context) (mail.StoredSettings, error) {
	return m.settings, nil
}
