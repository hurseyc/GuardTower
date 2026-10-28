package api

import (
	"bufio"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// DocPage is one page of the in-app wiki.
type DocPage struct {
	Slug    string   `json:"slug"`
	Title   string   `json:"title"`
	Section string   `json:"section"`
	Order   int      `json:"order"`
	Summary string   `json:"summary"`
	Routes  []string `json:"routes,omitempty"`
	Body    string   `json:"body,omitempty"`
}

// docSectionOrder fixes the sidebar order of sections.
var docSectionOrder = map[string]int{"Getting started": 0, "Setup": 1, "Operation": 2, "Family DNS": 3, "Admin": 4, "Reference": 5}

// LoadDocs parses every markdown file in fsys. Front matter is a simple
// "key: value" block between --- lines.
func LoadDocs(fsys fs.FS) ([]DocPage, error) {
	var pages []DocPage
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") || strings.HasPrefix(path.Base(p), "_") || strings.HasPrefix(p, "dev/") {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		pg := parseDoc(string(data))
		pg.Slug = strings.TrimSuffix(p, ".md")
		if pg.Title == "" {
			pg.Title = pg.Slug
		}
		pages = append(pages, pg)
		return nil
	})
	sort.SliceStable(pages, func(i, j int) bool {
		a, b := pages[i], pages[j]
		sa, oka := docSectionOrder[a.Section]
		sb, okb := docSectionOrder[b.Section]
		if !oka {
			sa = 99
		}
		if !okb {
			sb = 99
		}
		if sa != sb {
			return sa < sb
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Title < b.Title
	})
	return pages, err
}

func parseDoc(src string) DocPage {
	var pg DocPage
	body := src
	if strings.HasPrefix(src, "---\n") {
		if end := strings.Index(src[4:], "\n---"); end >= 0 {
			front := src[4 : 4+end]
			body = strings.TrimLeft(src[4+end+4:], "\n")
			sc := bufio.NewScanner(strings.NewReader(front))
			for sc.Scan() {
				k, v, ok := strings.Cut(sc.Text(), ":")
				if !ok {
					continue
				}
				v = strings.TrimSpace(v)
				switch strings.TrimSpace(k) {
				case "title":
					pg.Title = v
				case "section":
					pg.Section = v
				case "order":
					pg.Order, _ = strconv.Atoi(v)
				case "summary":
					pg.Summary = v
				case "routes":
					for _, r := range strings.Split(strings.Trim(v, "[]"), ",") {
						if r = strings.TrimSpace(r); r != "" {
							pg.Routes = append(pg.Routes, r)
						}
					}
				}
			}
		}
	}
	pg.Body = body
	return pg
}

// handleDocsIndex lists pages. With ?full=1 bodies are included so the client
// can build its search index in one request.
func (s *Server) handleDocsIndex(w http.ResponseWriter, r *http.Request) {
	if s.DocsFS == nil {
		writeJSON(w, 200, []DocPage{})
		return
	}
	pages, err := LoadDocs(s.DocsFS)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if r.URL.Query().Get("full") != "1" {
		for i := range pages {
			pages[i].Body = ""
		}
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, 200, pages)
}

func (s *Server) handleDocPage(w http.ResponseWriter, r *http.Request) {
	slug := path.Clean("/" + chi.URLParam(r, "*"))[1:]
	if s.DocsFS == nil || slug == "" || strings.Contains(slug, "..") {
		writeErr(w, 404, "no such page")
		return
	}
	data, err := fs.ReadFile(s.DocsFS, slug+".md")
	if err != nil {
		writeErr(w, 404, "no such page")
		return
	}
	pg := parseDoc(string(data))
	pg.Slug = slug
	writeJSON(w, 200, pg)
}
