package main

import "testing"

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
