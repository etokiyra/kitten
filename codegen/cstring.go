package codegen

import (
	"fmt"
	"strings"
)

// cStringLiteral converts a Kitten string literal token into a safe C string
// literal.
//
// The Kitten token text includes the surrounding quotation marks and the
// escapes exactly as written, so this function decodes those escapes and then
// re-encodes the resulting bytes for C. Re-encoding (rather than copying the
// source text) guarantees the generated C is well-formed: quotes, backslashes,
// control characters, question marks, and non-ASCII bytes are all escaped
// explicitly, so no source text can break out of the C literal.
func cStringLiteral(kittenLiteral string) (string, error) {
	if len(kittenLiteral) < 2 || kittenLiteral[0] != '"' || kittenLiteral[len(kittenLiteral)-1] != '"' {
		return "", fmt.Errorf("malformed string literal %s", kittenLiteral)
	}

	decoded, err := decodeKittenString(kittenLiteral[1 : len(kittenLiteral)-1])
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	builder.WriteByte('"')
	for index := 0; index < len(decoded); index++ {
		character := decoded[index]
		switch character {
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		case '\n':
			builder.WriteString(`\n`)
		case '\t':
			builder.WriteString(`\t`)
		case '\r':
			builder.WriteString(`\r`)
		case '?':
			// Escape '?' so it can never begin a trigraph.
			builder.WriteString(`\?`)
		default:
			if character >= 0x20 && character <= 0x7e {
				builder.WriteByte(character)
			} else {
				// Three-digit octal escapes are unambiguous and cannot merge
				// with following digits.
				fmt.Fprintf(&builder, `\%03o`, character)
			}
		}
	}
	builder.WriteByte('"')
	return builder.String(), nil
}

// decodeKittenString resolves the escape sequences the lexer supports: \n, \t,
// \", and \\. It operates on bytes so that multi-byte UTF-8 content passes
// through unchanged.
func decodeKittenString(body string) (string, error) {
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
