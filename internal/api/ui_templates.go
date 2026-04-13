package api

import (
	"bytes"
	"html/template"
	"io/fs"
	"log"
	"net/http"
)

type PageData struct {
	WorkspaceID       int64
	Active            string
	Page              string
	Policy            PolicyData
	BlockedDomainsStr string
}

type PolicyData struct {
	RateLimitWorkspacePerHour int
	RateLimitDomainPerHour    int
	BlockedRecipientDomains   []string
}

var tmpl *template.Template

func LoadTemplates(staticFS fs.FS) error {
	fsys, err := fs.Sub(staticFS, "templates")
	if err != nil {
		return err
	}

	tmpl = template.New("")
	_, err = tmpl.ParseFS(fsys, "*.html")
	return err
}

func RenderPage(w http.ResponseWriter, data PageData, page string) error {
	data.Page = page
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, page, data)
	if err != nil {
		log.Printf("template error: %v", err)
		return err
	}

	_, err = w.Write(buf.Bytes())
	return err
}
