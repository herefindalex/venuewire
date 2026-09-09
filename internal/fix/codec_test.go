package fix

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestKnownLogonFixtureParsesAndRoundTrips(t *testing.T) {
	raw := fixture(t, "logon.fixture")
	message, err := ParseStrict(raw)
	if err != nil {
		t.Fatal(err)
	}
	if message.BeginString != "FIX.4.4" || message.BodyLength != 83 || message.CheckSum != 215 {
		t.Fatalf("header=%+v", message)
	}
	if value, ok := message.Get(35); !ok || value != "A" {
		t.Fatalf("MsgType=%q ok=%v", value, ok)
	}
	encoded, err := Encode(message.BeginString, message.Fields)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, raw) {
		t.Fatalf("round trip differs:\n got %q\nwant %q", encoded, raw)
	}
}

func TestEncoderHeaderOrderBodyLengthAndChecksum(t *testing.T) {
	encoded, err := Encode("FIX.4.4", []Field{{35, "0"}, {34, "1"}, {49, "CLIENT"}, {56, "SERVER"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(encoded, []byte("8=FIX.4.4\x019=")) {
		t.Fatalf("header order wrong: %q", encoded)
	}
	if _, err := ParseStrict(encoded); err != nil {
		t.Fatalf("self-encoded message invalid: %v", err)
	}
}

func TestParserRejectsIncorrectChecksum(t *testing.T) {
	raw := fixture(t, "logon.fixture")
	raw[len(raw)-4] = '0'
	_, err := ParseStrict(raw)
	if !errors.Is(err, ErrCheckSum) {
		t.Fatalf("error=%v", err)
	}
	if _, err := ParseLenient(raw); err != nil {
		t.Fatalf("lenient diagnostic parse rejected checksum: %v", err)
	}
}

func TestParserDetectsIncorrectBodyLength(t *testing.T) {
	raw := bytes.Replace(fixture(t, "logon.fixture"), []byte("9=83"), []byte("9=82"), 1)
	_, err := ParseStrict(raw)
	if !errors.Is(err, ErrBodyLength) {
		t.Fatalf("error=%v", err)
	}
	if _, err := ParseLenient(raw); err != nil {
		t.Fatalf("lenient diagnostic parse rejected length: %v", err)
	}
}

func TestMissingAndMalformedTagsRejected(t *testing.T) {
	tests := [][]byte{
		[]byte("8=FIX.4.4\x019=5\x0134=1\x0110=000\x01"),
		[]byte("8=FIX.4.4\x019=5\x0135=A\x01broken\x0110=000\x01"),
		[]byte("9=5\x018=FIX.4.4\x0135=A\x0110=000\x01"),
	}
	for _, raw := range tests {
		if _, err := ParseStrict(raw); err == nil {
			t.Fatalf("accepted malformed %q", raw)
		}
	}
}

func TestDuplicateAndUnknownTagsArePreserved(t *testing.T) {
	raw, err := Encode("FIX.4.4", []Field{{35, "8"}, {9999, "unknown"}, {58, "first"}, {58, "second"}})
	if err != nil {
		t.Fatal(err)
	}
	message, err := ParseStrict(raw)
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := message.Get(9999); value != "unknown" {
		t.Fatalf("unknown tag lost: %+v", message.Fields)
	}
	values := message.Values(58)
	if len(values) != 2 || values[0] != "first" || values[1] != "second" {
		t.Fatalf("duplicates=%v", values)
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fix", name))
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.TrimSpace(data)
	return bytes.ReplaceAll(data, []byte{'|'}, []byte{SOH})
}

func FuzzFIXParser(f *testing.F) {
	f.Add(fixtureForFuzz())
	f.Add([]byte("not FIX"))
	f.Fuzz(func(t *testing.T, raw []byte) { _, _ = ParseStrict(raw) })
}

func fixtureForFuzz() []byte {
	return bytes.ReplaceAll([]byte("8=FIX.4.4|9=5|35=0|10=161|"), []byte{'|'}, []byte{SOH})
}
