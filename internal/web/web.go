// Package web serves the HTMX frontend. Every action funnels through
// the in-process gRPC server (grpcserver.Server) so the HTTP side and
// remote callers go through the same code path and return the same
// errors.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/stuttgart-things/machinery-catalog-locator/catalogservice"
	"github.com/stuttgart-things/machinery-catalog-locator/internal/grpcserver"
)

//go:embed templates/*.html
var templateFS embed.FS

type Server struct {
	GRPC      *grpcserver.Server
	Build     BuildInfo
	templates *template.Template
}

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// New parses the embedded templates and returns the HTTP handler bundle.
func New(g *grpcserver.Server, bi BuildInfo) (*Server, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &Server{GRPC: g, Build: bi, templates: tmpl}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /tree", s.handleTree)
	mux.HandleFunc("POST /remove-target", s.handleRemoveTarget)
	mux.HandleFunc("POST /delete-resource", s.handleDeleteResource)
	mux.HandleFunc("GET /entity-manifest", s.handleEntityManifest)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	return mux
}

type indexData struct {
	Build   BuildInfo
	RootURL string
}

type treeData struct {
	RootURL   string
	Roots     []*nodeView
	Resources []*catalogservice.Resource
}

// nodeView wraps a proto Node so the recursive template has access to
// the root URL (needed to build hx-get URLs for /entity-manifest and
// /delete-resource). Without this, `$.RootURL` inside the recursive
// template would rebind to the Node itself and fail at render time.
type nodeView struct {
	*catalogservice.Node
	Children []*nodeView // shadows the embedded Node's Children
	RootURL  string
}

func wrapNode(n *catalogservice.Node, root string) *nodeView {
	nv := &nodeView{Node: n, RootURL: root}
	for _, c := range n.GetChildren() {
		nv.Children = append(nv.Children, wrapNode(c, root))
	}
	return nv
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	bi := s.Build
	if len(bi.Commit) > 7 {
		bi.Commit = bi.Commit[:7]
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "index.html", indexData{
		Build:   bi,
		RootURL: r.URL.Query().Get("root"),
	}); err != nil {
		slog.Error("render index", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	root := strings.TrimSpace(r.URL.Query().Get("root"))
	if root == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<div class="empty-state">Enter a root URL above to resolve a catalog.</div>`)
		return
	}

	tree, err := s.GRPC.ResolveTree(r.Context(), &catalogservice.ResolveTreeRequest{RootUrl: root})
	if err != nil {
		renderError(w, fmt.Errorf("resolve: %w", err))
		return
	}
	resList, err := s.GRPC.ListResources(r.Context(), &catalogservice.ListResourcesRequest{RootUrl: root})
	if err != nil {
		renderError(w, fmt.Errorf("list resources: %w", err))
		return
	}
	sort.Slice(resList.Resources, func(i, j int) bool {
		a, b := resList.Resources[i], resList.Resources[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Name < b.Name
	})

	wrappedRoots := make([]*nodeView, 0, len(tree.Roots))
	for _, n := range tree.Roots {
		wrappedRoots = append(wrappedRoots, wrapNode(n, root))
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "tree.html", treeData{
		RootURL:   root,
		Roots:     wrappedRoots,
		Resources: resList.Resources,
	}); err != nil {
		slog.Error("render tree", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

type actionResult struct {
	OK           bool
	Title        string
	PullRequest  string
	ErrorMessage string
}

func (s *Server) handleRemoveTarget(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, err)
		return
	}
	resp, err := s.GRPC.RemoveTarget(r.Context(), &catalogservice.RemoveTargetRequest{
		LocationUrl: r.FormValue("location"),
		Target:      r.FormValue("target"),
	})
	if err != nil {
		renderActionResult(w, s.templates, actionResult{
			Title:        "Remove target failed",
			ErrorMessage: err.Error(),
		})
		return
	}
	renderActionResult(w, s.templates, actionResult{
		OK:          true,
		Title:       "Target removal PR opened",
		PullRequest: resp.GetPullRequestUrl(),
	})
}

func (s *Server) handleDeleteResource(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, err)
		return
	}
	resp, err := s.GRPC.DeleteResource(r.Context(), &catalogservice.DeleteResourceRequest{
		RootUrl:   r.FormValue("root"),
		Kind:      r.FormValue("kind"),
		Name:      r.FormValue("name"),
		Namespace: r.FormValue("namespace"),
	})
	if err != nil {
		renderActionResult(w, s.templates, actionResult{
			Title:        "Delete resource failed",
			ErrorMessage: err.Error(),
		})
		return
	}
	renderActionResult(w, s.templates, actionResult{
		OK:          true,
		Title:       fmt.Sprintf("Resource %s/%s deletion PR opened", r.FormValue("kind"), r.FormValue("name")),
		PullRequest: resp.GetPullRequestUrl(),
	})
}

// handleEntityManifest fetches the Crossplane manifests linked from a
// catalog entity and renders them as expandable <details> blocks. The
// HTMX button in the tree swaps the response into a sibling pane.
func (s *Server) handleEntityManifest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	resp, err := s.GRPC.GetEntityManifest(r.Context(), &catalogservice.GetEntityManifestRequest{
		RootUrl:   q.Get("root"),
		Kind:      q.Get("kind"),
		Name:      q.Get("name"),
		Namespace: q.Get("namespace"),
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		fmt.Fprintf(w, `<div class="alert alert-error">%s</div>`,
			template.HTMLEscapeString(err.Error()))
		return
	}
	var b strings.Builder
	for _, m := range resp.GetManifests() {
		fmt.Fprintf(&b,
			`<details open class="manifest"><summary><span class="badge badge-kind">%s</span> <span class="badge badge-source">%s</span></summary><pre>%s</pre></details>`,
			template.HTMLEscapeString(m.GetLinkKind()),
			template.HTMLEscapeString(m.GetSource().GetPath()),
			template.HTMLEscapeString(string(m.GetBody())),
		)
	}
	fmt.Fprint(w, b.String())
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, "ok")
}

func renderError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<div class="alert alert-error">%s</div>`,
		template.HTMLEscapeString(err.Error()))
}

func renderActionResult(w http.ResponseWriter, t *template.Template, r actionResult) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "action_result.html", r); err != nil {
		slog.Error("render action result", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
