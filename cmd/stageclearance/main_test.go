package main

import "testing"

func TestResolveAddress(t *testing.T) {
	tests := []struct {
		explicit, port, want string
		bad                  bool
	}{{"", "", "127.0.0.1:19081", false}, {"", "19123", "127.0.0.1:19123", false}, {"127.0.0.1:19222", "19123", "127.0.0.1:19222", false}, {"0.0.0.0:19081", "", "", true}, {"", "invalid", "", true}}
	for _, test := range tests {
		got, err := resolveAddress(test.explicit, test.port)
		if test.bad && err == nil {
			t.Fatalf("expected error for %#v", test)
		}
		if !test.bad && (err != nil || got != test.want) {
			t.Fatalf("got %q, %v, want %q", got, err, test.want)
		}
	}
}
