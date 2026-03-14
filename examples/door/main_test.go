package main

import (
	"reflect"
	"testing"
)

func TestRunDemo(t *testing.T) {
	lines, err := runDemo()
	if err != nil {
		t.Fatalf("runDemo() error = %v", err)
	}

	want := []string{
		"stop scenario after start: running",
		"stop scenario final: idle",
		"timeout scenario after start: running",
		"door-2: cycle took too long",
		"timeout scenario final: fault",
	}

	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("runDemo() = %#v, want %#v", lines, want)
	}
}
