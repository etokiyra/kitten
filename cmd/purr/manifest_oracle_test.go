package main

// Independent Go oracle for the purr.toml subset. It is deliberately written
// from the grammar in the phase instructions, not by translating the Kitten
// parser, and is used to check pinned and randomized cases.

import (
	"fmt"
	"strings"
)

type manifestOracle struct {
	text       []byte
	size       int
	index      int
	line       int
	column     int
	hasPackage bool
	hasName    bool
	hasVersion bool
	name       string
	version    string
	failed     bool
	error      string
}

func newManifestOracle(text []byte) *manifestOracle {
	return &manifestOracle{text: text, size: len(text), line: 1, column: 1}
}

func (p *manifestOracle) atEnd() bool { return p.index >= p.size }

func (p *manifestOracle) peek() int {
	if p.atEnd() {
		return -1
	}
	return int(p.text[p.index])
}

func (p *manifestOracle) peekNext() int {
	if p.index+1 >= p.size {
		return -1
	}
	return int(p.text[p.index+1])
}

func (p *manifestOracle) advance() int {
	value := int(p.text[p.index])
	p.index++
	if value == 10 {
		p.line++
		p.column = 1
	} else {
		p.column++
	}
	return value
}

func (p *manifestOracle) failAt(line, column int, message string) {
	if !p.failed {
		p.failed = true
		p.error = fmt.Sprintf("purr.toml:%d:%d: %s", line, column, message)
	}
}

func (p *manifestOracle) failHere(message string) {
	p.failAt(p.line, p.column, message)
}

func isOracleSpace(value int) bool { return value == 32 || value == 9 }
func isOracleDigit(value int) bool { return value >= 48 && value <= 57 }
func isOracleLower(value int) bool { return value >= 97 && value <= 122 }
func isOracleLetter(value int) bool {
	return (value >= 97 && value <= 122) || (value >= 65 && value <= 90)
}
func isOracleKeyByte(value int) bool {
	return isOracleLetter(value) || isOracleDigit(value) ||
		value == 95 || value == 45 || value == 46
}

func oracleEcho(value []byte) string {
	var builder strings.Builder
	for _, character := range value {
		if character < 32 || character >= 127 {
			fmt.Fprintf(&builder, "\\x%02x", character)
		} else {
			builder.WriteByte(character)
		}
	}
	return builder.String()
}

func (p *manifestOracle) skipSpace() {
	for !p.atEnd() && isOracleSpace(p.peek()) {
		p.advance()
	}
}

func (p *manifestOracle) consumeComment() {
	if !p.atEnd() && p.peek() == 35 {
		p.advance()
	}
	done := false
	for !done && !p.atEnd() {
		value := p.peek()
		if value == 10 {
			p.advance()
			done = true
		} else if value == 13 {
			if p.peekNext() == 10 {
				p.advance()
				p.advance()
			} else {
				p.failHere("stray carriage return")
			}
			done = true
		} else {
			p.advance()
		}
	}
}

func (p *manifestOracle) consumeBlankOrComment() bool {
	p.skipSpace()
	if p.atEnd() {
		return true
	}
	value := p.peek()
	if value == 10 {
		p.advance()
		return true
	}
	if value == 13 {
		if p.peekNext() == 10 {
			p.advance()
			p.advance()
			return true
		}
		p.failHere("stray carriage return")
		return true
	}
	if value == 35 {
		p.consumeComment()
		return true
	}
	return false
}

func (p *manifestOracle) finishLine() bool {
	p.skipSpace()
	if p.atEnd() {
		return true
	}
	value := p.peek()
	if value == 35 {
		p.consumeComment()
		return !p.failed
	}
	if value == 10 {
		p.advance()
		return true
	}
	if value == 13 {
		if p.peekNext() == 10 {
			p.advance()
			p.advance()
			return true
		}
		p.failHere("stray carriage return")
		return false
	}
	p.failHere("unexpected text after value")
	return false
}

func (p *manifestOracle) startsWith(word string) bool {
	if p.index+len(word) > p.size {
		return false
	}
	return string(p.text[p.index:p.index+len(word)]) == word
}

func (p *manifestOracle) hasKeyContinuation(size int) bool {
	after := p.index + size
	return after < p.size && isOracleKeyByte(int(p.text[after]))
}

