package httpx

import "testing"

func TestNormalizeGatewayBaseURL(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantURL   string
		wantValid bool
	}{
		{name: "empty uses auto detection", input: "   ", wantURL: "", wantValid: true},
		{name: "https with trailing slash", input: "https://gateway.example.com/", wantURL: "https://gateway.example.com", wantValid: true},
		{name: "http with port and path", input: "http://127.0.0.1:8080/edge/", wantURL: "http://127.0.0.1:8080/edge", wantValid: true},
		{name: "ftp scheme rejected", input: "ftp://gateway.example.com", wantValid: false},
		{name: "missing host rejected", input: "https:///path", wantValid: false},
		{name: "query string rejected", input: "https://gateway.example.com?foo=1", wantValid: false},
		{name: "fragment rejected", input: "https://gateway.example.com#hash", wantValid: false},
		{name: "userinfo rejected", input: "https://user:pass@gateway.example.com", wantValid: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotURL, gotValid := normalizeGatewayBaseURL(tc.input)
			if gotValid != tc.wantValid || gotURL != tc.wantURL {
				t.Fatalf("normalizeGatewayBaseURL(%q) = (%q, %v), want (%q, %v)", tc.input, gotURL, gotValid, tc.wantURL, tc.wantValid)
			}
		})
	}
}
