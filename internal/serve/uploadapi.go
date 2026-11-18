// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"hpk.local/hpk/internal/folders"
	"hpk.local/hpk/internal/project"
)

const defaultUploadMax = int64(2 << 30) // 2 GB per file

// uploadMax is the per-file size limit: files.upload_max in project.yaml.
func uploadMax(p *project.Project) int64 {
	var v int64
	if err := project.Field(p, []string{"files", "upload_max"}, &v); err != nil || v <= 0 {
		return defaultUploadMax
	}
	return v
}

// uploadName checks a name sent by the browser: one plain file name, nothing hidden,
// nothing Windows would reinterpret, no key or credential files.
func uploadName(name string) error {
	if name == "" || strings.ContainsAny(name, "/\\:\x00<>\"|?*") || strings.ContainsFunc(name, func(r rune) bool { return r < 32 }) {
		return errors.New("that file name is not allowed")
	}
	if name == "." || name == ".." {
		return errors.New("that file name is not allowed")
	}
	return allowedParts(name)
}

// uploadDir resolves a project-relative folder for writing: it must stay inside
// the project after following links, and is created when missing.
func uploadDir(root, rel string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	if rel == "" {
		return realRoot, nil
	}
	if err := allowedParts(rel); err != nil {
		return "", err
	}
	dest := filepath.Join(realRoot, filepath.FromSlash(rel))
	// the nearest folder that already exists must not lead out of the project
	probe := dest
	for {
		if _, err := os.Lstat(probe); err == nil {
			break
		}
		next := filepath.Dir(probe)
		if next == probe {
			return "", errors.New("that folder is outside the project")
		}
		probe = next
	}
	real, err := filepath.EvalSymlinks(probe)
	if err != nil || (real != realRoot && !strings.HasPrefix(real, realRoot+string(filepath.Separator))) {
		return "", errors.New("that folder is outside the project")
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	real, err = filepath.EvalSymlinks(dest)
	if err != nil || (real != realRoot && !strings.HasPrefix(real, realRoot+string(filepath.Separator))) {
		return "", errors.New("that folder is outside the project")
	}
	return real, nil
}

// numbered returns "name (n).ext" for n >= 2.
func numbered(name string, n int) string {
	ext := filepath.Ext(name)
	return fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), n, ext)
}

// publish moves tmp to dir/name without ever replacing a file; a clash picks
// "name (2).ext", "name (3).ext", … It returns the name used.
func publish(tmp, dir, name string) (string, error) {
	for n := 1; n < 10000; n++ {
		cand := name
		if n > 1 {
			cand = numbered(name, n)
		}
		dst := filepath.Join(dir, cand)
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		// a hard link fails if dst appeared meanwhile; volumes that cannot link
		// (FAT/exFAT, some network drives) get an O_EXCL copy instead. Never a
		// rename: on Windows Go's rename replaces an existing file.
		err := os.Link(tmp, dst)
		if err == nil {
			os.Remove(tmp)
			return cand, nil
		}
		if os.IsExist(err) {
			continue
		}
		switch err := copyExcl(tmp, dst); {
		case err == nil:
			os.Remove(tmp)
			return cand, nil
		case os.IsExist(err):
			continue
		default:
			return "", err
		}
	}
	return "", errors.New("too many files with that name")
}

// copyExcl creates dst (failing if it exists) and copies src into it; a failed
// copy leaves no dst behind.
func copyExcl(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

type uploaded struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Folder string `json:"folder"`
}

