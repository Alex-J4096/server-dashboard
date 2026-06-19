package server

import "testing"

func TestStates(t *testing.T) {
	for _, s := range []State{StateStopped, StateStarting, StateRunning, StateStopping, StateCrashed} {
		if s == "" {
			t.Fatal("empty state")
		}
	}
}
