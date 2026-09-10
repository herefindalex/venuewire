package deribitfix

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"

	bybitfix "venuewire/internal/fix"
)

func TestLogonAuthenticationVectorAndExplicitPolicies(t *testing.T) {
	nonce := make([]byte, 64)
	for i := range nonce {
		nonce[i] = byte(i)
	}
	fixed := time.UnixMilli(1788960000123)
	auth := &Authenticator{ClientID: "client", ClientSecret: "secret", SenderCompID: "sender", HeartbeatSeconds: 10, CancelOnDisconnect: false, ReportFillsAsExecReports: true, Now: func() time.Time { return fixed }, Random: bytes.NewReader(nonce)}
	raw, err := auth.Logon(1)
	if err != nil {
		t.Fatal(err)
	}
	message, err := bybitfix.ParseStrict(raw)
	if err != nil {
		t.Fatal(err)
	}
	rawData, _ := message.Get(96)
	wantRaw := strconv.FormatInt(fixed.UnixMilli(), 10) + "." + base64.StdEncoding.EncodeToString(nonce[:32])
	if rawData != wantRaw {
		t.Fatalf("raw data mismatch")
	}
	digest := sha256.Sum256([]byte(wantRaw + "secret"))
	wantPassword := base64.StdEncoding.EncodeToString(digest[:])
	password, _ := message.Get(554)
	if password != wantPassword {
		t.Fatal("password digest mismatch")
	}
	for tag, want := range map[int]string{35: "A", 49: "sender", 56: "DERIBITSERVER", 34: "1", 108: "10", 9001: "N", 9015: "Y"} {
		if got, _ := message.Get(tag); got != want {
			t.Fatalf("tag %d=%q want %q", tag, got, want)
		}
	}
	second, err := auth.Logon(2)
	if err != nil {
		t.Fatal(err)
	}
	secondMessage, _ := bybitfix.ParseStrict(second)
	secondRaw, _ := secondMessage.Get(96)
	if secondRaw[:13] == rawData[:13] {
		t.Fatal("timestamp did not increase in same millisecond")
	}
}

func TestLogonRejectsMissingConfigurationAndRandomFailure(t *testing.T) {
	if _, err := (&Authenticator{}).Logon(1); err == nil {
		t.Fatal("missing config accepted")
	}
	auth := &Authenticator{ClientID: "c", ClientSecret: "s", SenderCompID: "x", HeartbeatSeconds: 10, Random: bytes.NewReader(nil)}
	if _, err := auth.Logon(1); err == nil {
		t.Fatal("short random source accepted")
	}
}

func TestLogonSurvivesClockRollbackAndChangesDigestWithSecret(t *testing.T) {
	now := time.UnixMilli(2000)
	auth := &Authenticator{
		ClientID:         "client",
		ClientSecret:     "secret-a",
		SenderCompID:     "sender",
		HeartbeatSeconds: 10,
		Now:              func() time.Time { return now },
		Random:           bytes.NewReader(bytes.Repeat([]byte{7}, 64)),
	}
	firstRaw, err := auth.Logon(1)
	if err != nil {
		t.Fatal(err)
	}
	now = time.UnixMilli(1000)
	secondRaw, err := auth.Logon(2)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := bybitfix.ParseStrict(firstRaw)
	second, _ := bybitfix.ParseStrict(secondRaw)
	firstAuth, _ := first.Get(96)
	secondAuth, _ := second.Get(96)
	firstTimestamp, _ := strconv.ParseInt(strings.Split(firstAuth, ".")[0], 10, 64)
	secondTimestamp, _ := strconv.ParseInt(strings.Split(secondAuth, ".")[0], 10, 64)
	if secondTimestamp != firstTimestamp+1 {
		t.Fatalf("rollback timestamp=%d, want %d", secondTimestamp, firstTimestamp+1)
	}

	other := &Authenticator{
		ClientID:         "client",
		ClientSecret:     "secret-b",
		SenderCompID:     "sender",
		HeartbeatSeconds: 10,
		Now:              func() time.Time { return time.UnixMilli(2000) },
		Random:           bytes.NewReader(bytes.Repeat([]byte{7}, 32)),
	}
	otherRaw, err := other.Logon(1)
	if err != nil {
		t.Fatal(err)
	}
	otherMessage, _ := bybitfix.ParseStrict(otherRaw)
	firstPassword, _ := first.Get(554)
	otherPassword, _ := otherMessage.Get(554)
	if firstPassword == otherPassword {
		t.Fatal("changing client secret did not change password digest")
	}
}
