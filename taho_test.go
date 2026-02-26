package main

import "testing"

// When invoked without any arguments, we do not expect any output from
// HandleArgs
func TestHandleNoArgs(t *testing.T) {
	taho := NewTahoWithMockProxy()
	taho.HandleArgs()
}

// When invoked with an unknown argument, we expect handle args to produce a
// Fatalf with a specific message.
func TestHandleUnknownArg(t *testing.T) {
	taho := NewTahoWithMockProxy()
	taho.proxy.args = []string{"test", "unknown"}
	taho.proxy.expected = append(taho.proxy.expected, []string{"Fatalf", "Unable to handle argumet \"%s\"", "unknown"})
	taho.HandleArgs()
}
