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
// Phase 30 Plan 01 — builtins/crypto_test.go
//
// Three-layer test coverage for the `crypto` module:
//
//   Layer 1 (unit on BuildGlobals output):
//     - TestBuildGlobals_CryptoModule
//
//   Layer 2 (in-process via Runtime.Execute):
//     - TestCrypto_SHA256
//     - TestCrypto_SHA512
//     - TestCrypto_SHA1
//     - TestCrypto_MD5
//     - TestCrypto_HMACSHA256
//     - TestCrypto_Blake3
//     - TestCrypto_StableID
//     - TestCrypto_NegativeCases
//
// No Layer 3 (protobuf round-trip) needed for crypto — hash outputs are
// plain strings that already survive the convert pipeline.
//
// Fixtures are all inline Go string literals (no external fixture files)
// per 29-CONTEXT.md §Fixture placement.
// ---------------------------------------------------------------------------

// runCryptoScript compiles and runs a Starlark source string against the full
// BuildGlobals predeclared set (which includes `crypto`) via Runtime.Execute,
// returning the post-execution globals. Fails the test on any error.
func runCryptoScript(t *testing.T, src string) starlark.StringDict {
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

// runCryptoScriptExpectError runs a Starlark source string via Runtime.Execute,
// expecting a non-nil error whose message contains wantErrSubstr (case-
// insensitive). Fails the test if the script succeeds or if the error message
// does not contain the substring.
func runCryptoScriptExpectError(t *testing.T, src string, wantErrSubstr string) {
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
// Layer 1 -- structural assertion on BuildGlobals output
// ---------------------------------------------------------------------------

func TestBuildGlobals_CryptoModule(t *testing.T) {
	req := makeReq(nil, nil, nil)
	c := NewCollector(NewConditionCollector(), "test.star", nil)

	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatalf("testBuildGlobals error: %v", err)
	}

	v, ok := globals["crypto"]
	if !ok {
		t.Fatal(`globals["crypto"] missing -- crypto module not registered in BuildGlobals`)
	}

	mod, ok := v.(*starlarkstruct.Module)
	if !ok {
		t.Fatalf(`globals["crypto"] is %T, want *starlarkstruct.Module`, v)
	}

	if mod.Name != "crypto" {
		t.Errorf("mod.Name = %q, want %q", mod.Name, "crypto")
	}

	wantMembers := []string{"sha256", "sha512", "sha1", "md5", "hmac_sha256", "blake3", "stable_id"}
	for _, name := range wantMembers {
		if _, ok := mod.Members[name]; !ok {
			t.Errorf(`crypto.Members missing %q`, name)
		}
	}

	// Guard against drift that silently adds or removes a member.
	if got := len(mod.Members); got != len(wantMembers) {
		t.Errorf("len(mod.Members) = %d, want %d (crypto module drift?)", got, len(wantMembers))
	}
}

// ---------------------------------------------------------------------------
// Layer 2 -- in-process tests via Runtime.Execute
// ---------------------------------------------------------------------------

// TestCrypto_SHA256 covers CRY-01.
func TestCrypto_SHA256(t *testing.T) {
	out := runCryptoScript(t, `
result = crypto.sha256("foo")
empty = crypto.sha256("")
# Determinism: two calls, same result
a = crypto.sha256("foo")
b = crypto.sha256("foo")
deterministic = (a == b)
`)
	got, ok := out["result"].(starlark.String)
	if !ok {
		t.Fatalf(`out["result"] is %T, want starlark.String`, out["result"])
	}
	want := "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"
	if string(got) != want {
		t.Errorf("crypto.sha256(\"foo\") = %q, want %q", string(got), want)
	}

	empty, ok := out["empty"].(starlark.String)
	if !ok {
		t.Fatalf(`out["empty"] is %T, want starlark.String`, out["empty"])
	}
	if len(string(empty)) != 64 {
		t.Errorf("crypto.sha256(\"\") length = %d, want 64", len(string(empty)))
	}

	det, ok := out["deterministic"].(starlark.Bool)
	if !ok || !bool(det) {
		t.Error("crypto.sha256 is not deterministic: two calls returned different results")
	}
}

// TestCrypto_SHA512 covers CRY-02.
func TestCrypto_SHA512(t *testing.T) {
	out := runCryptoScript(t, `
result = crypto.sha512("foo")
`)
	got, ok := out["result"].(starlark.String)
	if !ok {
		t.Fatalf(`out["result"] is %T, want starlark.String`, out["result"])
	}
	if len(string(got)) != 128 {
		t.Errorf("crypto.sha512(\"foo\") length = %d, want 128", len(string(got)))
	}
	// Known SHA-512 of "foo"
	want := "f7fbba6e0636f890e56fbbf3283e524c6fa3204ae298382d624741d0dc6638326e282c41be5e4254d8820772c5518a2c5a8c0c7f7eda19594a7eb539453e1ed7"
	if string(got) != want {
		t.Errorf("crypto.sha512(\"foo\") = %q, want %q", string(got), want)
	}
}

// TestCrypto_SHA1 covers CRY-03.
func TestCrypto_SHA1(t *testing.T) {
	out := runCryptoScript(t, `
result = crypto.sha1("foo")
`)
	got, ok := out["result"].(starlark.String)
	if !ok {
		t.Fatalf(`out["result"] is %T, want starlark.String`, out["result"])
	}
	want := "0beec7b5ea3f0fdbc95d0dd47f3c5bc275da8a33"
	if string(got) != want {
		t.Errorf("crypto.sha1(\"foo\") = %q, want %q", string(got), want)
	}
	if len(string(got)) != 40 {
		t.Errorf("crypto.sha1(\"foo\") length = %d, want 40", len(string(got)))
	}
}

// TestCrypto_MD5 covers CRY-04.
func TestCrypto_MD5(t *testing.T) {
	out := runCryptoScript(t, `
result = crypto.md5("foo")
`)
	got, ok := out["result"].(starlark.String)
	if !ok {
		t.Fatalf(`out["result"] is %T, want starlark.String`, out["result"])
	}
	want := "acbd18db4cc2f85cedef654fccc4a4d8"
	if string(got) != want {
		t.Errorf("crypto.md5(\"foo\") = %q, want %q", string(got), want)
	}
	if len(string(got)) != 32 {
		t.Errorf("crypto.md5(\"foo\") length = %d, want 32", len(string(got)))
	}
}

// TestCrypto_HMACSHA256 covers CRY-05.
func TestCrypto_HMACSHA256(t *testing.T) {
	out := runCryptoScript(t, `
result = crypto.hmac_sha256("secret", "hello")
# Different key should produce different result
other = crypto.hmac_sha256("other", "hello")
differ = (result != other)
`)
	got, ok := out["result"].(starlark.String)
	if !ok {
		t.Fatalf(`out["result"] is %T, want starlark.String`, out["result"])
	}
	// crypto/hmac sha256 of key="secret", msg="hello"
	want := "88aab3ede8d3adf94d26ab90d3bafd4a2083070c3bcce9c014ee04a443847c0b"
	if string(got) != want {
		t.Errorf("crypto.hmac_sha256(\"secret\", \"hello\") = %q, want %q", string(got), want)
	}

	differ, ok := out["differ"].(starlark.Bool)
	if !ok || !bool(differ) {
		t.Error("different HMAC keys should produce different results")
	}
}

// TestCrypto_Blake3 covers CRY-07.
func TestCrypto_Blake3(t *testing.T) {
	out := runCryptoScript(t, `
result = crypto.blake3("foo")
a = crypto.blake3("foo")
b = crypto.blake3("foo")
deterministic = (a == b)
`)
	got, ok := out["result"].(starlark.String)
	if !ok {
		t.Fatalf(`out["result"] is %T, want starlark.String`, out["result"])
	}
	if len(string(got)) != 64 {
		t.Errorf("crypto.blake3(\"foo\") length = %d, want 64", len(string(got)))
	}

	det, ok := out["deterministic"].(starlark.Bool)
	if !ok || !bool(det) {
		t.Error("crypto.blake3 is not deterministic: two calls returned different results")
	}

	// Assert against known reference digest (computed with github.com/zeebo/blake3).
	expected := "04e0bb39f30b1a3feb89f536c93be15055482df748674b00d26e5a75777702e9"
	if string(got) != expected {
		t.Errorf("blake3('foo') = %s, want %s", string(got), expected)
	}
}

// TestCrypto_StableID covers CRY-06.
func TestCrypto_StableID(t *testing.T) {
	out := runCryptoScript(t, `
# Default length=8
default_id = crypto.stable_id("vpc-main")
default_len = len(default_id)

# Custom length=16
custom_id = crypto.stable_id("vpc-main", length=16)
custom_len = len(custom_id)

# Full length=64 (full SHA-256 hex)
full_id = crypto.stable_id("vpc-main", length=64)
full_len = len(full_id)

# Determinism: same seed+length
a = crypto.stable_id("vpc-main")
b = crypto.stable_id("vpc-main")
deterministic = (a == b)

# Different seeds produce different IDs
id_a = crypto.stable_id("a")
id_b = crypto.stable_id("b")
differ = (id_a != id_b)

# The full 64-char ID starts with the 8-char default
prefix_match = (full_id[:8] == default_id)
`)
	// Default length = 8
	defaultLen, ok := out["default_len"].(starlark.Int)
	if !ok {
		t.Fatalf(`out["default_len"] is %T, want starlark.Int`, out["default_len"])
	}
	if n, _ := defaultLen.Int64(); n != 8 {
		t.Errorf("len(crypto.stable_id(\"vpc-main\")) = %d, want 8", n)
	}

	// Custom length = 16
	customLen, ok := out["custom_len"].(starlark.Int)
	if !ok {
		t.Fatalf(`out["custom_len"] is %T, want starlark.Int`, out["custom_len"])
	}
	if n, _ := customLen.Int64(); n != 16 {
		t.Errorf("len(crypto.stable_id(\"vpc-main\", length=16)) = %d, want 16", n)
	}

	// Full length = 64
	fullLen, ok := out["full_len"].(starlark.Int)
	if !ok {
		t.Fatalf(`out["full_len"] is %T, want starlark.Int`, out["full_len"])
	}
	if n, _ := fullLen.Int64(); n != 64 {
		t.Errorf("len(crypto.stable_id(\"vpc-main\", length=64)) = %d, want 64", n)
	}

	// Determinism
	det, ok := out["deterministic"].(starlark.Bool)
	if !ok || !bool(det) {
		t.Error("crypto.stable_id is not deterministic: two calls returned different results")
	}

	// Different seeds differ
	differ, ok := out["differ"].(starlark.Bool)
	if !ok || !bool(differ) {
		t.Error("crypto.stable_id should produce different IDs for different seeds")
	}

	// Prefix match
	prefix, ok := out["prefix_match"].(starlark.Bool)
	if !ok || !bool(prefix) {
		t.Error("full 64-char ID should start with the default 8-char ID (same seed)")
	}
}

// TestCrypto_BytesInput verifies that passing starlark.Bytes produces the same
// digest as passing the equivalent string for all hash functions.
func TestCrypto_BytesInput(t *testing.T) {
	out := runCryptoScript(t, `
sha256_str = crypto.sha256("foo")
sha256_bytes = crypto.sha256(b"foo")
sha256_match = (sha256_str == sha256_bytes)

sha512_str = crypto.sha512("foo")
sha512_bytes = crypto.sha512(b"foo")
sha512_match = (sha512_str == sha512_bytes)

sha1_str = crypto.sha1("foo")
sha1_bytes = crypto.sha1(b"foo")
sha1_match = (sha1_str == sha1_bytes)

md5_str = crypto.md5("foo")
md5_bytes = crypto.md5(b"foo")
md5_match = (md5_str == md5_bytes)

blake3_str = crypto.blake3("foo")
blake3_bytes = crypto.blake3(b"foo")
blake3_match = (blake3_str == blake3_bytes)
`)
	assertBool(t, out, "sha256_match", true)
	assertBool(t, out, "sha512_match", true)
	assertBool(t, out, "sha1_match", true)
	assertBool(t, out, "md5_match", true)
	assertBool(t, out, "blake3_match", true)
}

// TestCrypto_WrongArity verifies that hash functions reject 0 or 2+ positional args.
func TestCrypto_WrongArity(t *testing.T) {
	// 0 args
	runCryptoScriptExpectError(t,
		`x = crypto.sha256()`,
		"missing argument",
	)
	// 2 args
	runCryptoScriptExpectError(t,
		`x = crypto.sha256("a", "b")`,
		"got 2 arguments",
	)
}

// TestCrypto_StableID_Length1 verifies that stable_id with length=1 returns a single hex character.
func TestCrypto_StableID_Length1(t *testing.T) {
	out := runCryptoScript(t, `
id = crypto.stable_id("test", length=1)
id_len = len(id)
`)
	assertInt(t, out, "id_len", 1)

	// Verify the single character is a valid hex digit.
	id, ok := out["id"].(starlark.String)
	if !ok {
		t.Fatalf(`out["id"] is %T, want starlark.String`, out["id"])
	}
	c := string(id)[0]
	if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
		t.Errorf("stable_id(length=1) = %q, want hex digit [0-9a-f]", string(id))
	}
}

// TestCrypto_NegativeCases asserts that bad inputs fail with expected errors.
func TestCrypto_NegativeCases(t *testing.T) {
	// Type error: int instead of string or bytes
	runCryptoScriptExpectError(t,
		`x = crypto.sha256(42)`,
		"got int, want string or bytes",
	)

	// stable_id length=0 out of range
	runCryptoScriptExpectError(t,
		`x = crypto.stable_id("x", length=0)`,
		"length must be between 1 and 64",
	)

	// stable_id length=65 out of range
	runCryptoScriptExpectError(t,
		`x = crypto.stable_id("x", length=65)`,
		"length must be between 1 and 64",
	)

	// hmac_sha256 type error for key
	runCryptoScriptExpectError(t,
		`x = crypto.hmac_sha256(42, "msg")`,
		"got int, want string or bytes",
	)

	// hmac_sha256 type error for message
	runCryptoScriptExpectError(t,
		`x = crypto.hmac_sha256("key", 42)`,
		"got int, want string or bytes",
	)
}
