package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf16"
	"unicode/utf8"
)

type textEncoding uint8

const (
	encodingUTF8 textEncoding = iota
	encodingUTF8BOM
	encodingUTF16LE
	encodingUTF16BE
)

func decodeText(data []byte) (string, textEncoding, error) {
	encoding := encodingUTF8
	if bytes.HasPrefix(data, []byte{0xFF, 0xFE, 0, 0}) || bytes.HasPrefix(data, []byte{0, 0, 0xFE, 0xFF}) {
		return "", encoding, errors.New("UTF-32 non pris en charge ; convertir en UTF-8")
	}
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		encoding, data = encodingUTF8BOM, data[3:]
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		encoding, data = encodingUTF16LE, data[2:]
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		encoding, data = encodingUTF16BE, data[2:]
	}
	var text string
	if encoding == encodingUTF16LE || encoding == encodingUTF16BE {
		if len(data)%2 != 0 {
			return "", encoding, errors.New("UTF-16 tronqué : nombre d'octets impair")
		}
		var order binary.ByteOrder = binary.LittleEndian
		if encoding == encodingUTF16BE {
			order = binary.BigEndian
		}
		words := make([]uint16, len(data)/2)
		for i := range words {
			words[i] = order.Uint16(data[i*2 : i*2+2])
		}
		for i := 0; i < len(words); i++ {
			w := words[i]
			if w >= 0xD800 && w <= 0xDBFF {
				if i+1 >= len(words) || words[i+1] < 0xDC00 || words[i+1] > 0xDFFF {
					return "", encoding, errors.New("UTF-16 invalide : surrogate haute non appariée")
				}
				i++
			} else if w >= 0xDC00 && w <= 0xDFFF {
				return "", encoding, errors.New("UTF-16 invalide : surrogate basse non appariée")
			}
		}
		text = string(utf16.Decode(words))
	} else {
		if !utf8.Valid(data) {
			return "", encoding, errors.New("UTF-8 invalide ou fichier binaire ; UTF-16 exige un BOM")
		}
		text = string(data)
	}
	if err := validateText(text); err != nil {
		return "", encoding, err
	}
	return text, encoding, nil
}

func validateText(text string) error {
	// Some containers are ASCII-compatible; valid UTF-8 alone is insufficient.
	for _, signature := range []string{"%PDF-", "PK\x03\x04", "PK\x05\x06", "PK\x07\x08", "GIF87a", "GIF89a", "RIFF", "\x89PNG\r\n\x1a\n"} {
		if len(text) >= len(signature) && text[:len(signature)] == signature {
			return errors.New("fichier binaire/conteneur refusé : rmwm traite le texte")
		}
	}
	for _, r := range text {
		if r == 0 || r == 0x7F || r < 0x20 && r != '\t' && r != '\n' && r != '\r' && r != '\f' {
			return fmt.Errorf("entrée binaire ou contrôle non textuel U+%04X refusé", r)
		}
	}
	return nil
}

func encodeText(text string, encoding textEncoding) []byte {
	switch encoding {
	case encodingUTF8BOM:
		return append([]byte{0xEF, 0xBB, 0xBF}, []byte(text)...)
	case encodingUTF16LE, encodingUTF16BE:
		words := utf16.Encode([]rune(text))
		out := make([]byte, 2+2*len(words))
		var order binary.ByteOrder = binary.LittleEndian
		out[0], out[1] = 0xFF, 0xFE
		if encoding == encodingUTF16BE {
			order = binary.BigEndian
			out[0], out[1] = 0xFE, 0xFF
		}
		for i, w := range words {
			order.PutUint16(out[2+i*2:4+i*2], w)
		}
		return out
	default:
		return []byte(text)
	}
}

func readLimited(reader io.Reader, maximum int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("taille supérieure à la limite de %d octets (-max-size)", maximum)
	}
	return data, nil
}

