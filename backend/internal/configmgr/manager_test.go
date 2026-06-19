package configmgr

import (
	"path/filepath"
	"testing"
)

func TestSafePath(t *testing.T) {
	m := &Manager{root: t.TempDir()}
	if _, err := m.SafePath("server.properties"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"../server.properties", "/etc/passwd", "foo.env", "world/x"} {
		if _, err := m.SafePath(p); err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	abs, _ := filepath.Abs("server.properties")
	if _, err := m.SafePath(abs); err == nil {
		t.Fatal("accepted absolute path")
	}
}
func TestValidateServerProperties(t *testing.T) {
	if err := ValidateServerProperties("server-name=Test\nmax-players=10\n"); err != nil {
		t.Fatal(err)
	}
	if ValidateServerProperties("bad line") == nil {
		t.Fatal("accepted bad line")
	}
	if ValidateServerProperties("server-port=99999") == nil {
		t.Fatal("accepted bad port")
	}
}
func TestValidateJSON(t *testing.T) {
	if ValidateJSON(`[{"name":"Steve"}]`) != nil {
		t.Fatal("valid rejected")
	}
	if ValidateJSON(`{`) == nil {
		t.Fatal("invalid accepted")
	}
}
