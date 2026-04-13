package builtins

import (
	"crypto/hmac"
	"crypto/md5"  //nolint:gosec // md5 intentionally exposed as Starlark builtin
	"crypto/sha1" //nolint:gosec // sha1 intentionally exposed as Starlark builtin
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"

	"github.com/zeebo/blake3"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// CryptoModule is the predeclared "crypto" namespace module.
// It provides deterministic hashing and ID generation functions.
var CryptoModule = &starlarkstruct.Module{
	Name: "crypto",
	Members: starlark.StringDict{
		"sha256":      starlark.NewBuiltin("crypto.sha256", cryptoSHA256),
		"sha512":      starlark.NewBuiltin("crypto.sha512", cryptoSHA512),
		"sha1":        starlark.NewBuiltin("crypto.sha1", cryptoSHA1),
		"md5":         starlark.NewBuiltin("crypto.md5", cryptoMD5),
		"hmac_sha256": starlark.NewBuiltin("crypto.hmac_sha256", cryptoHMACSHA256),
		"blake3":      starlark.NewBuiltin("crypto.blake3", cryptoBlake3),
		"stable_id":   starlark.NewBuiltin("crypto.stable_id", cryptoStableID),
	},
}

// toBytes extracts Go []byte from a Starlark string or bytes value.
func toBytes(fnName string, v starlark.Value) ([]byte, error) {
	switch v := v.(type) {
	case starlark.String:
		return []byte(string(v)), nil
	case starlark.Bytes:
		return []byte(string(v)), nil
	default:
		return nil, fmt.Errorf("%s: got %s, want string or bytes", fnName, v.Type())
	}
}

// cryptoSHA256 implements crypto.sha256(data) -> lowercase hex digest.
func cryptoSHA256(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "data", &data); err != nil {
		return nil, err
	}
	raw, err := toBytes(b.Name(), data)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	return starlark.String(hex.EncodeToString(sum[:])), nil
}

// cryptoSHA512 implements crypto.sha512(data) -> lowercase hex digest.
func cryptoSHA512(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "data", &data); err != nil {
		return nil, err
	}
	raw, err := toBytes(b.Name(), data)
	if err != nil {
		return nil, err
	}
	sum := sha512.Sum512(raw)
	return starlark.String(hex.EncodeToString(sum[:])), nil
}

// cryptoSHA1 implements crypto.sha1(data) -> lowercase hex digest.
func cryptoSHA1(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "data", &data); err != nil {
		return nil, err
	}
	raw, err := toBytes(b.Name(), data)
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum(raw) //nolint:gosec // intentional
	return starlark.String(hex.EncodeToString(sum[:])), nil
}

// cryptoMD5 implements crypto.md5(data) -> lowercase hex digest.
func cryptoMD5(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "data", &data); err != nil {
		return nil, err
	}
	raw, err := toBytes(b.Name(), data)
	if err != nil {
		return nil, err
	}
	sum := md5.Sum(raw) //nolint:gosec // intentional
	return starlark.String(hex.EncodeToString(sum[:])), nil
}

// cryptoHMACSHA256 implements crypto.hmac_sha256(key, message) -> hex HMAC digest.
func cryptoHMACSHA256(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var keyVal, msgVal starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "key", &keyVal, "message", &msgVal); err != nil {
		return nil, err
	}
	key, err := toBytes(b.Name(), keyVal)
	if err != nil {
		return nil, err
	}
	msg, err := toBytes(b.Name(), msgVal)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(msg)
	return starlark.String(hex.EncodeToString(mac.Sum(nil))), nil
}

// cryptoBlake3 implements crypto.blake3(data) -> lowercase hex digest.
func cryptoBlake3(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "data", &data); err != nil {
		return nil, err
	}
	raw, err := toBytes(b.Name(), data)
	if err != nil {
		return nil, err
	}
	sum := blake3.Sum256(raw)
	return starlark.String(hex.EncodeToString(sum[:])), nil
}

// cryptoStableID implements crypto.stable_id(seed, length=8) -> deterministic hex ID.
// It hashes the seed with SHA-256 and returns the first `length` hex characters.
func cryptoStableID(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var seed starlark.Value
	length := starlark.MakeInt(8)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "seed", &seed, "length?", &length); err != nil {
		return nil, err
	}
	raw, err := toBytes(b.Name(), seed)
	if err != nil {
		return nil, err
	}
	n, ok := length.Int64()
	if !ok || n < 1 || n > 64 {
		return nil, fmt.Errorf("%s: length must be between 1 and 64, got %s", b.Name(), length.String())
	}
	sum := sha256.Sum256(raw)
	hexStr := hex.EncodeToString(sum[:])
	return starlark.String(hexStr[:n]), nil
}
