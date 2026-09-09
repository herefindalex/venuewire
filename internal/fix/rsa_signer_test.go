package fix

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestRSASignerLoadsPKCS1AndProducesVerifiableSHA256Signature(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture-key.pem")
	block := &pem.Block{Type: "RSA " + "PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := LoadRSASigner(path)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("GET/realtime1700000005000")
	encoded, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatalf("signature failed verification: %v", err)
	}
}

func TestRSASignerRejectsInvalidPEM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.pem")
	if err := os.WriteFile(path, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRSASigner(path); err == nil {
		t.Fatal("invalid key accepted")
	}
}
