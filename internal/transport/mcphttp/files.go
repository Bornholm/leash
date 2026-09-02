package mcphttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
)

// Les endpoints /files/ donnent à un client (typiquement un agent) le moyen
// de déposer et récupérer des fichiers dans le workspace d'un tenant sans
// les faire transiter par le canal MCP, dont les résultats sont textuels et
// bornés. Ils partagent avec le transport MCP l'authentification Bearer, la
// résolution HMAC du discriminant et le cycle de vie des workspaces : passer
// par Manager.Acquire rafraîchit le TTL exactement comme un appel d'outil.
//
// Confinement des chemins : on utilise os.Root (Go 1.24+) plutôt qu'un
// filepath.Clean suivi d'un test de préfixe. Un test de préfixe ne voit que
// la chaîne : il laisse passer un chemin dont un composant intermédiaire est
// un lien symbolique vers l'extérieur du workspace — et le shell du tenant
// peut créer un tel lien dans son propre répertoire. os.Root résout chaque
// composant côté noyau et refuse de suivre un lien qui sortirait de la
// racine, ce qui ferme le cas symlink par construction et non par
// vérification a posteriori.

const filesPrefix = "/files/"

type fileEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type fileListing struct {
	Files []fileEntry `json:"files"`
	Total int64       `json:"total_bytes"`
}

// registerFileRoutes branche les routes fichiers sur le mux.
func (s *Server) registerFileRoutes(mux *http.ServeMux) {
	var h http.Handler = http.HandlerFunc(s.handleFiles)
	h = authMiddleware(s.cfg.APIKeys, h)
	mux.Handle("GET "+filesPrefix+"{path...}", h)
	mux.Handle("PUT "+filesPrefix+"{path...}", h)
	mux.Handle("DELETE "+filesPrefix+"{path...}", h)
}

// handleFiles résout le workspace du tenant puis dispatche sur la méthode.
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	key, ok := apiKeyFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := resolveDiscriminant(r, s.cfg, key)
	if err != nil {
		// Contrairement au MCP, pas de workspace éphémère ici : déposer un
		// fichier dans un workspace qu'on ne saura pas retrouver n'a pas de
		// sens et masquerait une erreur de configuration du client.
		http.Error(w, "missing or invalid workspace discriminant", http.StatusBadRequest)
		return
	}

	ws, err := s.mgr.Acquire(r.Context(), id, key)
	if err != nil {
		s.logger.Error("mcphttp: acquiring workspace for file request", "error", err,
			"workspace_id", id, "request_id", requestIDFromContext(r.Context()))
		http.Error(w, "unable to acquire workspace", http.StatusServiceUnavailable)
		return
	}

	rel := r.PathValue("path")

	root, err := os.OpenRoot(ws.dir)
	if err != nil {
		s.logger.Error("mcphttp: opening workspace root", "error", err, "workspace_id", id)
		http.Error(w, "unable to open workspace", http.StatusInternalServerError)
		return
	}
	defer func() { _ = root.Close() }()

	switch r.Method {
	case http.MethodGet:
		if rel == "" {
			s.listFiles(w, root, id)
			return
		}
		s.getFile(w, r, root, rel, id)
	case http.MethodPut:
		if rel == "" {
			http.Error(w, "a file path is required", http.StatusBadRequest)
			return
		}
		s.putFile(w, r, root, rel, id)
	case http.MethodDelete:
		if rel == "" {
			http.Error(w, "a file path is required", http.StatusBadRequest)
			return
		}
		s.deleteFile(w, root, rel, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// cleanRelPath normalise un chemin reçu de l'URL en chemin relatif simple.
// os.Root refuse déjà tout échappement ; ce nettoyage écarte en amont les
// formes qui n'ont de toute façon aucun sens ("", "/", "..", chemin absolu).
func cleanRelPath(rel string) (string, error) {
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return "", errors.New("empty path")
	}
	cleaned := path.Clean(rel)
	if cleaned == "." || cleaned == "/" || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", errors.New("path escapes the workspace")
	}
	return cleaned, nil
}

func (s *Server) getFile(w http.ResponseWriter, r *http.Request, root *os.Root, rel, wsID string) {
	cleaned, err := cleanRelPath(rel)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	f, err := root.Open(cleaned)
	if err != nil {
		s.writeFileError(w, err, wsID, "open")
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.Error(w, "not a regular file", http.StatusNotFound)
		return
	}

	ctype := mime.TypeByExtension(path.Ext(cleaned))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("Content-Disposition", "attachment; filename="+quoteFilename(path.Base(cleaned)))
	if r.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(w, f); err != nil {
		s.logger.Warn("mcphttp: streaming file to client failed", "error", err, "workspace_id", wsID)
	}
}

