// Package format описывает список поддерживаемых форматов, их расширения
// и средства определения формата по содержимому (sniffing).
package format

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Format — идентификатор формата данных.
type Format string

// Unknown — формат не определён.
const Unknown Format = ""

// Поддерживаемые форматы.
const (
	JSON Format = "json"
	YAML Format = "yaml"
	TOML Format = "toml"
	CSV  Format = "csv"
	ENV  Format = "env"
	XML  Format = "xml"
	INI  Format = "ini"
	NDJSON Format = "ndjson"
	TEXT Format = "text"
)

// info — метаданные формата.
type info struct {
	name        string
	aliases     []string
	extensions  []string
	tablealike  bool // формат плоский/табличный: требует разворачивания структур
	description string
}

var registry = map[Format]info{
	JSON: {
		name:        "JSON",
		aliases:     []string{"json", "json5", "application/json"},
		extensions:  []string{".json", ".jsonc", ".geojson", ".map"},
		description: "JavaScript Object Notation (RFC 8259)",
	},
	YAML: {
		name:        "YAML",
		aliases:     []string{"yaml", "yml", "application/yaml", "application/x-yaml"},
		extensions:  []string{".yaml", ".yml"},
		description: "YAML 1.2 (подмножество, реализуемое gopkg.in/yaml.v3)",
	},
	TOML: {
		name:        "TOML",
		aliases:     []string{"toml", "application/toml"},
		extensions:  []string{".toml"},
		description: "Tom's Obvious, Minimal Language v1.0",
	},
	CSV: {
		name:        "CSV",
		aliases:     []string{"csv", "tsv", "text/csv"},
		extensions:  []string{".csv", ".tsv"},
		tablealike:  true,
		description: "Запятые-разделённые значения (RFC 4180), поддержка TSV",
	},
	ENV: {
		name:        "ENV",
		aliases:     []string{"env", "dotenv", ".env", "environment", "application/x-env"},
		extensions:  []string{".env", ".environment"},
		tablealike:  true,
		description: "Файл переменных окружения KEY=VALUE",
	},
	XML: {
		name:        "XML",
		aliases:     []string{"xml", "application/xml", "text/xml"},
		extensions:  []string{".xml", ".svg", ".rss", ".xlf"},
		description: "Extensible Markup Language (W3C XML 1.0)",
	},
	INI: {
		name:        "INI",
		aliases:     []string{"ini", "cfg", "conf", "config", "properties", "text/plain"},
		extensions:  []string{".ini", ".cfg", ".conf", ".properties"},
		tablealike:  true,
		description: "INI/properties: секции [name] и пары KEY=VALUE",
	},
	NDJSON: {
		name:        "NDJSON",
		aliases:     []string{"ndjson", "jsonl", "json-lines", "ldjson", "application/x-ndjson"},
		extensions:  []string{".ndjson", ".jsonl"},
		description: "Newline-delimited JSON: по одному объекту в строке",
	},
	TEXT: {
		name:        "TEXT",
		aliases:     []string{"text", "txt", "plain", "raw", "text/plain"},
		description: "Произвольный текст (одна строка/скаляр)",
	},
}

// All возвращает все известные форматы в алфавитном порядке.
func All() []Format {
	out := make([]Format, 0, len(registry))
	for f := range registry {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Parse разбирает пользовательское имя формата (без учёта регистра).
// Пустая строка или "-" возвращают Format("") — «не задан».
func Parse(name string) (Format, error) {
	norm := strings.ToLower(strings.TrimSpace(name))
	if norm == "" || norm == "-" || norm == "auto" {
		return "", nil
	}
	norm = strings.TrimPrefix(norm, ".")
	// Прямое совпадение с идентификатором.
	if _, ok := registry[Format(norm)]; ok {
		return Format(norm), nil
	}
	// Совпадение по алиасу.
	for f, inf := range registry {
		for _, a := range inf.aliases {
			if strings.ToLower(a) == norm {
				return f, nil
			}
		}
	}
	return "", fmt.Errorf("неизвестный формат %q; доступные: %s", name, strings.Join(Names(), ", "))
}

// Names возвращает список имён форматов в каноническом виде.
func Names() []string {
	all := All()
	out := make([]string, len(all))
	for i, f := range all {
		out[i] = string(f)
	}
	return out
}

// Info возвращает метаданные формата.
func Info(f Format) (info, bool) {
	i, ok := registry[f]
	return i, ok
}

// Display возвращает «красивое» имя формата (JSON, YAML, ...).
func Display(f Format) string {
	if i, ok := registry[f]; ok {
		return i.name
	}
	return string(f)
}

// Description возвращает короткое описание формата.
func Description(f Format) string {
	if i, ok := registry[f]; ok {
		return i.description
	}
	return ""
}

// IsTableLike сообщает, что формат плоский (CSV/ENV/INI) и требует
// предварительного разворачивания вложенных структур.
func IsTableLike(f Format) bool {
	i, ok := registry[f]
	return ok && i.tablealike
}

// FromExtension определяет формат по имени файла.
// Возвращает Format(""), если расширение неизвестно.
func FromExtension(path string) Format {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		// Специальные имена: .env, .env.local и т.п.
		base := strings.ToLower(filepath.Base(path))
		if base == ".env" || strings.HasPrefix(base, ".env.") {
			return ENV
		}
		return ""
	}
	for f, inf := range registry {
		for _, e := range inf.extensions {
			if e == ext {
				return f
			}
		}
	}
	return ""
}

// Extensions возвращает список расширений формата.
func Extensions(f Format) []string {
	i, ok := registry[f]
	if !ok {
		return nil
	}
	return i.extensions
}
