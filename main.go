// rmwm is a local Unicode text cleaner with file, stream and clipboard inputs.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var version = "0.3.0"

type config struct {
	CleanOptions
	extensions       string
	extensionSet     map[string]bool
	output           string
	format           string
	check            bool
	dryRun           bool
	noBackup         bool
	noRecursive      bool
	quiet            bool
	stats            bool
	maxSizeMiB       int64
	maxBytes         int64
	background       bool
	backgroundWorker bool
	backgroundStop   bool
}

type report struct {
	Scanned  int `json:"files_scanned"`
	Changed  int `json:"files_changed"`
	Written  int `json:"files_written"`
	Skipped  int `json:"entries_skipped"`
	Removed  int `json:"characters_removed"`
	Replaced int `json:"characters_replaced"`
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
}

type application struct {
	cfg       config
	stdout    io.Writer
	stderr    io.Writer
	clipboard clipboardOps
	report    report
	seen      map[string]bool
}

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

const helpText = `rmwm - nettoyage Unicode local du texte et des chaînes JSON

Utilisation :
  rmwm C:\my\dir
  rmwm C:\my\dir -ext="md,json,txt"
  rmwm C:\my\dir\myfile.md
  cat myfile.md | rmwm -
  rmwm -format=json - < input.json > output.json
  rmwm "~"
  rmwm -bg
  rmwm -bg-stop

Un fichier est modifié sur place, avec une sauvegarde .bak si nécessaire.
Un dossier est parcouru récursivement ; extensions par défaut : md,json.
Avec -, le texte nettoyé est écrit sur stdout, sans ajout de fin de ligne.
Avec "~", le presse-papiers est nettoyé sur place en texte brut.
Sous Linux, citer '~' pour éviter son expansion en dossier personnel.
Avec -bg (Windows), surveillance avec confirmation native Oui/Non.
Par défaut, les tirets —, ⸺ et ⸻ deviennent chacun un tiret ASCII -.
Les options acceptent un ou deux tirets, avant ou après les chemins.

Options :
  -ext="md,json"           Extensions en mode dossier, avec ou sans point
  -o CHEMIN               Écrire un nouveau fichier ; - pour stdout
  -format=auto            auto (extension), text ou json ; stdin/clipboard = text
  -bg                     Windows : surveillance du presse-papiers en fond
  -bg-stop                Windows : arrêter la surveillance de cette session
  -check                  Vérifier sans écrire : 1 si nettoyage nécessaire
  -dry-run                Aperçu sans écriture ; code 0 hors erreur
  -no-backup              Ne pas conserver de sauvegarde .bak
  -no-recursive           Traiter seulement la racine du dossier
  -no-normalize-spaces    Conserver les espaces Unicode particuliers
  -no-normalize-dashes    Conserver les tirets —, ⸺ et ⸻
  -strip-bidi             Retirer aussi les contrôles directionnels légitimes
  -strip-emoji-glue       Retirer aussi les invisibles utiles aux emojis/langues
  -aggressive-homoglyphs  Convertir certains caractères ressemblants en ASCII
  -max-size=64            Taille maximale par fichier/flux/clipboard, en Mio
  -stats                  Bilan JSON sur stderr
  -quiet, -q              Masquer les messages ordinaires
  -version               Afficher la version
  -help, -h               Afficher cette aide

Codes de sortie : 0 = succès ; 1 = -check avec changements ; 2 = erreur.
Les dossiers .git, .hg, .svn, node_modules, vendor, .venv et venv sont ignorés.
Les liens symboliques ne sont pas suivis. Les entrées binaires sont refusées.
Encodages : UTF-8 (avec/sans BOM) et UTF-16 LE/BE avec BOM, conservés.
Clipboard : Windows natif ; Linux Wayland (wl-clipboard), X11 (xclip).
Copies d'images/fichiers ignorées ; un nettoyage remplace les formats riches
par du texte brut. Aucun contenu du presse-papiers n'est écrit sur stdout.
`

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runWithClipboard(args, stdin, stdout, stderr, systemClipboardOps())
}

