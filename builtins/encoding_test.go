package builtins

import (
	"strings"
	"testing"

	"github.com/crossplane/function-sdk-go/logging"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/wompipomp/function-starlark/runtime"
)

// ---------------------------------------------------------------------------
// Phase 30 Plan 02 — builtins/encoding_test.go
//
// Three-layer test coverage for the `encoding` module:
//
//   Layer 1 (unit on BuildGlobals output):
//     - TestBuildGlobals_EncodingModule
//
//   Layer 2 (in-process via Runtime.Execute):
//     - TestEncoding_Base64
//     - TestEncoding_Base64URL
//     - TestEncoding_Base32
//     - TestEncoding_Hex
//     - TestEncoding_NegativeCases
//
// No Layer 3 (protobuf round-trip) needed — encoding outputs are plain
// strings that already survive the convert pipeline.
//
// Fixtures are all inline Go string literals (no external fixture files)
// per 29-CONTEXT.md §Fixture placement.
// ---------------------------------------------------------------------------

// runEncodingScript compiles and runs a Starlark source string against the full
// BuildGlobals predeclared set (which includes `encoding`) via Runtime.Execute,
// returning the post-execution globals. Fails the test on any error.
func runEncodingScript(t *testing.T, src string) starlark.StringDict {
	t.Helper()
	req := makeReq(nil, nil, nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatalf("testBuildGlobals error: %v", err)
	}
	rt := runtime.NewRuntime(logging.NewNopLogger())
	out, err := rt.Execute(src, globals, "test.star", nil)
	if err != nil {
		t.Fatalf("rt.Execute error: %v\nsource:\n%s", err, src)
	}
	return out
}

// runEncodingScriptExpectError runs a Starlark source string via Runtime.Execute,
// expecting a non-nil error whose message contains wantErrSubstr (case-
// insensitive). Fails the test if the script succeeds or if the error message
// does not contain the substring.
func runEncodingScriptExpectError(t *testing.T, src string, wantErrSubstr string) {
	t.Helper()
	req := makeReq(nil, nil, nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatalf("testBuildGlobals error: %v", err)
	}
	rt := runtime.NewRuntime(logging.NewNopLogger())
	_, err = rt.Execute(src, globals, "test.star", nil)
	if err == nil {
		t.Fatalf("expected error containing %q, got nil\nsource:\n%s", wantErrSubstr, src)
	}
	if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(wantErrSubstr)) {
		t.Errorf("error message = %q, want substring %q (case-insensitive)", err.Error(), wantErrSubstr)
	}
}

// ---------------------------------------------------------------------------
// Layer 1 — structural assertion on BuildGlobals output
// ---------------------------------------------------------------------------

func TestBuildGlobals_EncodingModule(t *testing.T) {
	req := makeReq(nil, nil, nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil)

	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatalf("testBuildGlobals error: %v", err)
	}

	v, ok := globals["encoding"]
	if !ok {
		t.Fatal(`globals["encoding"] missing -- encoding module not registered in BuildGlobals`)
	}

	mod, ok := v.(*starlarkstruct.Module)
	if !ok {
		t.Fatalf(`globals["encoding"] is %T, want *starlarkstruct.Module`, v)
	}

	if mod.Name != "encoding" {
		t.Errorf("mod.Name = %q, want %q", mod.Name, "encoding")
	}

	wantMembers := []string{"b64enc", "b64dec", "b64url_enc", "b64url_dec", "b32enc", "b32dec", "hex_enc", "hex_dec"}
	for _, name := range wantMembers {
		if _, ok := mod.Members[name]; !ok {
			t.Errorf(`encoding.Members missing %q`, name)
		}
	}

	// Guard against drift that silently adds or removes a member.
	if got := len(mod.Members); got != len(wantMembers) {
		t.Errorf("len(mod.Members) = %d, want %d (encoding module drift?)", got, len(wantMembers))
	}
}

// ---------------------------------------------------------------------------
// Layer 2 — in-process tests via Runtime.Execute
// ---------------------------------------------------------------------------