// quoteFilename produit un nom de fichier entre guillemets pour l'en-tête
// Content-Disposition, en neutralisant guillemets et antislashs.
func quoteFilename(name string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(name) + `"`
}

func (s *Server) putFile(w http.ResponseWriter, r *http.Request, root *os.Root, rel, wsID string) {
	cleaned, err := cleanRelPath(rel)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	// Quota : on retranche la taille du fichier qu'on est en train de
	// remplacer, sinon réécrire un fichier compterait deux fois.
	used, err := workspaceUsage(root)
	if err != nil {
		s.logger.Error("mcphttp: computing workspace usage", "error", err, "workspace_id", wsID)
		http.Error(w, "unable to compute workspace usage", http.StatusInternalServerError)
		return
	}
	if info, statErr := root.Stat(cleaned); statErr == nil && !info.IsDir() {
		used -= info.Size()
	}

	budget := s.cfg.MaxFileBytes
	if s.cfg.WorkspaceQuotaBytes > 0 {
		remaining := s.cfg.WorkspaceQuotaBytes - used
		if remaining <= 0 {
			http.Error(w, "workspace quota exceeded", http.StatusInsufficientStorage)
			return
		}
		if remaining < budget {
			budget = remaining
		}
	}

	if dir := path.Dir(cleaned); dir != "." {
		if err := root.MkdirAll(dir, 0o700); err != nil {
			s.writeFileError(w, err, wsID, "mkdir")
			return
		}
	}

	f, err := root.Create(cleaned)
	if err != nil {
		s.writeFileError(w, err, wsID, "create")
		return
	}

	// budget+1 : lire un octet de plus que le budget suffit à détecter le
	// dépassement sans avoir à faire confiance à Content-Length.
	written, copyErr := io.Copy(f, io.LimitReader(r.Body, budget+1))
	closeErr := f.Close()

	if copyErr != nil || closeErr != nil {
		_ = root.Remove(cleaned)
		s.logger.Warn("mcphttp: writing uploaded file failed", "workspace_id", wsID,
			"error", errors.Join(copyErr, closeErr))
		http.Error(w, "unable to write file", http.StatusInternalServerError)
		return
	}

	if written > budget {
		_ = root.Remove(cleaned)
		if s.cfg.WorkspaceQuotaBytes > 0 && budget < s.cfg.MaxFileBytes {
			http.Error(w, "workspace quota exceeded", http.StatusInsufficientStorage)
			return
		}
		http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	}

	s.logger.Debug("mcphttp: file stored", "workspace_id", wsID, "bytes", written)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(fileEntry{Path: cleaned, Size: written})
}

func (s *Server) deleteFile(w http.ResponseWriter, root *os.Root, rel, wsID string) {
	cleaned, err := cleanRelPath(rel)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	if err := root.RemoveAll(cleaned); err != nil {
		s.writeFileError(w, err, wsID, "remove")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listFiles(w http.ResponseWriter, root *os.Root, wsID string) {
	listing := fileListing{Files: []fileEntry{}}
	err := fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		listing.Files = append(listing.Files, fileEntry{Path: p, Size: info.Size()})
		listing.Total += info.Size()
		return nil
	})
	if err != nil {
		s.logger.Error("mcphttp: listing workspace files", "error", err, "workspace_id", wsID)
		http.Error(w, "unable to list files", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(listing)
}

// workspaceUsage additionne la taille des fichiers réguliers du workspace.
func workspaceUsage(root *os.Root) (int64, error) {
	var total int64
	err := fs.WalkDir(root.FS(), ".", func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// writeFileError traduit une erreur système en réponse HTTP sans divulguer
// de chemin hôte au client.
func (s *Server) writeFileError(w http.ResponseWriter, err error, wsID, op string) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, fs.ErrPermission), errors.Is(err, os.ErrInvalid):
		http.Error(w, "path escapes the workspace", http.StatusBadRequest)
	default:
		s.logger.Warn("mcphttp: file operation failed", "op", op, "workspace_id", wsID, "error", err)
		// os.Root signale un échappement par une erreur non typée
		// ("path escapes from parent") : la traiter comme une requête
		// invalide plutôt que comme une panne serveur.
		if strings.Contains(err.Error(), "escapes from parent") {
			http.Error(w, "path escapes the workspace", http.StatusBadRequest)
			return
		}
		http.Error(w, "file operation failed", http.StatusInternalServerError)
	}
}
