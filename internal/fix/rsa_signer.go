package fix

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

type AuthSigner interface{ Sign([]byte) (string, error) }

type RSASigner struct{ key *rsa.PrivateKey }

func LoadRSASigner(path string) (*RSASigner, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read FIX private key: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("FIX private key is not PEM")
	}
	var key *rsa.PrivateKey
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("FIX private key is not RSA")
		}
	} else {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, errors.New("FIX private key is neither valid PKCS#8 nor PKCS#1 RSA")
		}
	}
	if key.N.BitLen() != 2048 && key.N.BitLen() != 4096 {
		return nil, fmt.Errorf("FIX RSA key must be 2048 or 4096 bits, got %d", key.N.BitLen())
	}
	return &RSASigner{key: key}, nil
}

func (s *RSASigner) Sign(payload []byte) (string, error) {
	if s == nil || s.key == nil {
		return "", errors.New("RSA signer has no private key")
	}
	digest := sha256.Sum256(payload)
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign FIX authentication: %w", err)
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}
