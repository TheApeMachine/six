package projector

import (
	"bytes"
	_ "embed"
	"io"
	"os"
	"text/template"
)

//go:embed section.tmpl
var sectionTmpl string

type Section struct {
	title    string
	content  string
	elements []Interface
	out      io.Writer
}

type sectionOpts func(*Section)

func NewSection(opts ...sectionOpts) *Section {
	s := &Section{
		out: os.Stdout,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (section *Section) Generate() error {
	tmpl, err := template.New("section").Parse(sectionTmpl)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	templateData := struct {
		Title   string
		Content string
	}{
		Title:   section.title,
		Content: section.content,
	}

	if err := tmpl.Execute(&buf, templateData); err != nil {
		return err
	}

	if _, err := section.out.Write(buf.Bytes()); err != nil {
		return err
	}

	for _, el := range section.elements {
		// Output redirection if possible?
		// We'll trust elements are configured with same writer,
		// or we can enforce it by passing out.
		if setter, ok := el.(interface{ SetOutput(io.Writer) }); ok {
			setter.SetOutput(section.out)
		}
		if err := el.Generate(); err != nil {
			return err
		}
	}

	return nil
}

func SectionWithTitle(title string) sectionOpts {
	return func(section *Section) {
		section.title = title
	}
}

func SectionWithContent(content string) sectionOpts {
	return func(section *Section) {
		section.content = content
	}
}

func SectionWithElements(elements ...Interface) sectionOpts {
	return func(section *Section) {
		section.elements = append(section.elements, elements...)
	}
}

func SectionWithOutput(out io.Writer) sectionOpts {
	return func(section *Section) {
		section.out = out
	}
}
