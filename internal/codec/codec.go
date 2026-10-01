// Package codec предоставляет набор кодеков данных (JSON, YAML, TOML, CSV,
// ENV, XML, INI, NDJSON, TEXT) с единым интерфейсом.
//
// Все кодеки работают с единой моделью value.Value: декодеры читают из
// io.Reader и возвращают value.Value, энкодеры принимают value.Value и
// пишут в io.Writer. Это позволяет конвертировать между любыми форматами
// без промежуточных представлений.
package codec

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/asakue/conv/internal/format"
	"github.com/asakue/conv/internal/value"
)

// Codec — единый интерфейс декодера и энкодера.
type Codec interface {
	// Decode читает данные из r и возвращает единую модель.
	Decode(r io.Reader) (value.Value, error)
	// Encode записывает значение в формате кодека в w.
	Encode(w io.Writer, v value.Value) error
}

// Options — настройки, общие для всех кодеков.
//
// Нулевое значение пригодно: отступ по умолчанию 2 пробела, заголовки CSV
// включены, автоопределение типов выключено.
type Options struct {
	// Indent — ширина отступа для форматированного вывода (JSON/YAML/XML).
	Indent int
	// Header — писать/читать строку заголовков (CSV).
	Header bool
	// Infer — восстанавливать типы из строк (CSV/ENV/INI).
	Infer bool
}

// DefaultOptions возвращает настройки по умолчанию.
func DefaultOptions() Options {
	return Options{Indent: 2, Header: true}
}

// IndentString возвращает отступ в виде строки из пробелов.
// Значение <= 0 означает компактный вывод без отступа.
func (o Options) IndentString() string {
	if o.Indent <= 0 {
		return ""
	}
	return strings.Repeat(" ", o.Indent)
}

// New создаёт кодек по формату с указанными настройками.
// Возвращает ошибку для неизвестного формата.
func New(f format.Format, opts Options) (Codec, error) {
	switch f {
	case format.JSON:
		return &JSONCodec{Indent: opts.IndentString()}, nil
	case format.YAML:
		return &YAMLCodec{Indent: opts.Indent}, nil
	case format.TOML:
		return &TOMLCodec{}, nil
	case format.CSV:
		return &CSVCodec{Comma: ',', Header: opts.Header, Infer: opts.Infer}, nil
	case format.ENV:
		return &ENVCodec{}, nil
	case format.XML:
		return &XMLCodec{Indent: opts.Indent}, nil
	case format.INI:
		return &INICodec{}, nil
	case format.NDJSON:
		return &NDJSONCodec{}, nil
	case format.TEXT:
		return &TextCodec{}, nil
	default:
		return nil, fmt.Errorf("неизвестный формат %q; доступны: %s",
			f, strings.Join(format.Names(), ", "))
	}
}

// MustNew — то же, что New, но паникует при неизвестном формате.
// Удобно для инициализации кодеков по умолчанию.
func MustNew(f format.Format, opts Options) Codec {
	c, err := New(f, opts)
	if err != nil {
		panic(err)
	}
	return c
}

// Sniff пытается определить формат по содержимому.
// Возвращает пустую строку, если формат не распознан.
//
// Эвристики намеренно консервативны: формат определяется только когда
// признаки однозначны. Неопределённые случаи留给 вызывающему коду
// (обычно это JSON по умолчанию).
func Sniff(data []byte) format.Format {
	data = trimBOM(data)
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return ""
	}

	switch trimmed[0] {
	case '{', '[':
		return format.JSON
	case '<':
		if bytes.HasPrefix(trimmed, []byte("<?xml")) || bytes.HasPrefix(trimmed, []byte("<!DOCTYPE")) {
			return format.XML
		}
		// XML без декларации: <root>...</root>.
		if len(trimmed) > 1 && isXMLNameStart(trimmed[1]) {
			return format.XML
		}
	case '#', ';':
		// Комментарий — различаем YAML/ENV/INI по остальным строкам.
		return sniffCommentLed(trimmed)
	}

	return sniffKeyValues(trimmed)
}

// sniffCommentLed различает YAML, INI и ENV для документов, начинающихся
// с комментария.
func sniffCommentLed(data []byte) format.Format {
	for _, line := range splitLines(data) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			return format.INI
		}
		if hasYAMLPair(line) {
			return format.YAML
		}
		if strings.Contains(line, "=") {
			return format.ENV
		}
		break
	}
	return format.YAML
}

// sniffKeyValues различает ENV, INI, TOML и YAML по первым строкам.
func sniffKeyValues(data []byte) format.Format {
	lines := splitLines(data)
	iniSections, envPairs, yamlPairs, tomlTables := 0, 0, 0, 0
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			iniSections++
			// [table] с точкой или кавычками — скорее TOML.
			if strings.ContainsAny(line, `."`) {
				tomlTables++
			}
		case strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]"):
			tomlTables++
		case strings.HasPrefix(line, "---"):
			return format.YAML
		case hasYAMLPair(line):
			yamlPairs++
		case strings.Contains(line, "="):
			envPairs++
		default:
			// Первая строка не похожа ни на что знакомое — не угадываем.
			return ""
		}
	}
	if tomlTables > 0 {
		return format.TOML
	}
	if iniSections > 0 && iniSections >= envPairs {
		return format.INI
	}
	if yamlPairs > 0 && yamlPairs >= envPairs {
		return format.YAML
	}
	if envPairs > 0 {
		return format.ENV
	}
	return ""
}

// hasYAMLPair сообщает, похожа ли строка на пару YAML ("ключ: значение").
func hasYAMLPair(line string) bool {
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "-") {
		return false
	}
	i := strings.Index(line, ":")
	if i <= 0 {
		return false
	}
	// Двоеточие должно заканчивать ключ: далее пробел или конец строки.
	if i+1 < len(line) && line[i+1] != ' ' {
		return false
	}
	// URL-подобные значения ("http://x") не считаются парой.
	return !strings.Contains(line[:i], "//")
}

// isXMLNameStart сообщает, допустим ли байт как начало имени XML-элемента.
func isXMLNameStart(b byte) bool {
	return b == '_' || b == ':' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// splitLines разбивает данные на строки, нормализуя CRLF и CR.
func splitLines(data []byte) []string {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

// normalizeNewlines переводит CRLF/CR в LF.
func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// splitFlat превращает flatten-карту value.Value в map[string]any.
func splitFlat(flat map[string]value.Value) map[string]any {
	out := make(map[string]any, len(flat))
	for k, v := range flat {
		out[k] = v.ToAny()
	}
	return out
}
