package fix

import (
	"bytes"
	"testing"
)

func TestFramerHandlesEveryFragmentBoundary(t *testing.T) {
	raw := fixture(t, "logon.fixture")
	for split := 0; split <= len(raw); split++ {
		framer := &Framer{}
		first, err := framer.Feed(raw[:split])
		if err != nil {
			t.Fatalf("split %d first: %v", split, err)
		}
		second, err := framer.Feed(raw[split:])
		if err != nil {
			t.Fatalf("split %d second: %v", split, err)
		}
		frames := append(first, second...)
		if len(frames) != 1 || !bytes.Equal(frames[0], raw) {
			t.Fatalf("split %d frames=%d", split, len(frames))
		}
		if _, err := ParseStrict(frames[0]); err != nil {
			t.Fatalf("split %d parse: %v", split, err)
		}
	}
}

func TestFramerExtractsConcatenatedMessages(t *testing.T) {
	first := fixture(t, "logon.fixture")
	second, err := Encode("FIX.4.4", []Field{{35, "0"}, {34, "2"}})
	if err != nil {
		t.Fatal(err)
	}
	frames, err := (&Framer{}).Feed(append(append([]byte{}, first...), second...))
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || !bytes.Equal(frames[0], first) || !bytes.Equal(frames[1], second) {
		t.Fatalf("frames=%q", frames)
	}
}

func TestFramerSizeLimitAppliesPerFrameNotReadBatch(t *testing.T) {
	first, err := Encode("FIX.4.4", []Field{{35, "0"}, {34, "1"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encode("FIX.4.4", []Field{{35, "0"}, {34, "2"}})
	if err != nil {
		t.Fatal(err)
	}
	max := len(first) + 1
	if len(first)+len(second) <= max {
		t.Fatal("fixture does not exceed aggregate limit")
	}
	frames, err := (&Framer{MaxSize: max}).Feed(append(append([]byte{}, first...), second...))
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Fatalf("frames=%d", len(frames))
	}
}

func TestFramerRejectsMalformedAndOversizedInput(t *testing.T) {
	if _, err := (&Framer{}).Feed([]byte("garbage")); err == nil {
		t.Fatal("garbage accepted")
	}
	if _, err := (&Framer{MaxSize: 4}).Feed([]byte("8=FIX")); err == nil {
		t.Fatal("oversized buffer accepted")
	}
}
