package parser

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode"
)

// ParseHeaderFrame extracts the human-readable ASCII body from a C37.118 Header frame.
// Layout: SYNC(2) | FRAMESIZE(2) | IDCODE(2) | SOC(4) | FRACSEC(4) | DATA… | CHK(2)
func ParseHeaderFrame(raw []byte) (text string, err error) {
	if len(raw) < 16 {
		return "", fmt.Errorf("header frame too short: %d", len(raw))
	}
	frameSize := int(binary.BigEndian.Uint16(raw[2:4]))
	if frameSize != len(raw) {
		return "", fmt.Errorf("header frame size mismatch: header=%d actual=%d", frameSize, len(raw))
	}
	syncType := raw[1] & 0x70
	if syncType != 0x10 {
		return "", fmt.Errorf("not a header frame: sync type=0x%02X", syncType)
	}
	body := raw[14 : len(raw)-2]
	return sanitizeHeaderText(body), nil
}

func sanitizeHeaderText(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		switch {
		case c == 0:
			// stop at first NUL (common padding)
			return strings.TrimSpace(sb.String())
		case c == '\n' || c == '\r' || c == '\t':
			sb.WriteByte(c)
		case c >= 32 && c < 127:
			sb.WriteByte(c)
		case unicode.IsPrint(rune(c)):
			sb.WriteByte(c)
		default:
			// skip non-printable binary
		}
	}
	return strings.TrimSpace(sb.String())
}
