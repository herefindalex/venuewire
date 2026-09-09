package rest

import "testing"

func TestSignKnownVectors(t *testing.T) {
	t.Parallel()
	const (
		timestamp  = int64(1700000000123)
		apiKey     = "test-api-key"
		secret     = "test-secret"
		recvWindow = int64(5000)
	)
	tests := []struct {
		name, payload, want string
	}{
		{
			name:    "GET query",
			payload: "category=linear&symbol=BTCUSDT",
			want:    "1eb74d5b364f7d9a7fa48e79968832b05cbeabc788803f65026fece4cd68809f",
		},
		{
			name:    "POST exact body",
			payload: `{"category":"linear","symbol":"BTCUSDT"}`,
			want:    "6e7af371963947cc9333774f7466f9edcd7af1575bd6994626c2636bf2077946",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Sign(timestamp, apiKey, recvWindow, tt.payload, secret); got != tt.want {
				t.Fatalf("Sign() = %q, want %q", got, tt.want)
			}
		})
	}
}
