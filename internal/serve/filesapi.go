// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"hpk.local/hpk/internal/places"
	"hpk.local/hpk/internal/safepath"
)

// GET /api/files?project=<name>&path=<path in the project>
//
// Lets the chat show what the agent made (a render, an .fbx with its textures,
// a video, a chart's CSV) straight from the project folder. Read only, and only
// inside the project's root, never through a link or junction; hidden files and
// folders (.git, .env, …) and key files are never served (safepath). Responses
// are sandboxed so an SVG or HTML file cannot run script on the Console's origin.

const maxAttachments = 20

const maxServe = 1 << 30 // 1 GB

func registerFileTypes() {
	for ext, typ := range map[string]string{
		".glb": "model/gltf-binary", ".gltf": "model/gltf+json", ".fbx": "application/octet-stream", ".obj": "text/plain; charset=utf-8",
		".mtl": "text/plain; charset=utf-8", ".stl": "model/stl", ".ply": "application/octet-stream", ".dae": "model/vnd.collada+xml",
		".3mf": "model/3mf", ".tga": "image/x-tga", ".hdr": "image/vnd.radiance", ".webm": "video/webm", ".mp4": "video/mp4",
		".mov": "video/quicktime", ".m4v": "video/mp4", ".ogg": "audio/ogg", ".mp3": "audio/mpeg", ".wav": "audio/wav", ".flac": "audio/flac",
		".csv": "text/csv; charset=utf-8", ".tsv": "text/tab-separated-values; charset=utf-8", ".avif": "image/avif", ".webp": "image/webp",
	} {
		_ = mime.AddExtensionType(ext, typ)
	}
}

// projectFile resolves rel inside root by safepath: plain names only (no
// hidden, key, device or 8.3 names, no alternate streams), and no link or
// junction anywhere on the way.
func projectFile(root, rel string) (string, os.FileInfo, error) {
	if root == "" {
		return "", nil, errors.New("this project has no folder")
	}
	rel = strings.TrimPrefix(strings.ReplaceAll(rel, "\\", "/"), "/")
	if rel == "" || filepath.IsAbs(rel) {
		return "", nil, errors.New("give a path inside the project")
	}
	realRoot, err := safepath.Root(root)
	if err != nil {
		return "", nil, err
	}
	full, fi, err := safepath.File(realRoot, rel)
	if err != nil {
		return "", nil, err
	}
	if fi.Size() > maxServe {
		return "", nil, errors.New("file is larger than 1 GB")
	}
	return full, fi, nil
}

func (s *Server) filesAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/files", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.loadFor(r, r.URL.Query().Get("project"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		root, err := places.Root(s.home, p)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		full, fi, err := projectFile(root, r.URL.Query().Get("path"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		f, err := os.Open(full)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		defer f.Close()
		h := w.Header()
		typ := mime.TypeByExtension(strings.ToLower(filepath.Ext(full)))
		if typ == "" {
			typ = "application/octet-stream"
		}
		h.Set("Content-Type", typ)
		h.Set("Cache-Control", "private, max-age=60")
		if strings.HasPrefix(typ, "application/pdf") {
			// the browser's PDF viewer inside the Console's frame
			h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; frame-ancestors 'self'")
		} else {
			h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'; frame-ancestors 'none'")
		}
		if strings.HasPrefix(typ, "text/html") || strings.HasPrefix(typ, "image/svg") || strings.HasPrefix(typ, "application/xhtml") {
			h.Set("Content-Disposition", "attachment")
		}
		http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
	})
}
