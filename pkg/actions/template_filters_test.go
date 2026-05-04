package actions

import "testing"

func TestExpandTokenFreeformBareArg(t *testing.T) {
	inv := Invocation{
		ActionName: "sms",
		SenderCall: "KE0XYZ",
		Args:       []KeyValue{{Key: FreeformArgKey, Value: "hello world"}},
	}
	got := expandToken("payload={{arg}}", inv, identityEncoder)
	if got != "payload=hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandTokenJSONFilter(t *testing.T) {
	inv := Invocation{
		Args: []KeyValue{{Key: FreeformArgKey, Value: `she said "hi"` + "\nline2"}},
	}
	got := expandToken(`{"msg":"{{arg|json}}"}`, inv, identityEncoder)
	want := `{"msg":"she said \"hi\"\nline2"}`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestExpandTokenURLFilter(t *testing.T) {
	inv := Invocation{
		Args: []KeyValue{{Key: FreeformArgKey, Value: "a b&c=d"}},
	}
	got := expandToken("https://x.test/?q={{arg|url}}", inv, identityEncoder)
	want := "https://x.test/?q=a+b%26c%3Dd"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestExpandTokenHTMLFilter(t *testing.T) {
	inv := Invocation{
		Args: []KeyValue{{Key: FreeformArgKey, Value: `<script>alert(1)</script>`}},
	}
	got := expandToken("<p>{{arg|html}}</p>", inv, identityEncoder)
	want := "<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestExpandTokenKVFilters(t *testing.T) {
	inv := Invocation{
		Args: []KeyValue{{Key: "to", Value: `a"b`}, {Key: "msg", Value: "x&y"}},
	}
	got := expandToken(`{"to":"{{arg.to|json}}","msg":"{{arg.msg|json}}"}`, inv, identityEncoder)
	want := `{"to":"a\"b","msg":"x&y"}`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestExpandTokenUnknownTokenLeftUntouched(t *testing.T) {
	inv := Invocation{}
	got := expandToken("hi {{nobody}}", inv, identityEncoder)
	if got != "hi {{nobody}}" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandTokenSubstitutionNotRescanned(t *testing.T) {
	// A value that itself looks like a token must not trigger a second
	// substitution.
	inv := Invocation{
		Args: []KeyValue{{Key: FreeformArgKey, Value: "{{action}}"}},
	}
	got := expandToken("{{arg}}", inv, identityEncoder)
	if got != "{{action}}" {
		t.Fatalf("substituted text was re-scanned: %q", got)
	}
}
