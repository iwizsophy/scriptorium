package textdecode

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"

	"golang.org/x/text/transform"
)

func TestParseFallbackEncodingsWindowsDefault(t *testing.T) {
	got := ParseFallbackEncodings("", "windows")
	if len(got) != 1 || got[0] != "shift_jis" {
		t.Fatalf("unexpected windows default fallback list: %#v", got)
	}
}

func TestParseFallbackEncodingsAliasesAndDedupe(t *testing.T) {
	got := ParseFallbackEncodings(" cp932 ; sjis windows31j ", "linux")
	if len(got) != 1 || got[0] != "shift_jis" {
		t.Fatalf("unexpected normalized fallback list: %#v", got)
	}
}

func TestParseFallbackEncodingsBlankNonWindows(t *testing.T) {
	if got := ParseFallbackEncodings("   ", "linux"); len(got) != 0 {
		t.Fatalf("expected empty fallback list, got %#v", got)
	}
}

func TestDecodeUTF8BOM(t *testing.T) {
	result, err := Decode([]byte{0xEF, 0xBB, 0xBF, 'h', 'i'}, nil)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if result.Encoding != "utf-8" || result.Text != "hi" {
		t.Fatalf("unexpected decode result: %#v", result)
	}
}

func TestDecodeUTF16WithBOM(t *testing.T) {
	utf16le := append([]byte{0xFF, 0xFE}, encodeUTF16(binary.LittleEndian, "hello")...)
	result, err := Decode(utf16le, nil)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if result.Encoding != "utf-16le" || result.Text != "hello" {
		t.Fatalf("unexpected decode result: %#v", result)
	}
}

func TestDecodeUTF16WithoutBOM(t *testing.T) {
	utf16be := encodeUTF16(binary.BigEndian, "hello")
	result, err := Decode(utf16be, nil)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if result.Encoding != "utf-16be" || result.Text != "hello" {
		t.Fatalf("unexpected decode result: %#v", result)
	}
}

func TestDecodeShiftJISFallback(t *testing.T) {
	encoded := []byte{0x83, 0x65, 0x83, 0x58, 0x83, 0x67}
	result, err := Decode(encoded, []string{"shift_jis"})
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if result.Encoding != "shift_jis" || result.Text != "テスト" {
		t.Fatalf("unexpected decode result: %#v", result)
	}
}

func TestDecodeBigEndianBOM(t *testing.T) {
	utf16be := append([]byte{0xFE, 0xFF}, encodeUTF16(binary.BigEndian, "hello")...)
	result, err := Decode(utf16be, nil)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if result.Encoding != "utf-16be" || result.Text != "hello" {
		t.Fatalf("unexpected decode result: %#v", result)
	}
}

func TestDecodeFallbackContinuesUntilSupportedEncoding(t *testing.T) {
	encoded := []byte{0x83, 0x65, 0x83, 0x58, 0x83, 0x67}
	result, err := Decode(encoded, []string{"unsupported", "shift_jis"})
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if result.Encoding != "shift_jis" || result.Text != "テスト" {
		t.Fatalf("unexpected decode result: %#v", result)
	}
}

func TestDecodeRejectsBinary(t *testing.T) {
	_, err := Decode([]byte{0x00, 0x01, 0x00, 0x02, 0x00}, nil)
	if !errors.Is(err, ErrBinary) {
		t.Fatalf("expected ErrBinary, got %v", err)
	}

	_, err = Decode([]byte{0xff, 0x00, 0xff}, nil)
	if !errors.Is(err, ErrBinary) {
		t.Fatalf("expected ErrBinary from invalid non-UTF payload with NUL, got %v", err)
	}
}

func TestDecodeRejectsUnsupportedEncoding(t *testing.T) {
	_, err := Decode([]byte{0x81}, nil)
	if !errors.Is(err, ErrUnsupportedEncoding) {
		t.Fatalf("expected ErrUnsupportedEncoding, got %v", err)
	}
}

func TestDecodeRejectsTooManyControls(t *testing.T) {
	_, err := Decode([]byte("abc\x01\x02def"), nil)
	if !errors.Is(err, ErrUnsupportedEncoding) {
		t.Fatalf("expected ErrUnsupportedEncoding, got %v", err)
	}
}

