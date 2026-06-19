package logs

import "testing"

func TestParseLevel(t *testing.T) {
	cases := map[string]string{"hello": "info", "WARN lag": "warn", "fatal crash": "error"}
	for in, want := range cases {
		if got := ParseLevel("stdout", in); got != want {
			t.Fatalf("%q: %s", in, got)
		}
	}
	if ParseLevel("stderr", "anything") != "error" {
		t.Fatal("stderr must be error")
	}
}
