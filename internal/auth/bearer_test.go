package auth

import "testing"

func TestValidateBearerToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		token     string
		wantError bool
	}{
		{name: "opaque", token: "controller-token_123"},
		{name: "base64", token: "Y29udHJvbGxlcg=="},
		{name: "empty", token: "", wantError: true},
		{name: "space", token: "controller token", wantError: true},
		{name: "padding in middle", token: "controller=token", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateBearerToken(test.token)
			if test.wantError && err == nil {
				t.Fatal("ValidateBearerToken() error = nil, want an error")
			}

			if !test.wantError && err != nil {
				t.Fatalf("ValidateBearerToken() error = %v", err)
			}
		})
	}
}