func writeAll(writer io.Writer, data []byte) error {
	n, err := writer.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

type sourceSnapshot struct {
	path string
	info os.FileInfo
	data []byte
}

func readSource(path string, maximum int64) (sourceSnapshot, error) {
	var result sourceSnapshot
	info, err := os.Lstat(path)
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() {
		return result, errors.New("seuls les fichiers réguliers sont acceptés ; liens refusés")
	}
	if info.Size() > maximum {
		return result, fmt.Errorf("fichier trop volumineux : %d octets, limite %d (-max-size)", info.Size(), maximum)
	}
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return result, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return result, errors.New("le fichier a changé pendant son ouverture")
	}
	data, err := readLimited(file, maximum)
	if err != nil {
		return result, err
	}
	after, err := file.Stat()
	if err != nil {
		return result, err
	}
	if opened.Size() != after.Size() || !opened.ModTime().Equal(after.ModTime()) || int64(len(data)) != after.Size() {
		return result, errors.New("le fichier a changé pendant la lecture")
	}
	return sourceSnapshot{path: path, info: after, data: data}, nil
}

func sourceUnchanged(source sourceSnapshot) error {
	current, err := readSource(source.path, int64(len(source.data))+1)
	if err != nil {
		return fmt.Errorf("vérification avant écriture : %w", err)
	}
	if !os.SameFile(source.info, current.info) || !bytes.Equal(source.data, current.data) ||
		!source.info.ModTime().Equal(current.info.ModTime()) || source.info.Mode() != current.info.Mode() {
		return errors.New("le fichier a changé depuis sa lecture ; aucune substitution effectuée")
	}
	return nil
}

// New files and backups are created with O_EXCL. Existing files and symlinks
// cannot be overwritten through either path.
func writeNewFile(path string, data []byte, mode os.FileMode) (err error) {
	return writeNewFileProtected(path, data, mode, "")
}

func writeNewFileProtected(path string, data []byte, mode os.FileMode, sourcePath string) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("création exclusive refusée (la sortie doit être absente) : %w", err)
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return statErr
	}
	completed := false
	defer func() {
		_ = file.Close()
		if !completed {
			if current, e := os.Lstat(path); e == nil && os.SameFile(info, current) {
				_ = os.Remove(path)
			}
		}
	}()
	if err = protectNewFile(file, sourcePath); err != nil {
		return fmt.Errorf("protection de la nouvelle copie : %w", err)
	}
	if err = writeAll(file, data); err != nil {
		return err
	}
	if err = file.Chmod(mode.Perm()); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	completed = true
	return nil
}

func createBackup(source sourceSnapshot) (string, error) {
	for i := 0; i < 10000; i++ {
		path := source.path + ".bak"
		if i > 0 {
			path += fmt.Sprintf(".%d", i)
		}
		err := writeNewFileProtected(path, source.data, source.info.Mode().Perm(), source.path)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		return path, nil
	}
	return "", errors.New("trop de sauvegardes existantes (10000)")
}

func replaceSource(source sourceSnapshot, data []byte, backupEnabled bool) (backup, warning string, err error) {
	if source.info.Mode().Perm()&0222 == 0 {
		return "", "", errors.New("fichier en lecture seule")
	}
	if err := sourceUnchanged(source); err != nil {
		return "", "", err
	}
	temporary, err := os.CreateTemp(filepath.Dir(source.path), ".rmwm-*.tmp")
	if err != nil {
		return "", "", err
	}
	name := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(name)
		}
	}()
	if err := protectNewFile(temporary, source.path); err != nil {
		return "", "", fmt.Errorf("protection du fichier temporaire : %w", err)
	}
	if err := writeAll(temporary, data); err != nil {
		return "", "", err
	}
	if err := temporary.Chmod(source.info.Mode().Perm()); err != nil {
		return "", "", err
	}
	if err := temporary.Sync(); err != nil {
		return "", "", err
	}
	if err := temporary.Close(); err != nil {
		return "", "", err
	}
	if err := sourceUnchanged(source); err != nil {
		return "", "", err
	}
	if backupEnabled {
		backup, err = createBackup(source)
		if err != nil {
			return "", "", fmt.Errorf("sauvegarde impossible : %w", err)
		}
	}
	if err := sourceUnchanged(source); err != nil {
		return backup, "", err
	}
	warning, err = commitReplacement(name, source.path)
	if err != nil {
		// Retain the completed output on commit failure; never discard the only
		// surviving copy after an unusual filesystem/Windows replacement error.
		removeTemporary = false
		return backup, warning, fmt.Errorf("remplacement impossible ; résultat conservé dans %s ; sauvegarde=%q : %w", name, backup, err)
	}
	return backup, warning, nil
}
