package rmwm_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	rmwm "github.com/1mm0rt41PC/rmwm-remove-ai-watermarks"
)

// ************************************************************************************************
// TestFacadeStreams exercises all stream wrappers through the public import.
func TestFacadeStreams(t *testing.T) {
	input := "a\u200bb—c"
	opt := rmwm.Opt{NormalizeDashes: true}
	if found, err := rmwm.HasWatermarks(strings.NewReader(input), opt); err != nil || !found {
		t.Errorf("HasWatermarks = %v, %v", found, err)
	}
	parts, err := rmwm.RunDry(strings.NewReader(input), opt)
	if err != nil || len(parts) != 1 || parts[0] != input+"\n---\nab-c" {
		t.Errorf("RunDry = %q, %v", parts, err)
	}
	var output bytes.Buffer
	if err := rmwm.CleanupWatermarks(strings.NewReader(input), &output, opt); err != nil || output.String() != "ab-c" {
		t.Errorf("CleanupWatermarks = %q, %v", output.String(), err)
	}
}

// ************************************************************************************************
// TestFacadeCleaners exercises both string wrappers and public statistics.
func TestFacadeCleaners(t *testing.T) {
	opt := rmwm.CleanOptions{NormalizeDashes: true}
	text, stats := rmwm.CleanText("a\u200bb—c", opt)
	want := rmwm.CleanStats{Removed: 1, Replaced: 1, InputRunes: 5, OutputRunes: 4}
	if text != "ab-c" || stats != want {
		t.Errorf("CleanText = %q, %+v; want ab-c, %+v", text, stats, want)
	}
	text, stats, err := rmwm.CleanJSON(`"a\u200bb—c"`, opt)
	if err != nil || text != `"ab-c"` || stats.Removed != 1 || stats.Replaced != 1 {
		t.Errorf("CleanJSON = %q, %+v, %v", text, stats, err)
	}
}

// ************************************************************************************************
// TestFacadeSentinels verifies errors returned internally match public sentinels.
func TestFacadeSentinels(t *testing.T) {
	if _, err := rmwm.RunDry(nil, rmwm.Opt{}); !errors.Is(err, rmwm.ErrNilArgument) {
		t.Errorf("RunDry nil reader: %v", err)
	}
	if _, err := rmwm.HasWatermarks(nil, rmwm.Opt{}); !errors.Is(err, rmwm.ErrNilArgument) {
		t.Errorf("HasWatermarks nil reader: %v", err)
	}
	if err := rmwm.CleanupWatermarks(strings.NewReader("ok"), nil, rmwm.Opt{}); !errors.Is(err, rmwm.ErrNilArgument) {
		t.Errorf("CleanupWatermarks nil writer: %v", err)
	}
	opt := rmwm.Opt{Format: "xml"}
	if _, err := rmwm.RunDry(strings.NewReader("ok"), opt); !errors.Is(err, rmwm.ErrInvalidFormat) {
		t.Errorf("RunDry invalid format: %v", err)
	}
	if _, err := rmwm.HasWatermarks(strings.NewReader("ok"), opt); !errors.Is(err, rmwm.ErrInvalidFormat) {
		t.Errorf("HasWatermarks invalid format: %v", err)
	}
	var output bytes.Buffer
	if err := rmwm.CleanupWatermarks(strings.NewReader("ok"), &output, opt); !errors.Is(err, rmwm.ErrInvalidFormat) || output.Len() != 0 {
		t.Errorf("CleanupWatermarks invalid format: %v, output=%q", err, output.String())
	}
}
