package builtins

import (
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// EncodingModule is the predeclared "encoding" namespace module.
// It provides base64, base32, and hex encode/decode functions.
var EncodingModule = &starlarkstruct.Module{
	Name: "encoding",
	Members: starlark.StringDict{
		"b64enc":     starlark.NewBuiltin("encoding.b64enc", encodingB64Enc),
		"b64dec":     starlark.NewBuiltin("encoding.b64dec", encodingB64Dec),
		"b64url_enc": starlark.NewBuiltin("encoding.b64url_enc", encodingB64URLEnc),
		"b64url_dec": starlark.NewBuiltin("encoding.b64url_dec", encodingB64URLDec),
		"b32enc":     starlark.NewBuiltin("encoding.b32enc", encodingB32Enc),
		"b32dec":     starlark.NewBuiltin("encoding.b32dec", encodingB32Dec),
		"hex_enc":    starlark.NewBuiltin("encoding.hex_enc", encodingHexEnc),
		"hex_dec":    starlark.NewBuiltin("encoding.hex_dec", encodingHexDec),
	},
}

// encodingB64Enc implements encoding.b64enc(data) -> standard base64 string.
func encodingB64Enc(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "data", &data); err != nil {
		return nil, err
	}
	raw, err := toBytes(b.Name(), data)
	if err != nil {
		return nil, err
	}
	return starlark.String(base64.StdEncoding.EncodeToString(raw)), nil
}

// encodingB64Dec implements encoding.b64dec(data) -> decoded string.
func encodingB64Dec(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "data", &data); err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	return starlark.String(string(decoded)), nil
}

// encodingB64URLEnc implements encoding.b64url_enc(data) -> URL-safe base64 without padding.
func encodingB64URLEnc(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "data", &data); err != nil {
		return nil, err
	}
	raw, err := toBytes(b.Name(), data)
	if err != nil {
		return nil, err
	}
	return starlark.String(base64.RawURLEncoding.EncodeToString(raw)), nil
}

// encodingB64URLDec implements encoding.b64url_dec(data) -> decoded string.
func encodingB64URLDec(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "data", &data); err != nil {
		return nil, err
	}
	decoded, err := base64.RawURLEncoding.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	return starlark.String(string(decoded)), nil
}

// encodingB32Enc implements encoding.b32enc(data) -> standard base32 string.
func encodingB32Enc(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "data", &data); err != nil {
		return nil, err
	}
	raw, err := toBytes(b.Name(), data)
	if err != nil {
		return nil, err
	}
	return starlark.String(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)), nil
}

// encodingB32Dec implements encoding.b32dec(data) -> decoded string.
func encodingB32Dec(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "data", &data); err != nil {
		return nil, err
	}
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	return starlark.String(string(decoded)), nil
}

// encodingHexEnc implements encoding.hex_enc(data) -> lowercase hex string.
func encodingHexEnc(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "data", &data); err != nil {
		return nil, err
	}
	raw, err := toBytes(b.Name(), data)
	if err != nil {
		return nil, err
	}
	return starlark.String(hex.EncodeToString(raw)), nil
}

// encodingHexDec implements encoding.hex_dec(data) -> decoded string.
func encodingHexDec(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "data", &data); err != nil {
		return nil, err
	}
	decoded, err := hex.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	return starlark.String(string(decoded)), nil
}
