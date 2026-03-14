package builtins

import (
	"testing"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"go.starlark.net/starlark"
	"google.golang.org/protobuf/types/known/structpb"
)

// ---------------------------------------------------------------------------
// ConnectionCollector / set_connection_details tests
// ---------------------------------------------------------------------------

func TestConnectionCollector_NewEmpty(t *testing.T) {
	cc := NewConnectionCollector()
	if cc == nil {
		t.Fatal("NewConnectionCollector returned nil")
	}
	cd := cc.ConnectionDetails()
	if len(cd) != 0 {
		t.Errorf("ConnectionDetails() = %d, want 0", len(cd))
	}
}

func TestSetConnectionDetails_Basic(t *testing.T) {
	cc := NewConnectionCollector()
	thread := new(starlark.Thread)

	d := new(starlark.Dict)
	_ = d.SetKey(starlark.String("username"), starlark.String("admin"))
	_ = d.SetKey(starlark.String("password"), starlark.String("secret123"))

	_, err := starlark.Call(thread, cc.SetConnectionDetailsBuiltin(), starlark.Tuple{d}, nil)
	if err != nil {
		t.Fatalf("set_connection_details error: %v", err)
	}

	cd := cc.ConnectionDetails()
	if len(cd) != 2 {
		t.Fatalf("ConnectionDetails() = %d, want 2", len(cd))
	}
	if string(cd["username"]) != "admin" {
		t.Errorf("username = %q, want 'admin'", cd["username"])
	}
	if string(cd["password"]) != "secret123" {
		t.Errorf("password = %q, want 'secret123'", cd["password"])
	}
}

func TestSetConnectionDetails_NonStringKey(t *testing.T) {
	cc := NewConnectionCollector()
	thread := new(starlark.Thread)

	d := new(starlark.Dict)
	_ = d.SetKey(starlark.MakeInt(42), starlark.String("value"))

	_, err := starlark.Call(thread, cc.SetConnectionDetailsBuiltin(), starlark.Tuple{d}, nil)
	if err == nil {
		t.Fatal("set_connection_details with non-string key should error")
	}
}

func TestSetConnectionDetails_NonStringValue(t *testing.T) {
	cc := NewConnectionCollector()
	thread := new(starlark.Thread)

	d := new(starlark.Dict)
	_ = d.SetKey(starlark.String("key"), starlark.MakeInt(42))

	_, err := starlark.Call(thread, cc.SetConnectionDetailsBuiltin(), starlark.Tuple{d}, nil)
	if err == nil {
		t.Fatal("set_connection_details with non-string value should error")
	}
}