// TestEncoding_Base64 covers ENC-08.
func TestEncoding_Base64(t *testing.T) {
	out := runEncodingScript(t, `
enc = encoding.b64enc("hello")
dec = encoding.b64dec("aGVsbG8=")
roundtrip = encoding.b64dec(encoding.b64enc("hello world"))
empty_roundtrip = encoding.b64dec(encoding.b64enc(""))
dec_type = type(encoding.b64dec("aGVsbG8="))
`)

	enc, ok := out["enc"].(starlark.String)
	if !ok {
		t.Fatalf(`out["enc"] is %T, want starlark.String`, out["enc"])
	}
	if want := "aGVsbG8="; string(enc) != want {
		t.Errorf("encoding.b64enc(\"hello\") = %q, want %q", string(enc), want)
	}

	dec, ok := out["dec"].(starlark.String)
	if !ok {
		t.Fatalf(`out["dec"] is %T, want starlark.String`, out["dec"])
	}
	if want := "hello"; string(dec) != want {
		t.Errorf("encoding.b64dec(\"aGVsbG8=\") = %q, want %q", string(dec), want)
	}

	rt, ok := out["roundtrip"].(starlark.String)
	if !ok {
		t.Fatalf(`out["roundtrip"] is %T, want starlark.String`, out["roundtrip"])
	}
	if want := "hello world"; string(rt) != want {
		t.Errorf("b64 round-trip = %q, want %q", string(rt), want)
	}

	empty, ok := out["empty_roundtrip"].(starlark.String)
	if !ok {
		t.Fatalf(`out["empty_roundtrip"] is %T, want starlark.String`, out["empty_roundtrip"])
	}
	if string(empty) != "" {
		t.Errorf("b64 empty round-trip = %q, want \"\"", string(empty))
	}

	decType, ok := out["dec_type"].(starlark.String)
	if !ok {
		t.Fatalf(`out["dec_type"] is %T, want starlark.String`, out["dec_type"])
	}
	if string(decType) != "string" {
		t.Errorf("type(b64dec(...)) = %q, want \"string\"", string(decType))
	}
}

// TestEncoding_Base64URL covers ENC-09.
func TestEncoding_Base64URL(t *testing.T) {
	out := runEncodingScript(t, `
encoded = encoding.b64url_enc("hello?world&foo=bar")
roundtrip = encoding.b64url_dec(encoding.b64url_enc("hello?world&foo=bar"))
dec_type = type(encoding.b64url_dec(encoding.b64url_enc("test")))
`)

	encoded, ok := out["encoded"].(starlark.String)
	if !ok {
		t.Fatalf(`out["encoded"] is %T, want starlark.String`, out["encoded"])
	}
	encStr := string(encoded)

	// URL-safe base64 must not contain +, /, or = characters.
	if strings.ContainsAny(encStr, "+/=") {
		t.Errorf("b64url_enc output %q contains +, /, or = (URL-unsafe or padded)", encStr)
	}

	rt, ok := out["roundtrip"].(starlark.String)
	if !ok {
		t.Fatalf(`out["roundtrip"] is %T, want starlark.String`, out["roundtrip"])
	}
	if want := "hello?world&foo=bar"; string(rt) != want {
		t.Errorf("b64url round-trip = %q, want %q", string(rt), want)
	}

	decType, ok := out["dec_type"].(starlark.String)
	if !ok {
		t.Fatalf(`out["dec_type"] is %T, want starlark.String`, out["dec_type"])
	}
	if string(decType) != "string" {
		t.Errorf("type(b64url_dec(...)) = %q, want \"string\"", string(decType))
	}
}

