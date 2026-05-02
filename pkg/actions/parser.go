package actions

import (
	"errors"
	"fmt"
	"strings"
)

// MaxActionNameLen mirrors the schema column width for actions.name.
const MaxActionNameLen = 32

var ErrParse = errors.New("actions: parse error")

// Parse converts an APRS message body into a ParsedInvocation.
// Grammar:  @@<otp>#<action> [k=v k=v ...]
// where <otp> is empty or exactly six ASCII digits.
func Parse(body string) (*ParsedInvocation, error) {
	if !strings.HasPrefix(body, "@@") {
		return nil, fmt.Errorf("%w: missing @@ prefix", ErrParse)
	}
	rest := body[2:]
	hash := strings.IndexByte(rest, '#')
	if hash < 0 {
		return nil, fmt.Errorf("%w: missing # separator", ErrParse)
	}
	otp := rest[:hash]
	if otp != "" {
		if len(otp) != 6 {
			return nil, fmt.Errorf("%w: OTP must be exactly 6 digits", ErrParse)
		}
		for i := 0; i < len(otp); i++ {
			if otp[i] < '0' || otp[i] > '9' {
				return nil, fmt.Errorf("%w: OTP must be ASCII digits", ErrParse)
			}
		}
	}
	tail := rest[hash+1:]
	var action, argTail string
	if sp := strings.IndexByte(tail, ' '); sp >= 0 {
		action = tail[:sp]
		argTail = tail[sp+1:]
	} else {
		action = tail
	}
	if action == "" {
		return nil, fmt.Errorf("%w: empty action name", ErrParse)
	}
	if len(action) > MaxActionNameLen {
		return nil, fmt.Errorf("%w: action name exceeds %d chars", ErrParse, MaxActionNameLen)
	}
	if !validActionName(action) {
		return nil, fmt.Errorf("%w: action name contains invalid characters", ErrParse)
	}
	args, err := parseArgs(argTail)
	if err != nil {
		return nil, err
	}
	return &ParsedInvocation{OTPDigits: otp, Action: action, Args: args}, nil
}

// validActionName enforces the spec charset for action names:
// letters, digits, dot, dash, underscore. ASCII only, case-sensitive.
func validActionName(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '.' || c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}

func parseArgs(s string) ([]KeyValue, error) {
	if s == "" {
		return nil, nil
	}
	tokens := strings.Fields(s)
	out := make([]KeyValue, 0, len(tokens))
	for _, tok := range tokens {
		eq := strings.IndexByte(tok, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("%w: arg %q is not key=value", ErrParse, tok)
		}
		out = append(out, KeyValue{Key: tok[:eq], Value: tok[eq+1:]})
	}
	return out, nil
}
