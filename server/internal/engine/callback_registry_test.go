package engine

import "testing"

func TestCallbackRegistryRevocationIsScopedAndIdempotent(t *testing.T) {
	e := &Engine{execs: newExecRegistry()}
	first := e.execs.register("first", &turnEnv{})
	second := e.execs.register("second", &turnEnv{})
	if _, _, err := e.Callback("first", first); err != nil {
		t.Fatal(err)
	}
	e.execs.unregister("first")
	e.execs.unregister("first")
	if _, _, err := e.Callback("first", first); err != ErrBadCallback {
		t.Fatal("revoked token accepted", err)
	}
	if _, _, err := e.Callback("second", second); err != nil {
		t.Fatal("other execution revoked", err)
	}
	if _, _, err := e.Callback("second", first); err != ErrBadCallback {
		t.Fatal("cross-execution token accepted", err)
	}
}
