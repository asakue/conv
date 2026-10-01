// Package main — точка входа CLI-утилиты conv.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/asakue/conv/internal/app"
	"github.com/asakue/conv/internal/format"
)

const usageText = `conv — конвертер между форматами данных

  conv [опции] <формат-выхода> [<формат-входа>] [файл]

Примеры:
  conv yaml data.json           # JSON → YAML
  conv csv data.yaml            # YAML → CSV
  conv xml -                    # stdin (XML)
  cat data.json | conv yaml     # stdin → YAML
  conv json data.csv --infer    # CSV с автоопределением типов
  conv yaml -m base.yaml patch.yaml  # merge двух YAML

Форматы: json, yaml, toml, csv, env, xml, ini, ndjson, text

Опции:
  --infer       Автоопределение типов в CSV/ENV/INI (int, float, bool, null)
  --header      Писать/читать строку заголовков в CSV (по умолчанию: да)
  --no-header   Не писать/читать строку заголовков в CSV
  --indent N    Отступ для JSON/YAML (по умолчанию: 2)
  --merge, -m   Объединить несколько входных файлов (deep merge)
  --flatten     Развернуть вложенную структуру в плоскую карту
  --unflatten   Собрать дерево из плоской карты
  --path P      Извлечь/записать значение по пути (a.b.c)
  --delete P    Удалить значение по пути
  --help, -h    Показать справку
  --version     Показать версию
`

func main() {
	f := parseFlags(os.Args[1:])
	if f.help {
		fmt.Print(usageText)
		return
	}
	if f.version {
		fmt.Println("conv 0.1.0")
		return
	}
	if f.outFmt == "" {
		fmt.Fprintln(os.Stderr, "ошибка: формат выхода не указан")
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(1)
	}

	a := &app.App{
		OutFmt:   f.outFmt,
		InFmt:    f.inFmt,
		Infer:    f.infer,
		Header:   f.header,
		NoHeader: f.noHeader,
		Indent:   f.indent,
		Merge:    f.merge,
		Flatten:  f.flatten,
		Unflatten: f.unflatten,
		Path:     f.path,
		Delete:   f.delete,
	}

	if err := a.Run(f.files); err != nil {
		fmt.Fprintf(os.Stderr, "conv: %v\n", err)
		os.Exit(1)
	}
}

type flags struct {
	help      bool
	version   bool
	outFmt    string
	inFmt     string
	files     []string
	infer     bool
	header    bool
	noHeader  bool
	indent    int
	merge     bool
	flatten   bool
	unflatten bool
	path      string
	delete    string
}

func parseFlags(args []string) flags {
	var f flags
	fs := flag.NewFlagSet("conv", flag.ContinueOnError)
	fs.BoolVar(&f.help, "help", false, "справка")
	fs.BoolVar(&f.help, "h", false, "справка")
	fs.BoolVar(&f.version, "version", false, "версия")
	fs.BoolVar(&f.infer, "infer", false, "автоопределение типов в CSV/ENV/INI")
	fs.BoolVar(&f.header, "header", true, "строка заголовков в CSV")
	fs.BoolVar(&f.noHeader, "no-header", false, "без заголовков в CSV")
	fs.IntVar(&f.indent, "indent", 2, "отступ для JSON/YAML")
	fs.BoolVar(&f.merge, "merge", false, "объединить несколько файлов")
	fs.BoolVar(&f.merge, "m", false, "объединить несколько файлов (короткое)")
	fs.BoolVar(&f.flatten, "flatten", false, "развернуть в плоскую карту")
	fs.BoolVar(&f.unflatten, "unflatten", false, "собрать дерево из плоской карты")
	fs.StringVar(&f.path, "path", "", "извлечь значение по пути (a.b.c)")
	fs.StringVar(&f.delete, "delete", "", "удалить значение по пути")

	_ = fs.Parse(args)

	// Позиционные аргументы: сначала форматы, потом файлы.
	positional := fs.Args()
	if len(positional) == 0 {
		return f
	}

	// Первый аргумент — формат выхода.
	outFmt, err := format.Parse(positional[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "ошибка: %v\n", err)
		os.Exit(1)
	}
	f.outFmt = string(outFmt)

	// Второй аргумент (опционально) — формат входа.
	if len(positional) > 1 {
		inFmt, err := format.Parse(positional[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "ошибка: %v\n", err)
			os.Exit(1)
		}
		f.inFmt = string(inFmt)
	}

	// Остальное — файлы.
	if len(positional) > 2 {
		f.files = positional[2:]
	}

	// Автоопределение заголовков: если --no-header, header=false.
	if f.noHeader {
		f.header = false
	}

	return f
}