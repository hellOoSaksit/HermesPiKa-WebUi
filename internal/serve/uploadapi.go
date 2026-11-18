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
	"hpk.local/hpk/internal/places"
	"hpk.local/hpk/internal/project"
	"hpk.local/hpk/internal/safepath"
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

// uploadName checks a name sent by the browser: one plain file name by the
// same rules as every project path (safepath.Name).
func uploadName(name string) error {
	if err := safepath.Name(name); err != nil {
		return errors.New("that file name is not allowed: " + err.Error())
	}
	return nil
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
// project root. Never overwrites. Solo mode only, like the folder picker. When a
// file fails part-way through, the error answer still lists the files saved
// before it ({error, files}).
// GET /api/files/list?project=&path=: the visible entries of a project folder,
// each file marked with the rule folder it belongs in.
//
// Both act on the project folder as checked again now (places.Root) and reach
// folders inside it only through safepath, which never follows a link or junction.
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
		root, err := places.Root(s.home, p)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
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
		fail := func(code int, err error) {
			writeJSON2(w, code, map[string]any{"error": err.Error(), "files": out})
		}
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				fail(http.StatusBadRequest, errors.New("the upload was cut off"))
				return
			}
			if part.FormName() != "file" {
				continue
			}
			// the raw name: Part.FileName() would quietly strip "../"
			_, params, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
			name := params["filename"]
			if err := uploadName(name); err != nil {
				fail(http.StatusBadRequest, fmt.Errorf("%q: %w", name, err))
				return
			}
			folder := ""
			if state == folders.OK {
				folder, _ = rules.For(name)
			}
			dir, err := safepath.Dir(root, folder, true)
			if err != nil {
				fail(http.StatusBadRequest, fmt.Errorf("the folder %s: %w", folder, err))
				return
			}
			got, err := saveOne(w, part, dir, name, max)
			if err != nil {
				var tooBig *http.MaxBytesError
				if errors.As(err, &tooBig) {
					fail(http.StatusRequestEntityTooLarge, fmt.Errorf("%s is larger than the limit of %d bytes", name, max))
				} else {
					fail(http.StatusBadRequest, errors.New("the upload was cut off"))
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
		root, err := places.Root(s.home, p)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		rel := strings.Trim(strings.ReplaceAll(r.URL.Query().Get("path"), "\\", "/"), "/")
		dir, err := safepath.Dir(root, rel, false)
		if err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("no such folder in the project: %w", err))
			return
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
			if safepath.Name(e.Name()) != nil {
				continue
			}
			fi, err := e.Info()
			if err != nil || safepath.IsLink(filepath.Join(dir, e.Name()), fi) {
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