func TestSetConnectionDetails_MergesAdditively(t *testing.T) {
	cc := NewConnectionCollector()
	thread := new(starlark.Thread)

	// First call
	d1 := new(starlark.Dict)
	_ = d1.SetKey(starlark.String("key1"), starlark.String("val1"))

	_, err := starlark.Call(thread, cc.SetConnectionDetailsBuiltin(), starlark.Tuple{d1}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Second call
	d2 := new(starlark.Dict)
	_ = d2.SetKey(starlark.String("key2"), starlark.String("val2"))

	_, err = starlark.Call(thread, cc.SetConnectionDetailsBuiltin(), starlark.Tuple{d2}, nil)
	if err != nil {
		t.Fatal(err)
	}

	cd := cc.ConnectionDetails()
	if len(cd) != 2 {
		t.Fatalf("ConnectionDetails() = %d, want 2 (additive merge)", len(cd))
	}
	if string(cd["key1"]) != "val1" {
		t.Error("key1 should be preserved from first call")
	}
	if string(cd["key2"]) != "val2" {
		t.Error("key2 should be added from second call")
	}
}

func TestConnectionDetails_ReturnsCopy(t *testing.T) {
	cc := NewConnectionCollector()
	thread := new(starlark.Thread)

	d := new(starlark.Dict)
	_ = d.SetKey(starlark.String("key"), starlark.String("val"))

	_, _ = starlark.Call(thread, cc.SetConnectionDetailsBuiltin(), starlark.Tuple{d}, nil)

	cd1 := cc.ConnectionDetails()
	cd2 := cc.ConnectionDetails()
	delete(cd1, "key")
	if len(cd2) != 1 {
		t.Error("ConnectionDetails() should return a copy")
	}
}

// ---------------------------------------------------------------------------
// ApplyConnectionDetails tests
// ---------------------------------------------------------------------------

func TestApplyConnectionDetails_Empty(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyConnectionDetails(rsp, nil)
	if rsp.Desired != nil {
		t.Error("ApplyConnectionDetails with nil should not create Desired")
	}
}

func TestApplyConnectionDetails_Sets(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	cd := map[string][]byte{
		"username": []byte("admin"),
		"password": []byte("secret"),
	}
	ApplyConnectionDetails(rsp, cd)

	if rsp.Desired == nil || rsp.Desired.Composite == nil {
		t.Fatal("Desired.Composite should be created")
	}
	got := rsp.Desired.Composite.ConnectionDetails
	if len(got) != 2 {
		t.Fatalf("ConnectionDetails = %d, want 2", len(got))
	}
	if string(got["username"]) != "admin" {
		t.Error("username mismatch")
	}
}

func TestApplyConnectionDetails_CreatesNilChain(t *testing.T) {
	// First-in-pipeline: Desired, Composite, and ConnectionDetails all nil
	rsp := &fnv1.RunFunctionResponse{}
	cd := map[string][]byte{"key": []byte("val")}
	ApplyConnectionDetails(rsp, cd)

	if rsp.Desired == nil {
		t.Fatal("Desired should be created")
	}
	if rsp.Desired.Composite == nil {
		t.Fatal("Composite should be created")
	}
	if rsp.Desired.Composite.ConnectionDetails == nil {
		t.Fatal("ConnectionDetails should be created")
	}
}

func TestApplyConnectionDetails_MergesExisting(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{
		Desired: &fnv1.State{
			Composite: &fnv1.Resource{
				ConnectionDetails: map[string][]byte{
					"existing": []byte("kept"),
				},
			},
		},
	}
	cd := map[string][]byte{"new": []byte("added")}
	ApplyConnectionDetails(rsp, cd)

	got := rsp.Desired.Composite.ConnectionDetails
	if len(got) != 2 {
		t.Fatalf("ConnectionDetails = %d, want 2", len(got))
	}
	if string(got["existing"]) != "kept" {
		t.Error("existing key should be preserved")
	}
	if string(got["new"]) != "added" {
		t.Error("new key should be added")
	}
}

// ---------------------------------------------------------------------------
// Resource() connection_details kwarg tests
// ---------------------------------------------------------------------------

func TestCollector_ConnectionDetails_Kwarg(t *testing.T) {
	c := NewCollector()
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	connDetails := new(starlark.Dict)
	_ = connDetails.SetKey(starlark.String("host"), starlark.String("db.example.com"))
	_ = connDetails.SetKey(starlark.String("port"), starlark.String("5432"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db-instance"),
		body,
	}, []starlark.Tuple{
		{starlark.String("connection_details"), connDetails},
	})
	if err != nil {
		t.Fatalf("Resource() error: %v", err)
	}

	res := c.Resources()
	cr, ok := res["db-instance"]
	if !ok {
		t.Fatal("missing resource 'db-instance'")
	}
	if cr.ConnectionDetails == nil {
		t.Fatal("ConnectionDetails is nil")
	}
	if string(cr.ConnectionDetails["host"]) != "db.example.com" {
		t.Errorf("host = %q, want 'db.example.com'", cr.ConnectionDetails["host"])
	}
	if string(cr.ConnectionDetails["port"]) != "5432" {
		t.Errorf("port = %q, want '5432'", cr.ConnectionDetails["port"])
	}
}

func TestCollector_ConnectionDetails_BackwardCompatible(t *testing.T) {
	// Resource() without connection_details kwarg should still work
	c := NewCollector()
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, nil)
	if err != nil {
		t.Fatalf("Resource() without connection_details should work: %v", err)
	}

	cr := c.Resources()["item"]
	if cr.ConnectionDetails != nil {
		t.Error("ConnectionDetails should be nil when not provided")
	}
}

