package projector

import (
	"bytes"
	_ "embed"
	"io"
	"os"
	"strings"
	"text/template"
)

//go:embed code_appendix.tmpl
var codeAppendixTmpl string

type CodeSection struct {
	Prompt string
	Label  string
	Code   string
}

type CodeAppendix struct {
	sections []CodeSection
	out      io.Writer
}

type codeAppendixOpts func(*CodeAppendix)

func NewCodeAppendix(opts ...codeAppendixOpts) *CodeAppendix {
	p := &CodeAppendix{
		out: os.Stdout,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func CodeAppendixWithSections(sections []CodeSection) codeAppendixOpts {
	return func(p *CodeAppendix) {
		p.sections = sections
	}
}

func CodeAppendixWithOutput(out io.Writer) codeAppendixOpts {
	return func(p *CodeAppendix) {
		p.out = out
	}
}

func (p *CodeAppendix) SetOutput(out io.Writer) {
	p.out = out
}

func (p *CodeAppendix) Generate() error {
	tmpl, err := template.New("code_appendix").Parse(codeAppendixTmpl)
	if err != nil {
		return err
	}

	for i := range p.sections {
		p.sections[i].Label = sanitizeLabel(p.sections[i].Prompt)
	}

	templateData := struct {
		Sections []CodeSection
	}{
		Sections: p.sections,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData); err != nil {
		return err
	}

	_, err = p.out.Write(buf.Bytes())
	return err
}

func WriteCodeAppendix(sections []CodeSection, outDir, filename string) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	f, err := os.Create(outDir + "/" + filename)
	if err != nil {
		return err
	}
	defer f.Close()

	ca := NewCodeAppendix(
		CodeAppendixWithSections(sections),
		CodeAppendixWithOutput(f),
	)
	return ca.Generate()
}

func sanitizeLabel(prompt string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, strings.ToLower(prompt))
}
