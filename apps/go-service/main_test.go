package main

import "testing"

func TestGreet(t *testing.T) {
	got := Greet("Bazel")
	want := "Hello from Go, Bazel!"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
