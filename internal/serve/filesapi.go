// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// GET /api/files?project=<name>&path=<path in the project>
//
// Lets the chat show what the agent made (a render, an .fbx with its textures,
// a video, a chart's CSV) straight from the project folder. Read only, and only
// inside the project's root after following links; hidden files and folders
// (.git, .env, …) and key files are never served. Responses are sandboxed so an
// SVG or HTML file cannot run script on the Console's origin.
var secretFileRE = regexp.MustCompile(`(?i)(^id_[a-z0-9_]+(\.pub)?$|\.(pem|key|p12|pfx|kdbx|keystore|jks|env|tfstate|tfvars|ppk|asc|gpg)$|\.tfstate\.|^credentials|^service-?account.*\.json$|secrets?\.(json|ya?ml|toml)$|^\.?(npmrc|pypirc|netrc|pgpass|htpasswd)$)`)

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

// projectFile resolves rel inside root, refusing anything outside it or hidden.
func projectFile(root, rel string) (string, os.FileInfo, error) {
	if root == "" {
		return "", nil, errors.New("this project has no folder")
	}
	rel = filepath.FromSlash(strings.TrimPrefix(strings.ReplaceAll(rel, "\\", "/"), "/"))
	if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "\x00") {
		return "", nil, errors.New("give a path inside the project")
	}
	// Windows tricks: 8.3 short names (GIT~1), alternate streams (a::$DATA),
	// names Win32 trims (x.pem. / "x.pem "). Refused outright.
	if strings.ContainsAny(rel, "~:") {
		return "", nil, errors.New("give the file's plain path")
	}
	if err := allowedParts(rel); err != nil {
		return "", nil, err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", nil, err
	}
	full, err := filepath.EvalSymlinks(filepath.Join(realRoot, rel))
	if err != nil {
		return "", nil, errors.New("no such file in the project")
	}
	if full != realRoot && !strings.HasPrefix(full, realRoot+string(filepath.Separator)) {
		return "", nil, errors.New("that file is outside the project")
	}
	// a link inside the project may point at .env or .git/config: check where it lands
	inside, err := filepath.Rel(realRoot, full)
	if err != nil {
		return "", nil, errors.New("that file is outside the project")
	}
	if err := allowedParts(inside); err != nil {
		return "", nil, err
	}
	fi, err := os.Stat(full)
	if err != nil || fi.IsDir() {
		return "", nil, errors.New("no such file in the project")
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
		full, fi, err := projectFile(p.Root, r.URL.Query().Get("path"))
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

// allowedParts refuses hidden components, components Windows would trim, and
// key/credential file names, anywhere in a project-relative path.
func allowedParts(rel string) error {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, part := range parts {
		if part == ".." || strings.HasPrefix(part, ".") {
			return errors.New("hidden files and folders are not shown")
		}
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return errors.New("give the file's plain path")
		}
	}
	if secretFileRE.MatchString(parts[len(parts)-1]) {
		return errors.New("key and credential files are not shown")
	}
	return nil
}
