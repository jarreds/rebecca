// Package rebecca is a readme generator.
package rebecca

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/doc"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func NewCodeMap(pkg string, dir string) (*CodeMap, error) {
	m := &CodeMap{
		pkg:      pkg,
		dir:      dir,
		Examples: map[string]*doc.Example{},
		Comments: map[string]string{},
	}
	if err := m.scanDir(); err != nil {
		return nil, err
	}
	return m, nil
}

type CodeMap struct {
	pkg      string
	dir      string
	Name     string
	fset     *token.FileSet
	Examples map[string]*doc.Example
	Comments map[string]string
}

func (m *CodeMap) ExampleFunc(plain bool) func(in string) string {
	return func(in string) string {
		e, ok := m.Examples[in]
		if !ok {
			panic(fmt.Sprintf("Example %s not found.", in))
		}
		buf := &bytes.Buffer{}

		cn := &printer.CommentedNode{e.Code, e.Comments}

		if plain {
			printer.Fprint(buf, m.fset, cn)
			out := buf.String()
			if strings.HasSuffix(out, "\n\n}") {
				// fix annoying line-feed before end brace
				out = out[:len(out)-2] + "}"
			}
			o := "\n\t// Output:"
			if strings.Contains(out, o) {
				// Nasty kludge to remove the output...
				// TODO: Fix this
				out = out[:strings.Index(out, o)] + "\n}"
			}
			return out
		}

		if _, ok := e.Code.(*ast.BlockStmt); ok {
			// We have to remove the block manually
			// or comments don't print
			buf1 := &bytes.Buffer{}
			printer.Fprint(buf1, m.fset, cn)
			s := buf1.String()
			s = s[1 : len(s)-1]
			s = strings.TrimSpace(strings.Replace(s, "\n\t", "\n", -1))
			buf.WriteString(s)
		} else {
			printer.Fprint(buf, m.fset, cn)
		}

		return fmt.Sprintf("```go\n%s\n```", strings.Trim(buf.String(), "\n"))

	}
}

func (m *CodeMap) OutputFunc(in string) string {
	e, ok := m.Examples[in]
	if !ok {
		panic(fmt.Sprintf("Example %s not found.", in))
	}
	return strings.Trim(e.Output, "\n")
}

var docRegex = regexp.MustCompile(`(\w+)\[([0-9:, ]+)\]`)

func (m *CodeMap) DocFunc(in string) string {

	if matches := docRegex.FindStringSubmatch(in); matches != nil {
		id := matches[1]
		c, ok := m.Comments[id]
		if !ok {
			panic(fmt.Sprintf("Doc for %s not found in %s.", id, in))
		}
		return extractSections(in, matches[2], c)
	}

	c, ok := m.Comments[in]
	if !ok {
		panic(fmt.Sprintf("Doc for %s not found.", in))
	}
	return strings.Trim(c, "\n")
}

func (m *CodeMap) PlaygroundFunc(in string) string {
	e, ok := m.Examples[in]
	if !ok {
		panic(fmt.Sprintf("Example %s not found.", in))
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, m.fset, e.Play); err != nil {
		panic(fmt.Sprintf("Failed to format code for %s: %v", in, err))
	}

	out := buf.String()
	if strings.HasSuffix(out, "\n\n}\n") {
		// fix annoying line-feed before end brace
		out = out[:len(out)-3] + "}"
	}

	return out
}

var bothRegex = regexp.MustCompile(`^(\d+):(\d+)$`)
var fromRegex = regexp.MustCompile(`^(\d+):$`)
var toRegex = regexp.MustCompile(`^:(\d+)$`)
var singleRegex = regexp.MustCompile(`^(\d+)$`)

func mustInt(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		// shoulnd't get here because the string has passed a regex
		panic(err)
	}
	return i
}

func checkBounds(start, end, length int, spec string) {

	if end == 0 {
		panic(fmt.Sprintf("End must be greater than 0 in %s", spec))
	}

	if start >= length {
		panic(fmt.Sprintf("Index %d out of range (length %d) in %s", start, length, spec))
	}

	if end >= length {
		panic(fmt.Sprintf("Index %d out of range (length %d) in %s", end, length, spec))
	}

	if end > -1 && start >= end {
		panic(fmt.Sprintf("Start must be less than end in %s", spec))
	}
}