// saveOne streams one multipart part into dir through a temp file.
func saveOne(w http.ResponseWriter, part io.Reader, dir, name string, max int64) (string, error) {
	tmp, err := os.CreateTemp(dir, ".hpk-upload-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	if _, err := io.Copy(tmp, http.MaxBytesReader(w, io.NopCloser(part), max)); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	got, err := publish(tmpName, dir, name)
	if err != nil {
		return "", err
	}
	ok = true
	return got, nil
}

// POST /api/files/upload?project=: multipart, field "file" (repeatable). Each file
// goes to the folder the approved folder rules name for its extension, else the
// project root. Never overwrites. Solo mode only, like the folder picker.
// GET /api/files/list?project=&path=: the visible entries of a project folder,
// each file marked with the rule folder it belongs in.
func (s *Server) uploadAPI(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/files/upload", func(w http.ResponseWriter, r *http.Request) {
		if !s.personalOn() {
			http.NotFound(w, r)
			return
		}
		p, err := s.loadFor(r, r.URL.Query().Get("project"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		if p.Root == "" {
			writeErr(w, http.StatusBadRequest, errors.New("this project has no folder"))
			return
		}
		mr, err := r.MultipartReader()
		if err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("send the files as multipart form data"))
			return
		}
		rules, state := folders.Load(s.home, p)
		max := uploadMax(p)
		out := []uploaded{}
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				writeErr(w, http.StatusBadRequest, errors.New("the upload was cut off"))
				return
			}
			if part.FormName() != "file" {
				continue
			}
			// the raw name: Part.FileName() would quietly strip "../"
			_, params, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
			name := params["filename"]
			if err := uploadName(name); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			folder := ""
			if state == folders.OK {
				folder, _ = rules.For(name)
			}
			dir, err := uploadDir(p.Root, folder)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			got, err := saveOne(w, part, dir, name, max)
			if err != nil {
				var tooBig *http.MaxBytesError
				if errors.As(err, &tooBig) {
					writeErr(w, http.StatusRequestEntityTooLarge, fmt.Errorf("%s is larger than the limit of %d bytes", name, max))
				} else {
					writeErr(w, http.StatusBadRequest, errors.New("the upload was cut off"))
				}
				return
			}
			rel := got
			if folder != "" {
				rel = folder + "/" + got
			}
			out = append(out, uploaded{Name: got, Path: rel, Folder: folder})
		}
		if len(out) == 0 {
			writeErr(w, http.StatusBadRequest, errors.New("no file in the request"))
			return
		}
		writeJSON(w, map[string]any{"files": out})
	})

	mux.HandleFunc("GET /api/files/list", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.loadFor(r, r.URL.Query().Get("project"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		if p.Root == "" {
			writeErr(w, http.StatusBadRequest, errors.New("this project has no folder"))
			return
		}
		rel := strings.Trim(strings.ReplaceAll(r.URL.Query().Get("path"), "\\", "/"), "/")
		realRoot, err := filepath.EvalSymlinks(p.Root)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		dir := realRoot
		if rel != "" {
			if strings.ContainsAny(rel, "~:\x00") {
				writeErr(w, http.StatusBadRequest, errors.New("give the folder's plain path"))
				return
			}
			if err := allowedParts(rel); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			dir, err = filepath.EvalSymlinks(filepath.Join(realRoot, filepath.FromSlash(rel)))
			if err != nil || (dir != realRoot && !strings.HasPrefix(dir, realRoot+string(filepath.Separator))) {
				writeErr(w, http.StatusBadRequest, errors.New("no such folder in the project"))
				return
			}
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("cannot open that folder"))
			return
		}
		rules, state := folders.Load(s.home, p)
		type entry struct {
			Name   string `json:"name"`
			Dir    bool   `json:"dir"`
			Size   int64  `json:"size"`
			Mtime  int64  `json:"mtime"`
			Folder string `json:"folder"`
		}
		list := []entry{}
		for _, e := range ents {
			if allowedParts(e.Name()) != nil || e.Type()&os.ModeSymlink != 0 {
				continue
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			en := entry{Name: e.Name(), Dir: fi.IsDir(), Mtime: fi.ModTime().Unix()}
			if !fi.IsDir() {
				en.Size = fi.Size()
				if state == folders.OK {
					en.Folder, _ = rules.For(e.Name())
				}
			}
			list = append(list, en)
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].Dir != list[j].Dir {
				return list[i].Dir
			}
			return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
		})
		writeJSON(w, map[string]any{"path": rel, "entries": list})
	})
}