func runWithClipboard(args []string, stdin io.Reader, stdout, stderr io.Writer, clipboard clipboardOps) int {
	cfg := config{CleanOptions: CleanOptions{NormalizeSpaces: true, NormalizeDashes: true}}
	flags := flag.NewFlagSet("rmwm", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { _, _ = io.WriteString(stderr, helpText) }
	var help, showVersion, keepSpaces, keepDashes bool
	flags.StringVar(&cfg.extensions, "ext", "md,json", "extensions en mode dossier")
	flags.StringVar(&cfg.output, "o", "", "fichier de sortie")
	flags.StringVar(&cfg.output, "output", "", "fichier de sortie")
	flags.StringVar(&cfg.format, "format", "auto", "auto, text ou json")
	flags.BoolVar(&cfg.check, "check", false, "vérifier sans écrire")
	flags.BoolVar(&cfg.dryRun, "dry-run", false, "aperçu sans écriture")
	flags.BoolVar(&cfg.noBackup, "no-backup", false, "sans sauvegarde")
	flags.BoolVar(&cfg.noRecursive, "no-recursive", false, "sans sous-dossiers")
	flags.BoolVar(&cfg.quiet, "quiet", false, "silencieux")
	flags.BoolVar(&cfg.quiet, "q", false, "silencieux")
	flags.BoolVar(&cfg.stats, "stats", false, "bilan JSON")
	flags.BoolVar(&cfg.background, "bg", false, "surveiller le presse-papiers Windows")
	flags.BoolVar(&cfg.backgroundStop, "bg-stop", false, "arrêter la surveillance Windows")
	// Internal worker flag: -bg starts this process without a console window.
	flags.BoolVar(&cfg.backgroundWorker, "bg-worker", false, "processus de surveillance interne")
	flags.BoolVar(&keepSpaces, "no-normalize-spaces", false, "conserver les espaces Unicode")
	flags.BoolVar(&keepDashes, "no-normalize-dashes", false, "conserver les tirets —, ⸺ et ⸻")
	flags.BoolVar(&cfg.StripBidi, "strip-bidi", false, "retirer tous les bidi")
	flags.BoolVar(&cfg.StripEmojiGlue, "strip-emoji-glue", false, "retirer les invisibles utiles")
	flags.BoolVar(&cfg.AggressiveHomoglyphs, "aggressive-homoglyphs", false, "normaliser les caractères ressemblants")
	flags.Int64Var(&cfg.maxSizeMiB, "max-size", 64, "limite en Mio")
	flags.BoolVar(&help, "help", false, "aide")
	flags.BoolVar(&help, "h", false, "aide")
	flags.BoolVar(&showVersion, "version", false, "version")
	ordered, err := interspersedArgs(flags, args)
	if err != nil {
		fmt.Fprintf(stderr, "rmwm: %v\n", err)
		return 2
	}
	if err = flags.Parse(ordered); err != nil {
		return 2
	}
	if help {
		_, err = io.WriteString(stdout, helpText)
		if err != nil {
			fmt.Fprintf(stderr, "rmwm: %v\n", err)
			return 2
		}
		return 0
	}
	if showVersion {
		_, err = fmt.Fprintf(stdout, "rmwm %s\n", version)
		if err != nil {
			fmt.Fprintf(stderr, "rmwm: %v\n", err)
			return 2
		}
		return 0
	}
	paths := flags.Args()
	usageError := func(message string) int {
		fmt.Fprintf(stderr, "rmwm: %s\nConsultez rmwm -help.\n", message)
		return 2
	}
	if cfg.backgroundWorker && !cfg.background {
		return usageError("-bg-worker est réservé au processus interne de -bg")
	}
	if cfg.background && cfg.backgroundStop {
		return usageError("-bg et -bg-stop sont mutuellement exclusifs")
	}
	backgroundMode := cfg.background || cfg.backgroundStop
	if backgroundMode && (len(paths) != 0 || cfg.output != "" || cfg.check || cfg.dryRun || cfg.stats) {
		return usageError("-bg/-bg-stop s'utilisent seuls, sans chemin, ~, -, -o, -check, -dry-run ni -stats")
	}
	if len(paths) == 0 && !backgroundMode {
		return usageError("indiquez un fichier, un dossier, - pour stdin ou ~ pour le presse-papiers")
	}
	if cfg.check && cfg.dryRun {
		return usageError("-check et -dry-run sont mutuellement exclusifs")
	}
	if cfg.output != "" && (len(paths) != 1 || cfg.check || cfg.dryRun) {
		return usageError("-o exige une entrée unique et ne se combine pas avec -check/-dry-run")
	}
	if cfg.format != "auto" && cfg.format != "text" && cfg.format != "json" {
		return usageError("-format doit valoir auto, text ou json")
	}
	if cfg.maxSizeMiB <= 0 || cfg.maxSizeMiB > (1<<43)-1 {
		return usageError("-max-size doit être un entier positif exprimé en Mio")
	}
	cfg.maxBytes = cfg.maxSizeMiB * 1024 * 1024
	cfg.NormalizeSpaces = !keepSpaces
	cfg.NormalizeDashes = !keepDashes
	cfg.extensionSet, err = parseExtensions(cfg.extensions)
	if err != nil {
		return usageError(err.Error())
	}
	for _, path := range paths {
		if path == "-" && len(paths) != 1 {
			return usageError("stdin (-) ne peut pas être mélangé avec des chemins")
		}
		if path == "~" && (len(paths) != 1 || cfg.output != "") {
			return usageError("le presse-papiers (~) exige une entrée unique et ne se combine pas avec -o")
		}
	}
	if backgroundMode {
		return runBackgroundCommand(cfg, clipboard, stderr)
	}
	a := application{cfg: cfg, stdout: stdout, stderr: stderr, clipboard: clipboard, seen: make(map[string]bool)}
	if paths[0] == "-" {
		a.processStream(stdin)
	} else if paths[0] == "~" {
		a.processClipboard()
	} else {
		for _, path := range paths {
			a.processPath(path)
		}
	}
	if cfg.stats {
		if err := json.NewEncoder(stderr).Encode(a.report); err != nil {
			return 2
		}
	} else if !cfg.quiet && (paths[0] != "-" || cfg.check || cfg.dryRun) {
		fmt.Fprintf(stderr, "rmwm: %d entrée(s) analysée(s), %d à nettoyer, %d écrite(s), %d erreur(s)\n",
			a.report.Scanned, a.report.Changed, a.report.Written, a.report.Errors)
	}
	if a.report.Errors > 0 {
		return 2
	}
	if cfg.check && a.report.Changed > 0 {
		return 1
	}
	return 0
}

// The standard flag package stops at the first positional argument. Moving
// recognized flags ahead of positionals permits `rmwm dir -ext=md,json` too.
// An explicit -- stops option handling, including for filenames starting in -.
func interspersedArgs(flags *flag.FlagSet, args []string) ([]string, error) {
	var options, paths []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			paths = append(paths, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			paths = append(paths, arg)
			continue
		}
		nameValue := strings.TrimPrefix(arg, "-")
		nameValue = strings.TrimPrefix(nameValue, "-")
		name, _, hasValue := strings.Cut(nameValue, "=")
		f := flags.Lookup(name)
		if f == nil {
			return nil, fmt.Errorf("option inconnue : %s", arg)
		}
		options = append(options, arg)
		boolFlag, isBool := f.Value.(interface{ IsBoolFlag() bool })
		if !hasValue && !(isBool && boolFlag.IsBoolFlag()) {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("valeur manquante pour %s", arg)
			}
			i++
			options = append(options, args[i])
		}
	}
	return append(append(options, "--"), paths...), nil
}

