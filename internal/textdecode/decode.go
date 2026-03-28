package textdecode

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

var (
	ErrBinary              = errors.New("binary file")
	ErrUnsupportedEncoding = errors.New("unsupported text encoding")
)

type Result struct {
	Text     string
	Encoding string
}

func ParseFallbackEncodings(value, goos string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})

	result := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		name := normalizeEncodingName(field)
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}

	if strings.TrimSpace(value) == "" && strings.EqualFold(goos, "windows") {
		return []string{"shift_jis"}
	}
	return result
}

func Decode(data []byte, fallbackEncodings []string) (Result, error) {
	if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		return decodeUTF8(data[3:], "utf-8")
	}
	if bytes.HasPrefix(data, []byte{0xFF, 0xFE}) {
		return decodeUTF16(data[2:], binary.LittleEndian, "utf-16le")
	}
	if bytes.HasPrefix(data, []byte{0xFE, 0xFF}) {
		return decodeUTF16(data[2:], binary.BigEndian, "utf-16be")
	}

	if order, encoding, ok := guessUTF16WithoutBOM(data); ok {
		return decodeUTF16(data, order, encoding)
	}

	if utf8.Valid(data) {
		return validateDecodedText(string(data), "utf-8")
	}

	for _, fallback := range fallbackEncodings {
		text, err := decodeFallback(data, fallback)
		if err != nil {
			continue
		}
		return validateDecodedText(text, fallback)
	}

	if bytes.IndexByte(data, 0x00) >= 0 {
		return Result{}, ErrBinary
	}
	return Result{}, ErrUnsupportedEncoding
}

func decodeUTF8(data []byte, encoding string) (Result, error) {
	if !utf8.Valid(data) {
		if bytes.IndexByte(data, 0x00) >= 0 {
			return Result{}, ErrBinary
		}
		return Result{}, ErrUnsupportedEncoding
	}
	return validateDecodedText(string(data), encoding)
}

func decodeUTF16(data []byte, order binary.ByteOrder, encoding string) (Result, error) {
	if len(data)%2 != 0 {
		if bytes.IndexByte(data, 0x00) >= 0 {
			return Result{}, ErrBinary
		}
		return Result{}, ErrUnsupportedEncoding
	}

	units := make([]uint16, len(data)/2)
	for idx := range units {
		units[idx] = order.Uint16(data[idx*2 : idx*2+2])
	}

	return validateDecodedText(string(utf16.Decode(units)), encoding)
}

func decodeFallback(data []byte, name string) (string, error) {
	switch normalizeEncodingName(name) {
	case "shift_jis":
		return decodeWithTransformer(data, japanese.ShiftJIS.NewDecoder())
	default:
		return "", fmt.Errorf("unsupported fallback encoding: %s", name)
	}
}

func decodeWithTransformer(data []byte, transformer transform.Transformer) (string, error) {
	reader := transform.NewReader(bytes.NewReader(data), transformer)
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(decoded) {
		return "", ErrUnsupportedEncoding
	}
	return string(decoded), nil
}

func validateDecodedText(text, encoding string) (Result, error) {
	if strings.ContainsRune(text, rune(0)) {
		return Result{}, ErrBinary
	}
	if hasTooManyDisallowedControls(text) {
		return Result{}, ErrUnsupportedEncoding
	}
	return Result{
		Text:     text,
		Encoding: encoding,
	}, nil
}

func normalizeEncodingName(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.ReplaceAll(normalized, "-", "_")

	switch normalized {
	case "", "utf8", "utf_8":
		return "utf-8"
	case "cp932", "ms932", "windows31j", "windows_31j", "sjis", "shiftjis", "shift_jis":
		return "shift_jis"
	default:
		return normalized
	}
}

// The spec requires rejecting text with "too many" control characters but
// does not define a numeric threshold. This implementation treats text as
// invalid when at least two disallowed control characters exceed 5% of runes.
func hasTooManyDisallowedControls(text string) bool {
	total := 0
	controls := 0

	for _, r := range text {
		total++
		if r < 0x20 || r == 0x7F {
			switch r {
			case '\n', '\r', '\t':
				continue
			default:
				controls++
			}
		}
	}

	if total == 0 {
		return false
	}
	return controls >= 2 && float64(controls)/float64(total) > 0.05
}

// The spec requires BOM-less UTF-16 detection but leaves the heuristic open.
// This implementation only guesses UTF-16 when zero bytes strongly favor one
// byte position, which keeps ASCII-heavy UTF-16 text detectable without making
// ordinary binary blobs look like text.
func guessUTF16WithoutBOM(data []byte) (binary.ByteOrder, string, bool) {
	if len(data) < 4 || len(data)%2 != 0 {
		return nil, "", false
	}

	sample := data
	if len(sample) > 128 {
		sample = sample[:128]
	}

	pairs := len(sample) / 2
	evenZeroes := 0
	oddZeroes := 0
	for idx := 0; idx+1 < len(sample); idx += 2 {
		if sample[idx] == 0x00 {
			evenZeroes++
		}
		if sample[idx+1] == 0x00 {
			oddZeroes++
		}
	}

	switch {
	case oddZeroes >= pairs/2 && oddZeroes >= evenZeroes*3:
		return binary.LittleEndian, "utf-16le", true
	case evenZeroes >= pairs/2 && evenZeroes >= oddZeroes*3:
		return binary.BigEndian, "utf-16be", true
	default:
		return nil, "", false
	}
}
