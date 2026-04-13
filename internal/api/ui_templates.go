package api

import (
	"html/template"
	"io/fs"
	"net/http"
)

type PageData struct {
	WorkspaceID       int64
	Active            string
	Policy            PolicyData
	BlockedDomainsStr string
}

type PolicyData struct {
	RateLimitWorkspacePerHour int
	RateLimitDomainPerHour    int
	BlockedRecipientDomains   []string
}

var layoutTmpl *template.Template

func LoadTemplates(staticFS fs.FS) error {
	fsys, err := fs.Sub(staticFS, "web")
	if err != nil {
		return err
	}
	tmpl, err := template.ParseFS(fsys, "templates/*.html")
	if err != nil {
		return err
	}
	layoutTmpl = tmpl
	return nil
}

func RenderPage(w http.ResponseWriter, data PageData, page string) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	return layoutTmpl.ExecuteTemplate(w, page, data)
}
