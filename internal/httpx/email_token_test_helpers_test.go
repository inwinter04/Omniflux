package httpx

import (
	"net/url"
	"strings"
	"testing"
)

func lastEmailToken(t *testing.T, env *integrationEnv) string {
	t.Helper()
	if len(env.mailer.messages) == 0 {
		t.Fatal("fake mailer has no message")
	}
	message := env.mailer.messages[len(env.mailer.messages)-1]
	index := strings.LastIndex(message.Text, "token=")
	if index < 0 {
		t.Fatalf("email text has no token: %q", message.Text)
	}
	value := message.Text[index+len("token="):]
	if end := strings.IndexAny(value, " \r\n"); end >= 0 {
		value = value[:end]
	}
	token, err := url.QueryUnescape(value)
	if err != nil {
		t.Fatalf("unescape email token: %v", err)
	}
	return token
}
