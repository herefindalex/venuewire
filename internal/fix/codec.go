package fix

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
)

const SOH byte = 0x01

var (
	ErrBodyLength = errors.New("invalid FIX BodyLength")
	ErrCheckSum   = errors.New("invalid FIX CheckSum")
)

type Field struct {
	Tag   int
	Value string
}

type Message struct {
	BeginString string
	BodyLength  int
	CheckSum    int
	Fields      []Field
	Raw         []byte
}

// Get returns the first occurrence. Values preserves all occurrences in wire
// order, which makes duplicate tags explicit and supports repeating groups.
func (m Message) Get(tag int) (string, bool) {
	for _, field := range m.Fields {
		if field.Tag == tag {
			return field.Value, true
		}
	}
	return "", false
}

func (m Message) Values(tag int) []string {
	var values []string
	for _, field := range m.Fields {
		if field.Tag == tag {
			values = append(values, field.Value)
		}
	}
	return values
}

func Encode(beginString string, fields []Field) ([]byte, error) {
	if beginString == "" {
		beginString = "FIX.4.4"
	}
	if len(fields) == 0 || fields[0].Tag != 35 {
		return nil, errors.New("first FIX body field must be MsgType(35)")
	}
	var body bytes.Buffer
	for _, field := range fields {
		if field.Tag <= 0 || field.Tag == 8 || field.Tag == 9 || field.Tag == 10 {
			return nil, fmt.Errorf("invalid body tag %d", field.Tag)
		}
		if bytes.IndexByte([]byte(field.Value), SOH) >= 0 {
			return nil, fmt.Errorf("tag %d contains SOH", field.Tag)
		}
		fmt.Fprintf(&body, "%d=%s%c", field.Tag, field.Value, SOH)
	}
	var message bytes.Buffer
	fmt.Fprintf(&message, "8=%s%c9=%d%c", beginString, SOH, body.Len(), SOH)
	message.Write(body.Bytes())
	checksum := CheckSum(message.Bytes())
	fmt.Fprintf(&message, "10=%03d%c", checksum, SOH)
	return message.Bytes(), nil
}

func CheckSum(data []byte) int {
	var sum uint64
	for _, value := range data {
		sum += uint64(value)
	}
	return int(sum % 256)
}

type ParseOptions struct {
	ValidateBodyLength bool
	ValidateCheckSum   bool
}

func ParseStrict(raw []byte) (Message, error) {
	return Parse(raw, ParseOptions{ValidateBodyLength: true, ValidateCheckSum: true})
}

func ParseLenient(raw []byte) (Message, error) { return Parse(raw, ParseOptions{}) }

func Parse(raw []byte, options ParseOptions) (Message, error) {
	message := Message{Raw: append([]byte(nil), raw...)}
	if len(raw) == 0 || raw[len(raw)-1] != SOH {
		return message, errors.New("FIX message must end with SOH")
	}
	parts := bytes.Split(raw[:len(raw)-1], []byte{SOH})
	if len(parts) < 4 {
		return message, errors.New("FIX message has too few fields")
	}
	parsed := make([]Field, 0, len(parts))
	for _, part := range parts {
		separator := bytes.IndexByte(part, '=')
		if separator <= 0 {
			return message, fmt.Errorf("malformed FIX field %q", part)
		}
		tag, err := strconv.Atoi(string(part[:separator]))
		if err != nil || tag <= 0 {
			return message, fmt.Errorf("invalid FIX tag %q", part[:separator])
		}
		parsed = append(parsed, Field{Tag: tag, Value: string(part[separator+1:])})
	}
	if parsed[0].Tag != 8 {
		return message, errors.New("BeginString(8) must be first")
	}
	if parsed[1].Tag != 9 {
		return message, errors.New("BodyLength(9) must be second")
	}
	if parsed[2].Tag != 35 {
		return message, errors.New("MsgType(35) must be first body field")
	}
	if parsed[len(parsed)-1].Tag != 10 {
		return message, errors.New("CheckSum(10) must be last")
	}
	message.BeginString = parsed[0].Value
	bodyLength, err := strconv.Atoi(parsed[1].Value)
	if err != nil || bodyLength < 0 {
		return message, fmt.Errorf("%w: %q", ErrBodyLength, parsed[1].Value)
	}
	message.BodyLength = bodyLength
	checksum, err := strconv.Atoi(parsed[len(parsed)-1].Value)
	if err != nil || len(parsed[len(parsed)-1].Value) != 3 || checksum < 0 || checksum > 255 {
		return message, fmt.Errorf("%w: %q", ErrCheckSum, parsed[len(parsed)-1].Value)
	}
	message.CheckSum = checksum
	message.Fields = append([]Field(nil), parsed[2:len(parsed)-1]...)
	bodyStart, checksumStart, err := frameOffsets(raw)
	if err != nil {
		return message, err
	}
	actualBodyLength := checksumStart - bodyStart
	if options.ValidateBodyLength && actualBodyLength != bodyLength {
		return message, fmt.Errorf("%w: declared=%d actual=%d", ErrBodyLength, bodyLength, actualBodyLength)
	}
	actualChecksum := CheckSum(raw[:checksumStart])
	if options.ValidateCheckSum && actualChecksum != checksum {
		return message, fmt.Errorf("%w: declared=%03d actual=%03d", ErrCheckSum, checksum, actualChecksum)
	}
	return message, nil
}

func frameOffsets(raw []byte) (bodyStart, checksumStart int, err error) {
	firstEnd := bytes.IndexByte(raw, SOH)
	if firstEnd < 0 {
		return 0, 0, errors.New("missing BeginString delimiter")
	}
	secondRelative := bytes.IndexByte(raw[firstEnd+1:], SOH)
	if secondRelative < 0 {
		return 0, 0, errors.New("missing BodyLength delimiter")
	}
	bodyStart = firstEnd + 1 + secondRelative + 1
	marker := []byte{SOH, '1', '0', '='}
	markerAt := bytes.LastIndex(raw, marker)
	if markerAt < 0 {
		return 0, 0, errors.New("missing CheckSum field")
	}
	checksumStart = markerAt + 1
	return bodyStart, checksumStart, nil
}
