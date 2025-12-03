package templates

import (
	"embed"
	"html/template"
	"io"
	"time"
)

//go:embed *.html admin/*.html
var fs embed.FS

type TemplateManager struct {
	templates *template.Template
}

func New() (*TemplateManager, error) {
	funcMap := template.FuncMap{
		"formatDate": func(t time.Time) string {
			return t.Format("2006-01-02 15:04")
		},
		"safe": func(s string) template.HTML {
			return template.HTML(s)
		},
		"year": func() int {
			return time.Now().Year()
		},
	}

	tmpl, err := template.New("").Funcs(funcMap).ParseFS(fs, "*.html", "admin/*.html")
	if err != nil {
		return nil, err
	}

	return &TemplateManager{
		templates: tmpl,
	}, nil
}

func (tm *TemplateManager) Render(w io.Writer, name string, data interface{}) error {
	return tm.templates.ExecuteTemplate(w, name, data)
}
