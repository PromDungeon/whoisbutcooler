package main

import (
	"strings"
	"testing"

	"github.com/PromDungeon/whoisbutcooler/internal/lookup"
)

func TestParseArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		isTTY   bool
		wantQ   string
		wantOne bool
		wantErr bool
	}{
		{name: "bare interactive", args: nil, isTTY: true},
		{name: "query interactive", args: []string{"8.8.8.8"}, isTTY: true, wantQ: "8.8.8.8"},
		{name: "explicit once", args: []string{"--once", "8.8.8.8"}, isTTY: true, wantQ: "8.8.8.8", wantOne: true},
		// A non-TTY stdout means output is being piped, so there is nobody to
		// type at a prompt.
		{name: "piped with query", args: []string{"1.1.1.1"}, isTTY: false, wantQ: "1.1.1.1", wantOne: true},
		{name: "piped without query", args: nil, isTTY: false, wantErr: true},
		{name: "once without query", args: []string{"--once"}, isTTY: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, once, err := parseArgs(tc.args, tc.isTTY)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if q != tc.wantQ || once != tc.wantOne {
				t.Fatalf("got (%q, %v), want (%q, %v)", q, once, tc.wantQ, tc.wantOne)
			}
		})
	}
}

func TestRenderOnceExplainsAPrivateAddressLikeThePrompt(t *testing.T) {
	// --once used to print the raw wrapped error, "192.168.1.1: address is in
	// a private or reserved range", while the interactive prompt explained
	// itself. A private address is rejected before any request is made, so
	// this touches no network.
	err := renderOnce(&lookup.Client{}, "192.168.1.1")
	if err == nil {
		t.Fatal("a private address rendered as a successful lookup")
	}
	if !strings.Contains(err.Error(), "no public database can place it") {
		t.Errorf("--once message = %q, want the explanation the prompt shows", err)
	}
	if strings.HasPrefix(err.Error(), "192.168.1.1:") {
		t.Errorf("--once message = %q, want no raw wrapped error", err)
	}
}
