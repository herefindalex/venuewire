package spotadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
)

func nonNegativeDecimal(raw string) (*big.Rat, bool) {
	value, ok := new(big.Rat).SetString(strings.TrimSpace(raw))
	return value, ok && value.Sign() >= 0
}

func positiveDecimal(raw string) (*big.Rat, bool) {
	value, ok := nonNegativeDecimal(raw)
	return value, ok && value.Sign() > 0
}

func minimumDecimal(left, right string) (string, error) {
	l, leftOK := nonNegativeDecimal(left)
	r, rightOK := nonNegativeDecimal(right)
	if !leftOK || !rightOK {
		return "", errors.New("capacity contains an invalid decimal")
	}
	if l.Cmp(r) <= 0 {
		return decimal(l), nil
	}
	return decimal(r), nil
}

func decimal(value *big.Rat) string {
	text := strings.TrimRight(strings.TrimRight(value.FloatString(18), "0"), ".")
	if text == "" || text == "-0" {
		return "0"
	}
	return text
}

func revision(values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:8])
}
