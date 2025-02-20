package substrate

import (
	"fmt"
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

	reqPath := caddyhttp.CleanPath(r.URL.Path, true)

	for _, m := range s.Cmd.Order.matchers {

		if !strings.HasPrefix(reqPath, m.path) {
			continue
		}

		bigPath := caddyhttp.CleanPath(reqPath+"/index"+m.ext, true)
		if s.fileExists(caddyhttp.SanitizedPathJoin(root, bigPath)) {
			return &bigPath
		}
	}

	for _, m := range s.Cmd.Order.matchers {
		if !strings.HasPrefix(reqPath, m.path) {
			continue
		}
		bigPath := reqPath + m.ext
		if s.fileExists(caddyhttp.SanitizedPathJoin(root, bigPath)) {
			return &bigPath
		}
	}

	if len(s.Cmd.Order.CatchAll) > 0 {
		for _, ca := range s.Cmd.Order.CatchAll {
			cad := path.Dir(ca)
			if !strings.HasPrefix(reqPath, cad) {
				continue
			}
			candidate := caddyhttp.SanitizedPathJoin(root, ca)
			if s.fileExists(candidate) {
				return &ca
			}
		}
	}

	return nil
}

func (s *SubstrateHandler) matchPath(r *http.Request) bool {
	for _, p := range s.Cmd.Order.Paths {
		if p == r.URL.Path {
			return true
		}
	}

	return false
}

func (s SubstrateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if s.Cmd == nil || s.Cmd.Order == nil {
		return next.ServeHTTP(w, r)
	}

	useProxy := s.matchPath(r)

	if !useProxy {
		match := s.findBestResource(r)
		if match != nil {
			useProxy = true
			r.URL.Path = *match
		}
	}

	if useProxy {
		s.proxy.SetHost(s.Cmd.Order.Host)
		if s.Cmd.Prefix != "" {
			r.URL.Path = strings.TrimPrefix(r.URL.Path, s.Cmd.Prefix)
		}
		var scheme string
		if r.TLS == nil {
			scheme = "http"
		} else {
			scheme = "https"
		}
		r.Header.Set("X-Forwarded-Path", r.RequestURI)
		r.Header.Set("X-Forwarded-BaseURL",
			fmt.Sprintf("%s://%s%s", scheme, r.Host, s.Cmd.Prefix))
		return s.proxy.ServeHTTP(w, r, next)
	}

	return next.ServeHTTP(w, r)
}