func TestDecodeHelperBranches(t *testing.T) {
	if _, err := decodeUTF8([]byte{0x00, 0xff}, "utf-8"); !errors.Is(err, ErrBinary) {
		t.Fatalf("expected ErrBinary from invalid UTF-8 with NUL, got %v", err)
	}
	if _, err := decodeUTF8([]byte{0xff}, "utf-8"); !errors.Is(err, ErrUnsupportedEncoding) {
		t.Fatalf("expected ErrUnsupportedEncoding from invalid UTF-8, got %v", err)
	}

	if _, err := decodeUTF16([]byte{0x00}, binary.LittleEndian, "utf-16le"); !errors.Is(err, ErrBinary) {
		t.Fatalf("expected ErrBinary from odd UTF-16 bytes with NUL, got %v", err)
	}
	if _, err := decodeUTF16([]byte{0x41}, binary.LittleEndian, "utf-16le"); !errors.Is(err, ErrUnsupportedEncoding) {
		t.Fatalf("expected ErrUnsupportedEncoding from odd UTF-16 bytes, got %v", err)
	}

	if _, err := decodeFallback([]byte("abc"), "unsupported"); err == nil {
		t.Fatal("expected unsupported fallback encoding error")
	}
	if _, err := decodeWithTransformer([]byte{0xff}, transform.Nop); !errors.Is(err, ErrUnsupportedEncoding) {
		t.Fatalf("expected ErrUnsupportedEncoding from invalid transformed UTF-8, got %v", err)
	}

	if _, err := validateDecodedText("abc\x00", "utf-8"); !errors.Is(err, ErrBinary) {
		t.Fatalf("expected ErrBinary from embedded NUL, got %v", err)
	}
	if _, err := validateDecodedText("ab\x01\x02cdef", "utf-8"); !errors.Is(err, ErrUnsupportedEncoding) {
		t.Fatalf("expected ErrUnsupportedEncoding from control-heavy text, got %v", err)
	}
	if result, err := validateDecodedText("line1\nline2\tok", "utf-8"); err != nil || result.Encoding != "utf-8" {
		t.Fatalf("expected allowed control characters, got result=%#v err=%v", result, err)
	}

	if normalizeEncodingName(" utf8 ") != "utf-8" || normalizeEncodingName("windows31j") != "shift_jis" || normalizeEncodingName("latin1") != "latin1" {
		t.Fatalf("unexpected normalized encoding names")
	}
	if hasTooManyDisallowedControls("a\x01b") {
		t.Fatal("expected below-threshold controls to be allowed")
	}
	if hasTooManyDisallowedControls("") {
		t.Fatal("expected empty text to be allowed")
	}

	if _, err := Decode([]byte{0x41, 0x00, 0x42}, nil); err == nil {
		t.Fatal("expected odd-length BOM-less payload to be rejected")
	}
	if result, err := Decode([]byte("plain ascii"), nil); err != nil || result.Encoding != "utf-8" || result.Text != "plain ascii" {
		t.Fatalf("expected ASCII payload to remain utf-8 text, got result=%#v err=%v", result, err)
	}

	littleEndianText := strings.Repeat("a", 80)
	if result, err := Decode(encodeUTF16(binary.LittleEndian, littleEndianText), nil); err != nil || result.Text != littleEndianText || !strings.HasPrefix(result.Encoding, "utf-16") {
		t.Fatalf("expected BOM-less little-endian UTF-16 text to decode, got result=%#v err=%v", result, err)
	}
	bigEndianText := strings.Repeat("b", 80)
	if result, err := Decode(encodeUTF16(binary.BigEndian, bigEndianText), nil); err != nil || result.Text != bigEndianText || !strings.HasPrefix(result.Encoding, "utf-16") {
		t.Fatalf("expected BOM-less big-endian UTF-16 text to decode, got result=%#v err=%v", result, err)
	}
}

func TestDecodeWithTransformerReturnsReaderError(t *testing.T) {
	if _, err := decodeWithTransformer([]byte("boom"), failingTransformer{}); err == nil || !strings.Contains(err.Error(), "forced transform failure") {
		t.Fatalf("expected transformer read error, got %v", err)
	}
}

func encodeUTF16(order binary.ByteOrder, text string) []byte {
	runes := utf16.Encode([]rune(text))
	buf := bytes.NewBuffer(make([]byte, 0, len(runes)*2))
	for _, unit := range runes {
		chunk := make([]byte, 2)
		order.PutUint16(chunk, unit)
		buf.Write(chunk)
	}
	return buf.Bytes()
}

type failingTransformer struct{}

func (failingTransformer) Reset() {}

func (failingTransformer) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
	return 0, 0, fmt.Errorf("forced transform failure")
}