func TestCollector_ConnectionDetails_NonStringKey(t *testing.T) {
	c := NewCollector()
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	connDetails := new(starlark.Dict)
	_ = connDetails.SetKey(starlark.MakeInt(42), starlark.String("val"))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, []starlark.Tuple{
		{starlark.String("connection_details"), connDetails},
	})
	if err == nil {
		t.Fatal("Resource() with non-string connection_details key should error")
	}
}

func TestCollector_ConnectionDetails_NonStringValue(t *testing.T) {
	c := NewCollector()
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	connDetails := new(starlark.Dict)
	_ = connDetails.SetKey(starlark.String("key"), starlark.MakeInt(42))

	_, err := starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, []starlark.Tuple{
		{starlark.String("connection_details"), connDetails},
	})
	if err == nil {
		t.Fatal("Resource() with non-string connection_details value should error")
	}
}

func TestCollector_StringToByteConversion(t *testing.T) {
	// Verify that string values are converted to []byte via []byte(string)
	c := NewCollector()
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	connDetails := new(starlark.Dict)
	_ = connDetails.SetKey(starlark.String("secret"), starlark.String("my-secret-value"))

	_, _ = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, []starlark.Tuple{
		{starlark.String("connection_details"), connDetails},
	})

	cr := c.Resources()["item"]
	// Verify it's raw bytes, not base64 encoded
	expected := []byte("my-secret-value")
	got := cr.ConnectionDetails["secret"]
	if string(got) != string(expected) {
		t.Errorf("secret = %q, want %q (raw bytes, no base64)", got, expected)
	}
}

// ---------------------------------------------------------------------------
// ApplyResources with ConnectionDetails tests
// ---------------------------------------------------------------------------

func TestApplyResources_WithConnectionDetails(t *testing.T) {
	c := NewCollector()
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	connDetails := new(starlark.Dict)
	_ = connDetails.SetKey(starlark.String("endpoint"), starlark.String("https://db.example.com"))

	_, _ = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("db"),
		body,
	}, []starlark.Tuple{
		{starlark.String("connection_details"), connDetails},
	})

	rsp := &fnv1.RunFunctionResponse{}
	if err := ApplyResources(rsp, c); err != nil {
		t.Fatalf("ApplyResources error: %v", err)
	}

	r, ok := rsp.Desired.Resources["db"]
	if !ok {
		t.Fatal("missing resource 'db'")
	}
	if r.ConnectionDetails == nil {
		t.Fatal("ConnectionDetails not set on resource")
	}
	if string(r.ConnectionDetails["endpoint"]) != "https://db.example.com" {
		t.Errorf("endpoint = %q, want 'https://db.example.com'", r.ConnectionDetails["endpoint"])
	}
}

func TestApplyResources_WithoutConnectionDetails(t *testing.T) {
	c := NewCollector()
	thread := new(starlark.Thread)

	body := new(starlark.Dict)
	_ = body.SetKey(starlark.String("apiVersion"), starlark.String("v1"))

	_, _ = starlark.Call(thread, c.Builtin(), starlark.Tuple{
		starlark.String("item"),
		body,
	}, nil)

	rsp := &fnv1.RunFunctionResponse{
		Desired: &fnv1.State{
			Composite: &fnv1.Resource{
				Resource: &structpb.Struct{},
			},
		},
	}
	if err := ApplyResources(rsp, c); err != nil {
		t.Fatalf("ApplyResources error: %v", err)
	}

	r := rsp.Desired.Resources["item"]
	// When no connection_details provided, ConnectionDetails should be nil
	if r.ConnectionDetails != nil {
		t.Error("ConnectionDetails should be nil when not provided")
	}
}
