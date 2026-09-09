package fix

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
)

type Framer struct {
	buffer  []byte
	MaxSize int
}

func (f *Framer) Feed(data []byte) ([][]byte, error) {
	if f.MaxSize <= 0 {
		f.MaxSize = 1 << 20
	}
	f.buffer = append(f.buffer, data...)
	var frames [][]byte
	for {
		frame, consumed, complete, err := nextFrame(f.buffer, f.MaxSize)
		if err != nil {
			return frames, err
		}
		if !complete {
			if len(f.buffer) > f.MaxSize {
				return frames, fmt.Errorf("FIX frame exceeds %d bytes", f.MaxSize)
			}
			return frames, nil
		}
		frames = append(frames, append([]byte(nil), frame...))
		f.buffer = f.buffer[consumed:]
	}
}

func nextFrame(buffer []byte, maxSize int) ([]byte, int, bool, error) {
	if len(buffer) < 2 {
		return nil, 0, false, nil
	}
	if !bytes.HasPrefix(buffer, []byte("8=")) {
		return nil, 0, false, errors.New("FIX frame does not begin with tag 8")
	}
	firstEnd := bytes.IndexByte(buffer, SOH)
	if firstEnd < 0 {
		return nil, 0, false, nil
	}
	secondEndRelative := bytes.IndexByte(buffer[firstEnd+1:], SOH)
	if secondEndRelative < 0 {
		return nil, 0, false, nil
	}
	secondEnd := firstEnd + 1 + secondEndRelative
	bodyLengthField := buffer[firstEnd+1 : secondEnd]
	if !bytes.HasPrefix(bodyLengthField, []byte("9=")) {
		return nil, 0, false, errors.New("FIX BodyLength(9) is not second")
	}
	bodyLength, err := strconv.Atoi(string(bodyLengthField[2:]))
	if err != nil || bodyLength < 0 {
		return nil, 0, false, ErrBodyLength
	}
	bodyStart := secondEnd + 1
	checksumStart := bodyStart + bodyLength
	frameEnd := checksumStart + len("10=000") + 1
	if frameEnd > maxSize {
		return nil, 0, false, fmt.Errorf("FIX frame exceeds %d bytes", maxSize)
	}
	if len(buffer) < frameEnd {
		return nil, 0, false, nil
	}
	if !bytes.HasPrefix(buffer[checksumStart:], []byte("10=")) || buffer[frameEnd-1] != SOH {
		return nil, 0, false, ErrBodyLength
	}
	return buffer[:frameEnd], frameEnd, true, nil
}

func (f *Framer) Buffered() int { return len(f.buffer) }