func parseExtensions(value string) (map[string]bool, error) {
	result := make(map[string]bool)
	for _, part := range strings.Split(value, ",") {
		ext := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(part), "."))
		if ext == "" {
			return nil, errors.New("-ext contient une extension vide")
		}
		for _, r := range ext {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
				return nil, fmt.Errorf("extension invalide dans -ext : %q", part)
			}
		}
		result["."+ext] = true
	}
	return result, nil
}

func (a *application) error(path string, err error) {
	a.report.Errors++
	fmt.Fprintf(a.stderr, "rmwm: %q: %v\n", path, err)
}

func (a *application) note(format string, args ...any) {
	if !a.cfg.quiet && !a.cfg.stats {
		fmt.Fprintf(a.stderr, format+"\n", args...)
	}
}

func (a *application) collect(changed bool, stats CleanStats) {
	a.report.Scanned++
	if changed {
		a.report.Changed++
	}
	a.report.Removed += stats.Removed
	a.report.Replaced += stats.Replaced
}

var excludedDirectories = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true,
	"vendor": true, ".venv": true, "venv": true,
}

func (a *application) processPath(path string) {
	info, err := os.Lstat(path)
	if err != nil {
		a.error(path, err)
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		a.error(path, errors.New("lien symbolique refusé"))
		return
	}
	if !info.IsDir() {
		a.processFile(path)
		return
	}
	if a.cfg.output != "" {
		a.error(path, errors.New("-o ne peut pas être utilisé avec un dossier"))
		return
	}
	root := filepath.Clean(path)
	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			a.error(current, walkErr)
			return nil // Keep processing independent files after an error.
		}
		if entry.Type()&os.ModeSymlink != 0 {
			a.report.Skipped++
			return nil
		}
		if entry.IsDir() {
			if current != root && (a.cfg.noRecursive || excludedDirectories[strings.ToLower(entry.Name())]) {
				a.report.Skipped++
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || !a.cfg.extensionSet[strings.ToLower(filepath.Ext(current))] {
			a.report.Skipped++
			return nil
		}
		a.processFile(current)
		return nil
	})
	if err != nil {
		a.error(path, err)
	}
}

