package main

import (
	"io"
	"strings"
	"testing"
)

// An unknown -mem value is a usage error, not a silent fallback to one of the
// two real modes.
func TestParseArgsRejectsUnknownMemoryMode(t *testing.T) {
	_, _, err := parseArgs([]string{"-mem", "sideways"}, io.Discard)
	if err == nil {
		t.Fatal("parseArgs accepted -mem sideways; want a usage error")
	}
	if !strings.Contains(err.Error(), "high|low") {
		t.Errorf("error %q does not name the valid values", err)
	}
}

func TestParseArgsRejectsUnknownWorldType(t *testing.T) {
	_, _, err := parseArgs([]string{"-world-type", "ironman"}, io.Discard)
	if err == nil {
		t.Fatal("parseArgs accepted -world-type ironman; want a usage error")
	}
	if !strings.Contains(err.Error(), "free|members") {
		t.Errorf("error %q does not name the valid values", err)
	}
}

// The two enums are the only flags whose spelling changes behaviour rather
// than a value, so pin both directions of each.
func TestParseArgsMapsEnumsToBooleans(t *testing.T) {
	for _, tc := range []struct {
		args      []string
		lowMemory bool
		members   bool
	}{
		{nil, false, true}, // defaults: -mem high, -world-type members
		{[]string{"-mem", "low"}, true, true},
		{[]string{"-mem", "high"}, false, true},
		{[]string{"-world-type", "free"}, false, false},
		{[]string{"-mem", "low", "-world-type", "free"}, true, false},
	} {
		opts, done, err := parseArgs(tc.args, io.Discard)
		if err != nil || done {
			t.Fatalf("parseArgs(%q): err=%v done=%v", tc.args, err, done)
		}
		if opts.lowMemory != tc.lowMemory || opts.members != tc.members {
			t.Errorf("parseArgs(%q): lowMemory=%v members=%v, want %v/%v",
				tc.args, opts.lowMemory, opts.members, tc.lowMemory, tc.members)
		}
	}
}

// --version must report and stop. If done were false the caller would go on
// to boot a server the user never asked for.
func TestParseArgsVersionReportsAndStops(t *testing.T) {
	var out strings.Builder
	opts, done, err := parseArgs([]string{"-version"}, &out)
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !done {
		t.Error("done=false for -version; the game would start anyway")
	}
	if opts != nil {
		t.Error("options returned alongside done; nothing should use them")
	}
	if out.Len() == 0 {
		t.Error("-version printed nothing")
	}
}

// ResolveCacheDir needs "the user named this directory" distinguished from
// "nobody said, so the embedded bundle may win" — a default value cannot
// carry that, only flag.Visit can.
func TestParseArgsDistinguishesExplicitCacheDir(t *testing.T) {
	opts, _, err := parseArgs(nil, io.Discard)
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if opts.explicitCacheDir {
		t.Error("explicitCacheDir true with no -cache-dir passed")
	}

	// Passing the default value explicitly must still count as explicit.
	opts, _, err = parseArgs([]string{"-cache-dir", "./data/pack"}, io.Discard)
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !opts.explicitCacheDir {
		t.Error("explicitCacheDir false when -cache-dir was passed with its default value")
	}
}

// A bad flag must not be reported to stderr by the flag package and then
// also crash: ContinueOnError means parseArgs owns the outcome.
func TestParseArgsReturnsErrorForUnknownFlag(t *testing.T) {
	if _, _, err := parseArgs([]string{"-nonsuch"}, io.Discard); err == nil {
		t.Fatal("parseArgs accepted an unknown flag")
	}
}
