package textio

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

type Encoding uint8

const (
	UTF8 Encoding = iota
	UTF8BOM
	UTF16LE
	UTF16BE
)

// ************************************************************************************************
// decodeText validates both the encoding and the textual input policy.
func DecodeText(data []byte) (string, Encoding, error) {
	text, encoding, err := mk_decodeTextBytes(data)
	if err != nil {
		return "", encoding, err
	}
	if err = mk_validateText(text); err != nil {
		return "", encoding, err
	}
	return text, encoding, nil
}

// ************************************************************************************************
// decodeTextBytes decodes supported encodings without reapplying input policy
// to already cleaned output (which may expose a container-like text prefix).
func DecodeTextBytes(data []byte) (string, Encoding, error) {
	encoding := UTF8
	if bytes.HasPrefix(data, []byte{0xFF, 0xFE, 0, 0}) || bytes.HasPrefix(data, []byte{0, 0, 0xFE, 0xFF}) {
		return "", encoding, errors.New("UTF-32 non pris en charge ; convertir en UTF-8")
	}
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		encoding, data = UTF8BOM, data[3:]
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		encoding, data = UTF16LE, data[2:]
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		encoding, data = UTF16BE, data[2:]
	}
	var text string
	if encoding == UTF16LE || encoding == UTF16BE {
		if len(data)%2 != 0 {
			return "", encoding, errors.New("UTF-16 tronqué : nombre d'octets impair")
		}
		var order binary.ByteOrder = binary.LittleEndian
		if encoding == UTF16BE {
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
	return text, encoding, nil
}

func ValidateText(text string) error {
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

func EncodeText(text string, encoding Encoding) []byte {
	switch encoding {
	case UTF8BOM:
		return append([]byte{0xEF, 0xBB, 0xBF}, []byte(text)...)
	case UTF16LE, UTF16BE:
		words := utf16.Encode([]rune(text))
		out := make([]byte, 2+2*len(words))
		var order binary.ByteOrder = binary.LittleEndian
		out[0], out[1] = 0xFF, 0xFE
		if encoding == UTF16BE {
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

func ReadLimited(reader io.Reader, maximum int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("taille supérieure à la limite de %d octets (-max-size)", maximum)
	}
	return data, nil
}

func WriteAll(writer io.Writer, data []byte) error {
	n, err := writer.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

type SourceSnapshot struct {
	Path string
	Info os.FileInfo
	Data []byte
}

func ReadSource(path string, maximum int64) (SourceSnapshot, error) {
	var result SourceSnapshot
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
	data, err := ReadLimited(file, maximum)
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
	return SourceSnapshot{Path: path, Info: after, Data: data}, nil
}

func sourceUnchanged(source SourceSnapshot) error {
	current, err := ReadSource(source.Path, int64(len(source.Data))+1)
	if err != nil {
		return fmt.Errorf("vérification avant écriture : %w", err)
	}
	if !os.SameFile(source.Info, current.Info) || !bytes.Equal(source.Data, current.Data) ||
		!source.Info.ModTime().Equal(current.Info.ModTime()) || source.Info.Mode() != current.Info.Mode() {
		return errors.New("le fichier a changé depuis sa lecture ; aucune substitution effectuée")
	}
	return nil
}

// New files and backups are created with O_EXCL. Existing files and symlinks
// cannot be overwritten through either path.
func WriteNewFile(path string, data []byte, mode os.FileMode) (err error) {
	return WriteNewFileProtected(path, data, mode, "")
}

func WriteNewFileProtected(path string, data []byte, mode os.FileMode, sourcePath string) (err error) {
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
	if err = WriteAll(file, data); err != nil {
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

func createBackup(source SourceSnapshot) (string, error) {
	for i := 0; i < 10000; i++ {
		path := source.Path + ".bak"
		if i > 0 {
			path += fmt.Sprintf(".%d", i)
		}
		err := WriteNewFileProtected(path, source.Data, source.Info.Mode().Perm(), source.Path)
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

func ReplaceSource(source SourceSnapshot, data []byte, backupEnabled bool) (backup, warning string, err error) {
	if source.Info.Mode().Perm()&0222 == 0 {
		return "", "", errors.New("fichier en lecture seule")
	}
	if err := sourceUnchanged(source); err != nil {
		return "", "", err
	}
	temporary, err := os.CreateTemp(filepath.Dir(source.Path), ".rmwm-*.tmp")
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
	if err := protectNewFile(temporary, source.Path); err != nil {
		return "", "", fmt.Errorf("protection du fichier temporaire : %w", err)
	}
	if err := WriteAll(temporary, data); err != nil {
		return "", "", err
	}
	if err := temporary.Chmod(source.Info.Mode().Perm()); err != nil {
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
	warning, err = commitReplacement(name, source.Path)
	if err != nil {
		// Retain the completed output on commit failure; never discard the only
		// surviving copy after an unusual filesystem/Windows replacement error.
		removeTemporary = false
		return backup, warning, fmt.Errorf("remplacement impossible ; résultat conservé dans %s ; sauvegarde=%q : %w", name, backup, err)
	}
	return backup, warning, nil
}