// TestEncoding_Base32 covers ENC-10.
func TestEncoding_Base32(t *testing.T) {
	out := runEncodingScript(t, `
enc = encoding.b32enc("hello")
dec = encoding.b32dec("NBSWY3DP")
roundtrip = encoding.b32dec(encoding.b32enc("arbitrary data"))
dec_type = type(encoding.b32dec("NBSWY3DP"))
`)

	enc, ok := out["enc"].(starlark.String)
	if !ok {
		t.Fatalf(`out["enc"] is %T, want starlark.String`, out["enc"])
	}
	if want := "NBSWY3DP"; string(enc) != want {
		t.Errorf("encoding.b32enc(\"hello\") = %q, want %q", string(enc), want)
	}

	dec, ok := out["dec"].(starlark.String)
	if !ok {
		t.Fatalf(`out["dec"] is %T, want starlark.String`, out["dec"])
	}
	if want := "hello"; string(dec) != want {
		t.Errorf("encoding.b32dec(\"NBSWY3DP\") = %q, want %q", string(dec), want)
	}

	rt, ok := out["roundtrip"].(starlark.String)
	if !ok {
		t.Fatalf(`out["roundtrip"] is %T, want starlark.String`, out["roundtrip"])
	}
	if want := "arbitrary data"; string(rt) != want {
		t.Errorf("b32 round-trip = %q, want %q", string(rt), want)
	}

	decType, ok := out["dec_type"].(starlark.String)
	if !ok {
		t.Fatalf(`out["dec_type"] is %T, want starlark.String`, out["dec_type"])
	}
	if string(decType) != "string" {
		t.Errorf("type(b32dec(...)) = %q, want \"string\"", string(decType))
	}
}

// TestEncoding_Hex covers ENC-11.
func TestEncoding_Hex(t *testing.T) {
	out := runEncodingScript(t, `
enc = encoding.hex_enc("hello")
dec = encoding.hex_dec("68656c6c6f")
roundtrip = encoding.hex_dec(encoding.hex_enc("arbitrary data"))
dec_type = type(encoding.hex_dec("68656c6c6f"))
`)

	enc, ok := out["enc"].(starlark.String)
	if !ok {
		t.Fatalf(`out["enc"] is %T, want starlark.String`, out["enc"])
	}
	if want := "68656c6c6f"; string(enc) != want {
		t.Errorf("encoding.hex_enc(\"hello\") = %q, want %q", string(enc), want)
	}

	dec, ok := out["dec"].(starlark.String)
	if !ok {
		t.Fatalf(`out["dec"] is %T, want starlark.String`, out["dec"])
	}
	if want := "hello"; string(dec) != want {
		t.Errorf("encoding.hex_dec(\"68656c6c6f\") = %q, want %q", string(dec), want)
	}

	rt, ok := out["roundtrip"].(starlark.String)
	if !ok {
		t.Fatalf(`out["roundtrip"] is %T, want starlark.String`, out["roundtrip"])
	}
	if want := "arbitrary data"; string(rt) != want {
		t.Errorf("hex round-trip = %q, want %q", string(rt), want)
	}

	decType, ok := out["dec_type"].(starlark.String)
	if !ok {
		t.Fatalf(`out["dec_type"] is %T, want starlark.String`, out["dec_type"])
	}
	if string(decType) != "string" {
		t.Errorf("type(hex_dec(...)) = %q, want \"string\"", string(decType))
	}
}

// TestEncoding_BytesInput verifies that encode functions accept bytes input.
func TestEncoding_BytesInput(t *testing.T) {
	out := runEncodingScript(t, `
# b"hello" is a Starlark bytes literal
enc = encoding.b64enc(b"hello")
`)

	enc, ok := out["enc"].(starlark.String)
	if !ok {
		t.Fatalf(`out["enc"] is %T, want starlark.String`, out["enc"])
	}
	if want := "aGVsbG8="; string(enc) != want {
		t.Errorf("encoding.b64enc(b\"hello\") = %q, want %q", string(enc), want)
	}
}

// TestEncoding_NegativeCases asserts that bad inputs fail with expected errors.
func TestEncoding_NegativeCases(t *testing.T) {
	// b64dec with invalid input
	runEncodingScriptExpectError(t,
		`x = encoding.b64dec("!!!invalid!!!")`,
		"encoding.b64dec",
	)

	// b32dec with invalid input
	runEncodingScriptExpectError(t,
		`x = encoding.b32dec("!!!invalid!!!")`,
		"encoding.b32dec",
	)

	// hex_dec with invalid input
	runEncodingScriptExpectError(t,
		`x = encoding.hex_dec("xyz")`,
		"encoding.hex_dec",
	)

	// b64enc with wrong type (int)
	runEncodingScriptExpectError(t,
		`x = encoding.b64enc(42)`,
		"got int, want string or bytes",
	)
}
