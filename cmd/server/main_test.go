package main

import (
	"errors"
	"net"
	"net/http"
	"testing"
)

func TestServeShutdownErrorsAreExpected(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "http server closed", err: http.ErrServerClosed},
		{name: "listener closed", err: net.ErrClosed},
		{name: "wrapped listener closed", err: errors.Join(errors.New("serve failed"), net.ErrClosed)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !isExpectedServeClose(test.err) {
				t.Fatalf("isExpectedServeClose(%v) = false, want true", test.err)
			}
		})
	}

	if isExpectedServeClose(errors.New("unexpected serve failure")) {
		t.Fatal("unexpected serve failure was treated as an expected shutdown")
	}
}