func extractSections(full string, sections string, comment string) string {

	var sentances []string
	for _, s := range strings.Split(comment, ".") {
		// ignore empty sentances
		trimmed := strings.Trim(s, " \n")
		if trimmed != "" {
			sentances = append(sentances, s)
		}
	}

	var out string
	for _, section := range strings.Split(sections, ",") {
		var arr []string
		if matches := bothRegex.FindStringSubmatch(section); matches != nil {
			// "i:j"
			checkBounds(mustInt(matches[1]), mustInt(matches[2]), len(sentances), full)
			arr = sentances[mustInt(matches[1]):mustInt(matches[2])]
		} else if matches := fromRegex.FindStringSubmatch(section); matches != nil {
			// "i:"
			checkBounds(mustInt(matches[1]), -1, len(sentances), full)
			arr = sentances[mustInt(matches[1]):]
		} else if matches := toRegex.FindStringSubmatch(section); matches != nil {
			// ":i"
			checkBounds(-1, mustInt(matches[1]), len(sentances), full)
			arr = sentances[:mustInt(matches[1])]
		} else if matches := singleRegex.FindStringSubmatch(section); matches != nil {
			// "i"
			checkBounds(mustInt(matches[1]), -1, len(sentances), full)
			arr = []string{sentances[mustInt(matches[1])]}
		} else {
			panic(fmt.Sprintf("Invalid section %s in %s", section, full))
		}

		for _, s := range arr {
			s1 := strings.Trim(s, " \n")
			if s1 != "" {
				out += s + "."
			}
		}
	}
	return strings.Trim(out, " ")
}

func (m *CodeMap) scanTests(name string, p *ast.Package) error {
	for name, f := range p.Files {
		if !strings.HasSuffix(name, "_test.go") {
			continue
		}
		examples := doc.Examples(f)
		for _, ex := range examples {
			m.Examples["Example"+ex.Name] = ex
		}
	}
	return nil
}

func (m *CodeMap) scanPkg(name string, p *ast.Package) error {
	for fpath, f := range p.Files {
		if f.Doc.Text() != "" {
			_, name := filepath.Split(fpath)
			m.Comments[strings.Replace(name, ".", "_", -1)] = f.Doc.Text()
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Doc.Text() == "" {
					continue
				}
				if d.Recv == nil {
					// function
					//fmt.Println(d.Name, d.Doc.Text())
					name := fmt.Sprint(d.Name)
					m.Comments[name] = d.Doc.Text()
				} else {
					// method
					e := d.Recv.List[0].Type
					if se, ok := e.(*ast.StarExpr); ok {
						// if the method receiver has a *, discard it.
						e = se.X
					}
					b := &bytes.Buffer{}
					printer.Fprint(b, m.fset, e)
					//fmt.Printf("%s.%s %s", b.String(), d.Name, d.Doc.Text())
					name := fmt.Sprintf("%s.%s", b.String(), d.Name)
					m.Comments[name] = d.Doc.Text()
				}
			case *ast.GenDecl:
				switch s := d.Specs[0].(type) {
				case *ast.TypeSpec:
					//fmt.Println(s.Name, d.Doc.Text())
					name := fmt.Sprint(s.Name)
					m.Comments[name] = d.Doc.Text()
					if t, ok := s.Type.(*ast.StructType); ok {
						for _, f := range t.Fields.List {
							if f.Doc.Text() == "" {
								continue
							}
							if f.Names[0].IsExported() {
								fieldName := fmt.Sprint(name, ".", f.Names[0])
								m.Comments[fieldName] = f.Doc.Text()
							}
						}
					}
				case *ast.ValueSpec:
					if d.Doc.Text() == "" {
						continue
					}
					//fmt.Println(s.Names[0], d.Doc.Text())
					if len(s.Names) == 0 {
						continue
					}
					name := fmt.Sprint(s.Names[0])
					m.Comments[name] = d.Doc.Text()
				}
			}
		}
	}
	return nil
}

func (m *CodeMap) scanDir() error {
	// Create the AST by parsing src.
	m.fset = token.NewFileSet() // positions are relative to fset
	pkgs, err := parser.ParseDir(m.fset, m.dir, nil, parser.ParseComments)
	if err != nil {
		return err
	}
	for name, p := range pkgs {
		m.Name = strings.TrimSuffix(name, "_test")
		if err := m.scanTests(name, p); err != nil {
			return err
		}
		if err := m.scanPkg(name, p); err != nil {
			return err
		}
	}

	return nil
}
