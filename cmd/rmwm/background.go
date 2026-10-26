package main

import "strconv"

func monitorWorkerArguments(cfg config) []string {
	args := []string{"-bg", "-bg-worker", "-format=" + cfg.format,
		"-max-size=" + strconv.FormatInt(cfg.maxSizeMiB, 10)}
	if !cfg.NormalizeSpaces {
		args = append(args, "-no-normalize-spaces")
	}
	if !cfg.NormalizeDashes {
		args = append(args, "-no-normalize-dashes")
	}
	if cfg.StripEmojiGlue {
		args = append(args, "-strip-emoji-glue")
	}
	if cfg.StripBidi {
		args = append(args, "-strip-bidi")
	}
	if cfg.AggressiveHomoglyphs {
		args = append(args, "-aggressive-homoglyphs")
	}
	return args
}