func (p *manifestOracle) scanKeyToken() string {
	start := p.index
	for !p.atEnd() && isOracleKeyByte(p.peek()) {
		p.advance()
	}
	return string(p.text[start:p.index])
}

func (p *manifestOracle) parseHeader(packageSeen bool) {
	startLine, startColumn := p.line, p.column
	startIndex := p.index
	p.advance()
	depth := 1
	scanning := true
	for scanning && !p.atEnd() {
		value := p.peek()
		if value == 10 || value == 13 {
			scanning = false
		} else if value == 91 {
			depth++
			p.advance()
		} else if value == 93 {
			depth--
			p.advance()
			if depth == 0 {
				scanning = false
			}
		} else {
			p.advance()
		}
	}
	if depth != 0 {
		raw := string(p.text[startIndex:p.index])
		p.failAt(startLine, startColumn, "unexpected table "+oracleEcho([]byte(raw)))
		return
	}
	raw := string(p.text[startIndex:p.index])
	if raw != "[package]" {
		p.failAt(startLine, startColumn, "unexpected table "+oracleEcho([]byte(raw)))
		return
	}
	if packageSeen {
		p.failAt(startLine, startColumn, "duplicate [package] table")
		return
	}
	p.hasPackage = true
	p.finishLine()
}

func (p *manifestOracle) parseString() (string, int, int, bool) {
	openLine, openColumn := p.line, p.column
	p.advance() // opening quote
	var value strings.Builder
	for !p.atEnd() {
		current := p.peek()
		if current == 10 {
			p.failAt(openLine, openColumn, "unterminated string")
			return "", 0, 0, false
		}
		if current == 13 {
			if p.peekNext() == 10 {
				p.failAt(openLine, openColumn, "unterminated string")
			} else {
				p.failHere("stray carriage return")
			}
			return "", 0, 0, false
		}
		if current == 34 {
			p.advance()
			return value.String(), openLine, openColumn, true
		}
		if current == 92 {
			p.advance()
			if p.atEnd() {
				p.failAt(openLine, openColumn, "unterminated string")
				return "", 0, 0, false
			}
			escapeLine, escapeColumn := p.line, p.column
			escaped := p.peek()
			switch escaped {
			case 92:
				value.WriteByte(92)
				p.advance()
			case 34:
				value.WriteByte(34)
				p.advance()
			case 110:
				value.WriteByte(10)
				p.advance()
			case 116:
				value.WriteByte(9)
				p.advance()
			case 114:
				value.WriteByte(13)
				p.advance()
			default:
				p.failAt(escapeLine, escapeColumn,
					"invalid escape \\"+oracleEcho([]byte{byte(escaped)}))
				return "", 0, 0, false
			}
		} else {
			value.WriteByte(byte(current))
			p.advance()
		}
	}
	p.failAt(openLine, openColumn, "unterminated string")
	return "", 0, 0, false
}

func (p *manifestOracle) validateName(value string, line, column int) bool {
	if len(value) == 0 {
		p.failAt(line, column, "invalid package name "+oracleEcho([]byte(value))+": must not be empty")
		return false
	}
	if len(value) > 64 {
		p.failAt(line, column, "invalid package name "+oracleEcho([]byte(value))+": must be at most 64 bytes")
		return false
	}
	if !isOracleLower(int(value[0])) {
		p.failAt(line, column, "invalid package name "+oracleEcho([]byte(value))+": must start with a lowercase letter")
		return false
	}
	for index := 1; index < len(value); index++ {
		character := int(value[index])
		if !(isOracleLower(character) || isOracleDigit(character) || character == 95 || character == 45) {
			p.failAt(line, column, "invalid package name "+oracleEcho([]byte(value))+
				": may contain only lowercase letters, digits, _ and -")
			return false
		}
	}
	last := int(value[len(value)-1])
	if last == 45 || last == 95 {
		p.failAt(line, column, "invalid package name "+oracleEcho([]byte(value))+": must not end with - or _")
		return false
	}
	return true
}

