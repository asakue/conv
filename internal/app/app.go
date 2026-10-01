// Package app — ядро утилиты conv.
//
// Задача: прочитать данные (из файлов или stdin), декодировать в единую
// модель value.Value, выполнить опции (--flatten, --path, --delete, --merge),
// затем закодировать в целевой формат и записать в stdout.
package app

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/asakue/conv/internal/codec"
	"github.com/asakue/conv/internal/format"
	"github.com/asakue/conv/internal/value"
)

// App — ядро конвертера.
type App struct {
	OutFmt    string
	InFmt     string
	Infer     bool
	Header    bool
	NoHeader  bool
	Indent    int
	Merge     bool
	Flatten   bool
	Unflatten bool
	Path      string
	Delete    string
}

// Run — основная функция.
func (a *App) Run(files []string) error {
	outFmt, err := format.Parse(a.OutFmt)
	if err != nil {
		return fmt.Errorf("формат выхода: %w", err)
	}
	opts := codec.Options{
		Indent: a.Indent,
		Header: a.Header,
		Infer:  a.Infer,
	}
	enc, err := codec.New(outFmt, opts)
	if err != nil {
		return err
	}

	// 1. Читаем входы ОДИН РАЗ: stdin (если нужен) и каждый файл.
	inputs, err := readInputs(files)
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		return fmt.Errorf("нет входных данных")
	}

	// 2. Определяем формат входа: явный флаг → расширение → sniffing → json.
	inFmt, err := a.resolveInputFormat(files, inputs)
	if err != nil {
		return err
	}

	// 3. Декодируем каждый вход.
	decoded := make([]value.Value, 0, len(inputs))
	for _, inp := range inputs {
		v, err := decodeBytes(inp, inFmt, opts)
		if err != nil {
			return fmt.Errorf("декод (%s): %w", inFmt, err)
		}
		decoded = append(decoded, v)
	}

	// Merge: объединяем несколько входных значений.
	var root value.Value
	if len(decoded) == 1 {
		root = decoded[0]
	} else if len(decoded) > 1 {
		if a.Merge {
			// Deep merge: последовательное объединение.
			root = value.NewNil()
			for _, d := range decoded {
				root = value.Merge(root, d, false)
			}
		} else {
			// Без merge — собираем в массив.
			root = value.NewArray(decoded)
		}
	}

	// Flatten / Unflatten.
	if a.Flatten && a.Unflatten {
		return fmt.Errorf("--flatten и --unflatten несовместимы")
	}
	if a.Flatten {
		opt := value.FlattenOptions{
			Sep:         ".",
			ArrayStyle:  "brackets",
			NullAsEmpty: a.Infer,
		}
		if root.Type == value.TypeArray {
			// Разворачиваем каждый элемент массива.
			var out []value.Value
			for _, item := range root.ArrayVal() {
				flat := value.Flatten(item, opt)
				out = append(out, value.NewMap(flat))
			}
			root = value.NewArray(out)
		} else if root.Type == value.TypeMap {
			flat := value.Flatten(root, opt)
			root = value.NewMap(flat)
		}
	}
	if a.Unflatten {
		if root.Type == value.TypeMap {
			root = value.Unflatten(root.MapVal(), ".")
		} else if root.Type == value.TypeArray {
			var out []value.Value
			for _, item := range root.ArrayVal() {
				if item.Type == value.TypeMap {
					out = append(out, value.Unflatten(item.MapVal(), "."))
				} else {
					out = append(out, item)
				}
			}
			root = value.NewArray(out)
		}
	}

	// Path / Delete.
	if a.Path != "" && a.Delete != "" {
		return fmt.Errorf("--path и --delete несовместимы")
	}
	if a.Path != "" {
		v, err := value.Get(root, a.Path)
		if err != nil {
			return fmt.Errorf("path: %w", err)
		}
		root = v
	}
	if a.Delete != "" {
		var err error
		root, err = value.Delete(root, a.Delete)
		if err != nil {
			return fmt.Errorf("delete: %w", err)
		}
	}

	// Encode.
	if err := enc.Encode(os.Stdout, root); err != nil {
		return fmt.Errorf("запись: %w", err)
	}
	return nil
}

// readInputs читает все входы ровно один раз: stdin (если нужен) и каждый файл.
//
// Порядок сохраняется: "-" означает stdin; если файлы не указаны, читается stdin.
func readInputs(files []string) ([][]byte, error) {
	var inputs [][]byte

	// 1. stdin: если файлов нет вообще или среди файлов есть "-".
	if len(files) == 0 || containsStdin(files) {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("stdin: %w", err)
		}
		inputs = append(inputs, data)
	}

	// 2. Файлы.
	for _, f := range files {
		if isStdin(f) {
			continue // уже прочитан выше
		}
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("открыть %s: %w", f, err)
		}
		inputs = append(inputs, data)
	}

	return inputs, nil
}

// resolveInputFormat определяет формат входа: явный флаг → расширение файла →
// sniffing по уже прочитанным данным → json по умолчанию.
//
// Важно: данные не читаются повторно — sniffing работает по буферу из inputs.
func (a *App) resolveInputFormat(files []string, inputs [][]byte) (string, error) {
	if a.InFmt != "" {
		f, err := format.Parse(a.InFmt)
		if err != nil {
			return "", err
		}
		return string(f), nil
	}

	// По расширению первого реального файла.
	for _, f := range files {
		if isStdin(f) {
			continue
		}
		if ext := format.FromExtension(f); ext != format.Unknown {
			return string(ext), nil
		}
	}

	// Sniffing по содержимому.
	for _, data := range inputs {
		if sniff := codec.Sniff(data); sniff != format.Unknown {
			return string(sniff), nil
		}
	}

	// По умолчанию JSON.
	return string(format.JSON), nil
}

// containsStdin проверяет, есть ли в списке файлов stdin.
func containsStdin(files []string) bool {
	for _, f := range files {
		if isStdin(f) {
			return true
		}
	}
	return false
}

// decodeBytes декодирует байты в заданном формате.
func decodeBytes(data []byte, inFmt string, opts codec.Options) (value.Value, error) {
	f, err := format.Parse(inFmt)
	if err != nil {
		return value.Value{}, err
	}
	dec, err := codec.New(f, opts)
	if err != nil {
		return value.Value{}, err
	}
	return dec.Decode(bytes.NewReader(data))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func isStdin(path string) bool {
	return path == "-" || path == ""
}