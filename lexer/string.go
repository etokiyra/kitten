package lexer

import (
	"fmt"
	"strings"
)

// DecodeString decodes a string literal token into the byte sequence it denotes.
//
// The input is the token's Text, including the surrounding quotation marks and
// any escape sequences. The supported escapes are \n, \t, \", and \\; any other
// escape, a missing quotation mark, or a trailing backslash is an error. The
// result is a byte sequence; multi-byte UTF-8 content passes through unchanged.
func DecodeString(literal string) (string, error) {
	if len(literal) < 2 || literal[0] != '"' || literal[len(literal)-1] != '"' {
		return "", fmt.Errorf("malformed string literal %s", literal)
	}
	return decodeStringBody(literal[1 : len(literal)-1])
}

// decodeStringBody resolves the escapes in a string literal's contents, given
// without the surrounding quotation marks.
func decodeStringBody(body string) (string, error) {
	var builder strings.Builder
	for index := 0; index < len(body); index++ {
		character := body[index]
		if character != '\\' {
			builder.WriteByte(character)
			continue
		}

		index++
		if index >= len(body) {
			return "", fmt.Errorf("string literal ends with a backslash")
		}
		switch body[index] {
		case 'n':
			builder.WriteByte('\n')
		case 't':
			builder.WriteByte('\t')
		case '"':
			builder.WriteByte('"')
		case '\\':
			builder.WriteByte('\\')
		default:
			return "", fmt.Errorf("unsupported escape sequence \\%c", body[index])
		}
	}
	return builder.String(), nil
}
