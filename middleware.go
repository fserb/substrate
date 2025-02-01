package substrate

import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func (s *SubstrateHandler) fileExists(path string) bool {
	info, err := fs.Stat(s.fs, path)
	if err != nil {
		return false
	}

	if strings.HasSuffix(path, "/") {
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

	path := r.URL.Path

	if s.fileExists(caddyhttp.SanitizedPathJoin(root, path)) {
		return &path
	}

	for _, suffix := range s.Order.TryFiles {
		bigPath := path + suffix
		if s.fileExists(caddyhttp.SanitizedPathJoin(root, bigPath)) {
			return &bigPath
		}
	}

	return nil
}

func (s *SubstrateHandler) enableReverseProxy(r *http.Request) bool {
	for _, ext := range s.Order.Match {
		if strings.HasSuffix(r.URL.Path, ext) {
			return true
		}
	}
	return false
}

func (s SubstrateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if s.Order == nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		s.log.Error("No order")
		return nil
	}

	match := s.findBestResource(r)
	fmt.Printf("match: %s (path %s)\n", *match, r.URL.Path)
	if *match != r.URL.Path {
		r.Header.Set("X-Forwarded-Path", r.URL.Path)
		r.URL.Path = *match
	}

	if s.enableReverseProxy(r) {
		fmt.Println("enableReverseProxy")
		repl := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
		repl.Set("substrate.host", s.Order.Host)
	}

	return next.ServeHTTP(w, r)
}

