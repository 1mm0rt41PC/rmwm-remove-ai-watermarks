package main

import (
	rmwm "github.com/1mm0rt41PC/rmwm-remove-ai-watermarks"
	"testing"
)

func TestMonitorWorkerOptionsRoundTripThroughCLI(t *testing.T) {
	// Exercise the actual CLI parser, with an injected worker instead of Win32.
	// Detaching must retain every cleaner option and the content/size policy.
	for _, want := range []config{
		{CleanOptions: rmwm.CleanOptions{NormalizeSpaces: true, NormalizeDashes: true}, format: "auto", maxSizeMiB: 64},
		{CleanOptions: rmwm.CleanOptions{NormalizeSpaces: true, NormalizeDashes: false}, format: "text", maxSizeMiB: 64},
		{CleanOptions: rmwm.CleanOptions{NormalizeSpaces: false, NormalizeDashes: true}, format: "text", maxSizeMiB: 64},
		{CleanOptions: rmwm.CleanOptions{StripEmojiGlue: true, StripBidi: true, AggressiveHomoglyphs: true}, format: "json", maxSizeMiB: 7},
	} {
		called := false
		result := runClipboardTest(monitorWorkerArguments(want), clipboardOps{
			monitor: func(got config) error {
				called = true
				if got.CleanOptions != want.CleanOptions || got.format != want.format || got.maxBytes != want.maxSizeMiB*1024*1024 {
					t.Errorf("worker policy = %+v; want %+v", got, want)
				}
				if !got.background || !got.backgroundWorker || got.backgroundStop {
					t.Error("worker arguments did not select the internal background worker")
				}
				return nil
			},
		})
		rmwmTestCode(t, result, 0)
		if !called || len(result.stdout) != 0 || len(result.stderr) != 0 {
			t.Fatalf("background worker dispatch or silence failed: called=%t stdout=%q stderr=%q", called, result.stdout, result.stderr)
		}
	}
}