func (p *manifestOracle) validateVersion(value string, line, column int) bool {
	parts := 1
	partLength := 0
	emptyPart := false
	for index := 0; index < len(value); index++ {
		if value[index] == 46 {
			if partLength == 0 {
				emptyPart = true
			}
			parts++
			partLength = 0
		} else {
			partLength++
		}
	}
	if partLength == 0 {
		emptyPart = true
	}
	if parts != 3 || emptyPart {
		p.failAt(line, column, "invalid version "+oracleEcho([]byte(value))+": expected MAJOR.MINOR.PATCH")
		return false
	}
	start := 0
	for part := 0; part < 3; part++ {
		end := start
		for end < len(value) && value[end] != 46 {
			end++
		}
		length := end - start
		if length < 1 || length > 9 {
			p.failAt(line, column, "invalid version "+oracleEcho([]byte(value))+
				": a version part must have 1 to 9 digits")
			return false
		}
		for index := start; index < end; index++ {
			if !isOracleDigit(int(value[index])) {
				p.failAt(line, column, "invalid version "+oracleEcho([]byte(value))+
					": a version part must have 1 to 9 digits")
				return false
			}
		}
		if length > 1 && value[start] == 48 {
			p.failAt(line, column, "invalid version "+oracleEcho([]byte(value))+
				": a version part must not have a leading zero")
			return false
		}
		start = end + 1
	}
	return true
}

func (p *manifestOracle) parseEntry() {
	startLine, startColumn := p.line, p.column
	isName, isVersion := false, false
	if p.startsWith("name") && !p.hasKeyContinuation(4) {
		isName = true
		for count := 0; count < 4; count++ {
			p.advance()
		}
	} else if p.startsWith("version") && !p.hasKeyContinuation(7) {
		isVersion = true
		for count := 0; count < 7; count++ {
			p.advance()
		}
	} else if p.peek() >= 0 && isOracleLetter(p.peek()) {
		token := p.scanKeyToken()
		p.failAt(startLine, startColumn, "unknown key "+oracleEcho([]byte(token)))
		return
	} else {
		p.failAt(startLine, startColumn, "expected key name or version")
		return
	}
	key := "name"
	if isVersion {
		key = "version"
	}
	if isName && p.hasName {
		p.failAt(startLine, startColumn, "duplicate key name")
		return
	}
	if isVersion && p.hasVersion {
		p.failAt(startLine, startColumn, "duplicate key version")
		return
	}
	p.skipSpace()
	if p.peek() != 61 {
		if p.peek() == 13 && p.peekNext() != 10 {
			p.failHere("stray carriage return")
		} else {
			p.failHere("expected = after key " + key)
		}
		return
	}
	p.advance()
	p.skipSpace()
	if p.peek() != 34 {
		if p.peek() == 13 && p.peekNext() != 10 {
			p.failHere("stray carriage return")
		} else {
			p.failHere("expected a string value for " + key)
		}
		return
	}
	value, valueLine, valueColumn, ok := p.parseString()
	if !ok {
		return
	}
	if !p.finishLine() {
		return
	}
	if isName {
		if !p.validateName(value, valueLine, valueColumn) {
			return
		}
		p.name = value
		p.hasName = true
	} else {
		if !p.validateVersion(value, valueLine, valueColumn) {
			return
		}
		p.version = value
		p.hasVersion = true
	}
}

func oracleParseManifest(text []byte) (string, string, string) {
	p := newManifestOracle(text)
	for !p.failed && !p.hasPackage {
		if p.atEnd() {
			p.failHere("expected [package]")
		} else {
			startIndex, startLine, startColumn := p.index, p.line, p.column
			if p.consumeBlankOrComment() {
			} else if p.index != startIndex {
				p.failAt(startLine, startColumn, "expected [package]")
			} else if p.peek() == 91 {
				p.parseHeader(false)
			} else {
				p.failAt(startLine, startColumn, "expected [package]")
			}
		}
	}
	for !p.failed && !p.atEnd() {
		startIndex, startLine, startColumn := p.index, p.line, p.column
		if p.consumeBlankOrComment() {
		} else if p.index != startIndex {
			p.failAt(startLine, startColumn, "expected key name or version")
		} else if p.peek() == 91 {
			p.parseHeader(true)
		} else {
			p.parseEntry()
		}
	}
	if !p.failed {
		if !p.hasPackage {
			p.failHere("expected [package]")
		} else if !p.hasName {
			p.failHere("missing key name")
		} else if !p.hasVersion {
			p.failHere("missing key version")
		}
	}
	if p.failed {
		return "", "", p.error
	}
	return p.name, p.version, ""
}
