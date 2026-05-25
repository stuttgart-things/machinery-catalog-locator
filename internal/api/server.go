// Package api stellt die HTTP-Schnittstelle des catalog-locator bereit.
package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/stuttgart-things/machinery-catalog-locator/internal/catalog"
	ghforge "github.com/stuttgart-things/machinery-catalog-locator/internal/github"
)

// Server buendelt die Abhaengigkeiten der HTTP-API.
type Server struct {
	Resolver *catalog.Resolver
	Reader   catalog.FileReader
	PR       *ghforge.PRService
	Log      *slog.Logger
}

// Routes registriert alle Routen und liefert einen Handler.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /locations/tree", s.handleTree)
	mux.HandleFunc("GET /resources", s.handleResources)
	mux.HandleFunc("POST /locations/remove-target", s.handleRemoveTarget)
	mux.HandleFunc("POST /resources/delete", s.handleDeleteResource)
	return s.withLogging(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleTree loest die Root-Location auf und gibt den kompletten Baum zurueck.
// GET /locations/tree?root=<blob-url>
func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	root, err := catalog.ParseBlobURL(r.URL.Query().Get("root"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	nodes, err := s.Resolver.Resolve(r.Context(), root)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"roots": nodes})
}

// handleResources liefert eine flache Liste aller Nicht-Location-Entitaeten.
// GET /resources?root=<blob-url>
func (s *Server) handleResources(w http.ResponseWriter, r *http.Request) {
	root, err := catalog.ParseBlobURL(r.URL.Query().Get("root"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	nodes, err := s.Resolver.Resolve(r.Context(), root)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	res := catalog.Resources(nodes)
	out := make([]resourceDTO, 0, len(res))
	for _, n := range res {
		out = append(out, resourceDTO{
			Kind:      n.Entity.Kind,
			Name:      n.Entity.Metadata.Name,
			Namespace: n.Entity.Metadata.Namespace,
			Source:    n.Source,
			ViaTarget: n.ViaTarget,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"resources": out})
}

type resourceDTO struct {
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Source    catalog.SourceRef `json:"source"`
	ViaTarget string            `json:"viaTarget,omitempty"`
}

// removeTargetReq -> POST /locations/remove-target
type removeTargetReq struct {
	Location string `json:"location"` // Blob-URL der location.yaml
	Target   string `json:"target"`   // zu entfernender Target-String
}

// handleRemoveTarget entfernt einen Target-Eintrag aus einer location.yaml
// und oeffnet dafuer einen PR (Operation 1).
func (s *Server) handleRemoveTarget(w http.ResponseWriter, r *http.Request) {
	var req removeTargetReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("body ungueltig: %w", err))
		return
	}
	locRef, err := catalog.ParseBlobURL(req.Location)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	raw, err := s.Reader.Read(r.Context(), locRef)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	patched, err := catalog.RemoveTargetFromLocation(raw, req.Target)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}

	url, err := s.PR.OpenPullRequest(r.Context(), ghforge.PRRequest{
		Owner:         locRef.Owner,
		Repo:          locRef.Repo,
		BaseBranch:    locRef.Ref,
		HeadBranch:    branchName("remove-target", req.Target),
		Title:         fmt.Sprintf("chore(catalog): remove target from %s", locRef.Path),
		Body:          fmt.Sprintf("Entfernt das Target `%s` aus `%s`.", req.Target, locRef.Path),
		CommitMessage: fmt.Sprintf("chore(catalog): remove target %s", req.Target),
		Edits:         map[string][]byte{locRef.Path: patched},
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"pullRequest": url})
}

// deleteResourceReq -> POST /resources/delete
type deleteResourceReq struct {
	Root      string `json:"root"`      // Blob-URL der Root-Location
	Kind      string `json:"kind"`      // z. B. "Component"
	Name      string `json:"name"`      // metadata.name
	Namespace string `json:"namespace"` // optional, default "default"
}

// handleDeleteResource loescht eine echte Ressource aus Git und entfernt
// in einem PR zugleich das verweisende Target aus der Parent-Location
// (Operation 2). Beides in einem PR verhindert verwaiste Targets.
func (s *Server) handleDeleteResource(w http.ResponseWriter, r *http.Request) {
	var req deleteResourceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("body ungueltig: %w", err))
		return
	}
	root, err := catalog.ParseBlobURL(req.Root)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	nodes, err := s.Resolver.Resolve(r.Context(), root)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	node, ok := catalog.Find(nodes, req.Kind, req.Name, req.Namespace)
	if !ok {
		writeError(w, http.StatusNotFound,
			fmt.Errorf("ressource %s/%s nicht im Baum gefunden", req.Kind, req.Name))
		return
	}
	if node.Parent == nil {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Errorf("ressource ist eine Root-Location und kann so nicht geloescht werden"))
		return
	}

	prReq := ghforge.PRRequest{
		Owner:      node.Source.Owner,
		Repo:       node.Source.Repo,
		BaseBranch: node.Source.Ref,
		HeadBranch: branchName("delete", node.Entity.Kind+"-"+node.Entity.Metadata.Name),
		Title: fmt.Sprintf("chore(catalog): remove %s/%s",
			node.Entity.Kind, node.Entity.Metadata.Name),
		CommitMessage: fmt.Sprintf("chore(catalog): remove %s/%s",
			node.Entity.Kind, node.Entity.Metadata.Name),
		Edits:   map[string][]byte{},
		Deletes: []string{},
	}

	// Ressourcendatei: ganze Datei loeschen oder nur ein Dokument herausschneiden.
	if node.FileDocCount > 1 {
		raw, err := s.Reader.Read(r.Context(), node.Source)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		patched, err := catalog.RemoveDocumentFromFile(raw, node.DocIndex)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		prReq.Edits[node.Source.Path] = patched
	} else {
		prReq.Deletes = append(prReq.Deletes, node.Source.Path)
	}

	// Parent-Target mitentfernen - nur wenn die Location im selben Repo/Branch liegt.
	parent := node.Parent
	if parent.Source.SameRepo(node.Source) {
		raw, err := s.Reader.Read(r.Context(), parent.Source)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		patched, err := catalog.RemoveTargetFromLocation(raw, node.ViaTarget)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		prReq.Edits[parent.Source.Path] = patched
	}

	prReq.Body = fmt.Sprintf(
		"Entfernt `%s/%s` aus dem Catalog.\n\n- Datei: `%s`\n- Parent-Location: `%s`",
		node.Entity.Kind, node.Entity.Metadata.Name,
		node.Source.Path, parent.Source.Path)

	url, err := s.PR.OpenPullRequest(r.Context(), prReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"pullRequest":       url,
		"parentTouched":     parent.Source.SameRepo(node.Source),
		"documentExtracted": node.FileDocCount > 1,
	})
}

// --- Helfer ---

func branchName(prefix, hint string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, hint)
	clean = strings.Trim(strings.ToLower(clean), "-")
	if len(clean) > 40 {
		clean = clean[:40]
	}
	return fmt.Sprintf("machinery-catalog-locator/%s-%s-%d", prefix, clean, time.Now().Unix())
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.Log.Info("request",
			"method", r.Method, "path", r.URL.Path,
			"dur", time.Since(start).String())
	})
}
