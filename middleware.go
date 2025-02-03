package substrate

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func (s *SubstrateHandler) fileExists(path string) bool {
	isDir := false
	if strings.HasSuffix(path, "/") {
		path = path[:len(path)-1]
		isDir = true
	}

	info, err := fs.Stat(s.fs, path)
	if err != nil {
		return false
	}

	if isDir {
		return info.IsDir()
	}
	return !info.IsDir()
}

func (s *SubstrateHandler) findBestResource(r *http.Request) *string {
	v := caddyhttp.GetVar(r.Context(), "root")
	root, ok := v.(string)
	if !ok {
		root = "."
	}

	reqPath := r.URL.Path

	if s.fileExists(caddyhttp.SanitizedPathJoin(root, reqPath)) {
		return &reqPath
	}

	for _, suffix := range s.Cmd.Order.TryFiles {
		bigPath := reqPath + suffix
		if s.fileExists(caddyhttp.SanitizedPathJoin(root, bigPath)) {
			return &bigPath
		}
	}

	if len(s.Cmd.Order.CatchAll) > 0 {
		dir := reqPath
		for {
			for _, ca := range s.Cmd.Order.CatchAll {
				candidate := path.Join(dir, ca)
				if s.fileExists(caddyhttp.SanitizedPathJoin(root, candidate)) {
					return &candidate
				}
			}
			if dir == "/" || dir == "." {
				break
			}
			newDir := path.Dir(dir)
			if newDir == dir {
				break
			}
			dir = newDir
		}
	}

	return nil
}

func (s *SubstrateHandler) enableReverseProxy(r *http.Request) bool {
	for _, ext := range s.Cmd.Order.Match {
		if strings.HasSuffix(r.URL.Path, ext) {
			return true
		}
	}
	return false
}

func (s SubstrateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if s.Cmd == nil || s.Cmd.Order == nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		s.log.Error("No order")
		return nil
	}

	match := s.findBestResource(r)
	if *match != r.URL.Path {
		r.Header.Set("X-Forwarded-Path", r.URL.Path)
		r.URL.Path = *match
	}

	if s.enableReverseProxy(r) {
		s.proxy.Upstreams[0].Dial = s.Cmd.Order.Host
		return s.proxy.ServeHTTP(w, r, next)
	}

	return next.ServeHTTP(w, r)
}

