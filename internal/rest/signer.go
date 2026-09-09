package rest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

func Sign(timestamp int64, apiKey string, recvWindow int64, payload, secret string) string {
	plaintext := strconv.FormatInt(timestamp, 10) + apiKey + strconv.FormatInt(recvWindow, 10) + payload
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(plaintext))
	return hex.EncodeToString(mac.Sum(nil))
}
