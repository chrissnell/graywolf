package actions

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSanitizeFreeformAcceptsValidPayload(t *testing.T) {
	schema := []ArgSpec{{Key: FreeformArgKey, Regex: `^[\x20-\x7E]+$`, MaxLen: 100}}
	clean, err := SanitizeFreeform(schema, "+15555551212 hello world", FreeformValueCeiling)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := []KeyValue{{Key: FreeformArgKey, Value: "+15555551212 hello world"}}
	if !reflect.DeepEqual(clean, want) {
		t.Fatalf("got %+v, want %+v", clean, want)
	}
}

func TestSanitizeFreeformRejectsControlChars(t *testing.T) {
	// Tab, NUL, CR, LF, ESC must all be rejected even with `.*` regex —
	// defense in depth against log injection and terminal-escape attacks
	// via APRS messages.
	schema := []ArgSpec{{Key: FreeformArgKey, Regex: `.*`, MaxLen: 200}}
	for _, bad := range []string{"hello\tworld", "ab\x00cd", "line1\rline2", "line1\nline2", "esc\x1b[31mred"} {
		t.Run(bad, func(t *testing.T) {
			_, err := SanitizeFreeform(schema, bad, FreeformValueCeiling)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "control") {
				t.Fatalf("err = %v, want to mention control", err)
			}
		})
	}
}

func TestSanitizeFreeformAppliesCeiling(t *testing.T) {
	// MaxLen left at 0; ceiling enforces.
	schema := []ArgSpec{{Key: FreeformArgKey, Regex: `.*`, MaxLen: 0}}
	value := strings.Repeat("a", FreeformValueCeiling+1)
	_, err := SanitizeFreeform(schema, value, FreeformValueCeiling)
	if err == nil {
		t.Fatal("expected too-long error")
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Fatalf("err = %v, want too long", err)
	}
}

func TestSanitizeFreeformRespectsOperatorMaxLen(t *testing.T) {
	schema := []ArgSpec{{Key: FreeformArgKey, Regex: `.*`, MaxLen: 32}}
	if _, err := SanitizeFreeform(schema, strings.Repeat("a", 33), FreeformValueCeiling); err == nil {
		t.Fatal("expected too-long error")
	}
}

func TestSanitizeFreeformOperatorMaxLenCappedByCeiling(t *testing.T) {
	// Operator MaxLen wider than ceiling cannot widen past it.
	schema := []ArgSpec{{Key: FreeformArgKey, Regex: `.*`, MaxLen: FreeformValueCeiling + 50}}
	if _, err := SanitizeFreeform(schema, strings.Repeat("a", FreeformValueCeiling+10), FreeformValueCeiling); err == nil {
		t.Fatal("expected ceiling to cap operator MaxLen")
	}
}

func TestSanitizeFreeformRejectsBadRegex(t *testing.T) {
	schema := []ArgSpec{{Key: FreeformArgKey, Regex: `^[A-Z]+$`, MaxLen: 100}}
	_, err := SanitizeFreeform(schema, "lowercase", FreeformValueCeiling)
	if err == nil {
		t.Fatal("expected regex error")
	}
	var bae *BadArgError
	if !errors.As(err, &bae) || bae.Reason != "regex" {
		t.Fatalf("expected BadArgError reason=regex, got %v", err)
	}
}

func TestSanitizeFreeformRequiresExactlyOneSpec(t *testing.T) {
	if _, err := SanitizeFreeform(nil, "x", FreeformValueCeiling); err == nil {
		t.Fatal("expected error for nil schema")
	}
	twoSpecs := []ArgSpec{{Key: "a"}, {Key: "b"}}
	if _, err := SanitizeFreeform(twoSpecs, "x", FreeformValueCeiling); err == nil {
		t.Fatal("expected error for two specs")
	}
}

func TestSanitizeFreeformEmptyValueWhenRequired(t *testing.T) {
	schema := []ArgSpec{{Key: FreeformArgKey, Regex: `.+`, MaxLen: 100, Required: true}}
	_, err := SanitizeFreeform(schema, "", FreeformValueCeiling)
	if err == nil {
		t.Fatal("expected missing-arg error")
	}
}

func TestSanitizeFreeformEmptyValueWhenOptional(t *testing.T) {
	schema := []ArgSpec{{Key: FreeformArgKey, Regex: `.*`, MaxLen: 100}}
	clean, err := SanitizeFreeform(schema, "", FreeformValueCeiling)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if clean != nil {
		t.Fatalf("expected nil result for empty optional, got %+v", clean)
	}
}