func (a *application) clean(data []byte, name string) ([]byte, CleanStats, error) {
	text, encoding, err := decodeText(data)
	if err != nil {
		return nil, CleanStats{}, err
	}
	format := a.cfg.format
	if format == "auto" {
		format = "text"
		if strings.EqualFold(filepath.Ext(name), ".json") {
			format = "json"
		}
	}
	var cleaned string
	var stats CleanStats
	if format == "json" {
		cleaned, stats, err = CleanJSON(text, a.cfg.CleanOptions)
	} else {
		cleaned, stats = CleanText(text, a.cfg.CleanOptions)
	}
	if err != nil {
		return nil, CleanStats{}, err
	}
	if text == cleaned {
		return data, stats, nil
	}
	return encodeText(cleaned, encoding), stats, nil
}

func (a *application) processStream(stdin io.Reader) {
	data, err := readLimited(stdin, a.cfg.maxBytes)
	if err != nil {
		a.error("stdin", err)
		return
	}
	cleaned, stats, err := a.clean(data, "-")
	if err != nil {
		a.error("stdin", err)
		return
	}
	changed := !bytes.Equal(data, cleaned)
	a.collect(changed, stats)
	if a.cfg.check || a.cfg.dryRun {
		if changed {
			a.note("[à nettoyer] stdin (retirés=%d, remplacés=%d)", stats.Removed, stats.Replaced)
		}
		return
	}
	if a.cfg.output != "" && a.cfg.output != "-" {
		if err = writeNewFile(a.cfg.output, cleaned, 0600); err != nil {
			a.error(a.cfg.output, err)
			return
		}
		a.report.Written++
		a.note("[écrit] %s", a.cfg.output)
	} else if err = writeAll(a.stdout, cleaned); err != nil {
		a.error("stdout", err)
	}
}

func (a *application) processFile(path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		a.error(path, err)
		return
	}
	key := abs
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	if a.seen[key] {
		return
	}
	a.seen[key] = true
	source, err := readSource(abs, a.cfg.maxBytes)
	if err != nil {
		a.error(path, err)
		return
	}
	cleaned, stats, err := a.clean(source.data, path)
	if err != nil {
		a.error(path, err)
		return
	}
	changed := !bytes.Equal(source.data, cleaned)
	a.collect(changed, stats)
	if a.cfg.check || a.cfg.dryRun {
		if changed {
			a.note("[à nettoyer] %s (retirés=%d, remplacés=%d)", path, stats.Removed, stats.Replaced)
		}
		return
	}
	if a.cfg.output == "-" {
		if err := writeAll(a.stdout, cleaned); err != nil {
			a.error("stdout", err)
		}
		return
	}
	if a.cfg.output != "" {
		out, err := filepath.Abs(a.cfg.output)
		if err != nil {
			a.error(a.cfg.output, err)
			return
		}
		equalPath := out == abs || runtime.GOOS == "windows" && strings.EqualFold(out, abs)
		if !equalPath {
			if err := writeNewFileProtected(out, cleaned, source.info.Mode().Perm(), source.path); err != nil {
				a.error(a.cfg.output, err)
				return
			}
			a.report.Written++
			a.note("[écrit] %s", a.cfg.output)
			return
		}
	}
	if !changed {
		return // Preserve timestamps and avoid backups for unchanged files.
	}
	backup, warning, err := replaceSource(source, cleaned, !a.cfg.noBackup)
	if err != nil {
		a.error(path, err)
		return
	}
	a.report.Written++
	if warning != "" {
		a.report.Warnings++
		fmt.Fprintf(a.stderr, "rmwm: %s\n", warning)
	}
	if backup != "" {
		a.note("[nettoyé] %s (retirés=%d, remplacés=%d ; sauvegarde=%s)", path, stats.Removed, stats.Replaced, backup)
	} else {
		a.note("[nettoyé] %s (retirés=%d, remplacés=%d)", path, stats.Removed, stats.Replaced)
	}
}
