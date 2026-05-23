package server

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/glup3/immich-public-proxy/internal/config"
	"github.com/glup3/immich-public-proxy/internal/immich"
	"github.com/glup3/immich-public-proxy/internal/render"
	"github.com/glup3/immich-public-proxy/internal/session"
)

type shareMode string

const (
	shareModeView     shareMode = ""
	shareModeDownload shareMode = "download"
)

type Options struct {
	Config        config.Config
	Client        *immich.Client
	Sessions      *session.Store
	Logger        *slog.Logger
	PublicBaseURL string
}

type Server struct {
	config   config.Config
	client   *immich.Client
	renderer *render.Renderer
	sessions *session.Store
	router   chi.Router
	logger   *slog.Logger
}

func New(options Options) (*Server, error) {
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.Client == nil {
		return nil, fmt.Errorf("immich client is required")
	}
	if options.Sessions == nil {
		return nil, fmt.Errorf("session manager is required")
	}
	renderer, err := render.New(options.Config, options.PublicBaseURL)
	if err != nil {
		return nil, err
	}
	srv := &Server{
		config:   options.Config,
		client:   options.Client,
		renderer: renderer,
		sessions: options.Sessions,
		router:   chi.NewRouter(),
		logger:   options.Logger,
	}
	srv.routes()
	return srv, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.router.Get("/healthcheck", s.healthcheck)
	s.router.Get("/share/healthcheck", s.healthcheck)

	s.router.Get("/share/static/*", s.static("/share/static/"))
	s.router.Get("/robots.txt", s.staticRoot)
	s.router.Get("/favicon.ico", s.staticRoot)

	s.router.Get("/{shareType:share|s}/{key}", s.share)
	s.router.Get("/{shareType:share|s}/{key}/download", s.share)
	s.router.Post("/share/unlock", s.unlock)
	s.router.Post("/{shareType:share|s}/{key}", s.redirectPostShare)
	s.router.Post("/{shareType:share|s}/{key}/download", s.redirectPostShare)

	s.router.Get("/share/{type:photo|video}/{key}/{id}", s.asset)
	s.router.Get("/share/{type:photo|video}/{key}/{id}/{size}", s.asset)

	if s.config.IPP.ShowHomePage {
		s.router.Get("/", s.home)
		s.router.Get("/share", s.home)
		s.router.Get("/share/", s.home)
	}
	s.router.Get("/*", s.staticRootOrNotFound)
}

func Run(ctx context.Context, addr string, handler http.Handler, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	httpServer := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server started", "addr", addr)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (s *Server) healthcheck(w http.ResponseWriter, r *http.Request) {
	if s.client.Accessible(r.Context()) {
		_, _ = w.Write([]byte("ok"))
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
}

func (s *Server) share(w http.ResponseWriter, r *http.Request) {
	s.config.AddResponseHeaders(w.Header())
	shareType := chi.URLParam(r, "shareType")
	key := chi.URLParam(r, "key")
	mode := shareModeView
	if strings.HasSuffix(r.URL.Path, "/download") {
		mode = shareModeDownload
	}

	keyType := immich.KeyTypeFromShare(shareType)
	if keyType == immich.KeyTypeSlug && !s.config.IPP.AllowSlugLinks {
		s.respondInvalid(w, http.StatusNotFound, "slug links are disabled")
		return
	}
	if !immich.IsKey(key) {
		s.respondInvalid(w, http.StatusNotFound, "wrong key format "+key)
		return
	}

	password := s.sessions.PasswordForShare(r, key)
	link, access, err := s.client.FetchSharedLink(r.Context(), key, password, keyType)
	if err != nil {
		s.logger.Error("fetch shared link", "key", key, "key_type", keyType, "error", err)
		s.respondInvalid(w, http.StatusNotFound, "invalid request")
		return
	}
	if access == immich.ShareAccessInvalid {
		s.respondInvalid(w, http.StatusNotFound, "invalid request")
		return
	}

	invalidPassword := access == immich.ShareAccessPasswordRequired && password != ""
	if invalidPassword {
		s.logger.Info("invalid password", "key", key)
		_ = s.sessions.ForgetShare(w, r, key)
	}
	if access == immich.ShareAccessPasswordRequired || password != "" {
		setNoStoreHeaders(w.Header())
	}
	if access == immich.ShareAccessPasswordRequired {
		if invalidPassword {
			w.WriteHeader(http.StatusUnauthorized)
		}
		if err := s.renderer.Password(w, sanitizeKey(key), password != ""); err != nil {
			s.logger.Error("render password page", "error", err)
		}
		return
	}

	if password != "" && s.sessions.PasswordForShare(r, link.Key) == "" {
		if err := s.sessions.RememberPassword(w, r, link.Key, password); err != nil {
			s.logger.Error("store session password", "key", link.Key, "error", err)
		}
	}

	if mode == shareModeDownload && render.CanDownload(s.config, &link) {
		if err := s.downloadAll(r.Context(), w, &link); err != nil {
			s.logger.Error("download all", "key", key, "error", err)
			s.respondInvalid(w, http.StatusNotFound, "download failed")
		}
		return
	}
	if len(link.Assets) == 1 {
		s.logger.Info("serving link", "key", key)
		asset := link.Assets[0]
		if asset.Type == immich.AssetTypeImage && !s.config.IPP.SingleImageGallery && password == "" {
			s.serveAsset(w, r, asset, immich.ImageSizePreview)
			return
		}
		openItem := 0
		if s.config.IPP.SingleItemAutoOpen {
			openItem = 1
		}
		if err := s.renderer.Gallery(w, r, &link, openItem, render.CanDownload(s.config, &link)); err != nil {
			s.logger.Error("render gallery", "error", err)
		}
		return
	}

	s.logger.Info("serving link", "key", key)
	if err := s.renderer.Gallery(w, r, &link, 0, render.CanDownload(s.config, &link)); err != nil {
		s.logger.Error("render gallery", "error", err)
	}
}

func (s *Server) unlock(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key      string `json:"key"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if body.Key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	if err := s.sessions.RememberPassword(w, r, body.Key, body.Password); err != nil {
		s.logger.Error("store session password", "key", body.Key, "error", err)
		http.Error(w, "unable to store password", http.StatusInternalServerError)
		return
	}
}

func (s *Server) redirectPostShare(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, r.URL.RequestURI(), http.StatusSeeOther)
}

func (s *Server) asset(w http.ResponseWriter, r *http.Request) {
	s.config.AddResponseHeaders(w.Header())
	assetType := chi.URLParam(r, "type")
	key := chi.URLParam(r, "key")
	id := chi.URLParam(r, "id")
	size := chi.URLParam(r, "size")

	if !immich.IsKey(key) || !immich.IsID(id) {
		s.respondInvalid(w, http.StatusNotFound, "invalid key or ID for "+r.URL.Path)
		return
	}
	if size != "" && !immich.IsImageSize(size) {
		s.respondInvalid(w, http.StatusNotFound, "invalid size parameter "+r.URL.Path)
		return
	}

	password := s.sessions.PasswordForShare(r, key)
	link, access, err := s.client.FetchSharedLink(r.Context(), key, password, immich.KeyTypeKey)
	if err != nil {
		s.logger.Error("fetch shared link for asset", "key", key, "error", err)
		s.respondInvalid(w, http.StatusNotFound, "invalid share link")
		return
	}
	if access == immich.ShareAccessPasswordRequired {
		http.Redirect(w, r, "/share/"+key, http.StatusFound)
		return
	}
	if access == immich.ShareAccessInvalid {
		s.respondInvalid(w, http.StatusNotFound, "invalid share link")
		return
	}

	asset, ok := findAsset(link.Assets, id)
	if !ok {
		s.respondInvalid(w, http.StatusNotFound, "asset not found in share")
		return
	}
	if assetType == "video" {
		asset.Type = immich.AssetTypeVideo
	} else {
		asset.Type = immich.AssetTypeImage
	}
	s.serveAsset(w, r, asset, immich.ImageSize(size))
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request, asset immich.Asset, size immich.ImageSize) {
	resp, err := s.client.StreamAsset(r.Context(), asset, size, r.Header.Get("Range"), s.config.IPP.DownloadOriginalPhoto)
	if err != nil {
		s.logger.Error("proxy asset", "asset_id", asset.ID, "error", err)
		s.respondInvalid(w, http.StatusNotFound, "failed response from immich for asset "+asset.ID)
		return
	}
	defer resp.Body.Close()

	if size == immich.ImageSizeOriginal && asset.OriginalFileName != "" && s.config.IPP.DownloadOriginalPhoto {
		w.Header().Set("Content-Disposition", `attachment; filename="`+render.Filename(s.config, asset)+`"`)
	}
	copyHeaders(w.Header(), resp.Header, []string{
		"Content-Type",
		"Content-Length",
		"Last-Modified",
		"ETag",
		"Cache-Control",
		"Content-Range",
	})
	if asset.Type == immich.AssetTypeVideo {
		w.Header().Set("Accept-Ranges", "bytes")
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) downloadAll(ctx context.Context, w http.ResponseWriter, share *immich.SharedLink) error {
	w.Header().Set("Content-Type", "application/zip")
	filename := render.SafeTitleFilename(render.Title(share))
	if filename == "" {
		filename = "photos"
	}
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+filename+".zip")

	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()

	for _, asset := range share.Assets {
		resp, err := s.client.DownloadAsset(ctx, asset, s.config.IPP.DownloadOriginalPhoto)
		if err != nil {
			s.logger.Warn("download asset", "asset_id", asset.ID, "error", err)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			s.logger.Warn("download asset unexpected status", "asset_id", asset.ID, "status", resp.StatusCode)
			_ = resp.Body.Close()
			continue
		}
		writer, err := zipWriter.Create(render.Filename(s.config, asset))
		if err != nil {
			_ = resp.Body.Close()
			return err
		}
		_, copyErr := io.Copy(writer, resp.Body)
		closeErr := resp.Body.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func (s *Server) home(w http.ResponseWriter, _ *http.Request) {
	if err := s.renderer.Home(w); err != nil {
		s.logger.Error("render home", "error", err)
	}
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.respondInvalid(w, http.StatusNotFound, "invalid route "+r.URL.Path)
}

func (s *Server) static(prefix string) http.HandlerFunc {
	fs := http.StripPrefix(prefix, http.FileServer(http.Dir("public")))
	return func(w http.ResponseWriter, r *http.Request) {
		cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, prefix))
		if cleanPath == "." || strings.HasPrefix(cleanPath, "..") {
			s.notFound(w, r)
			return
		}
		info, err := os.Stat(filepath.Join("public", cleanPath))
		if err != nil || info.IsDir() {
			s.notFound(w, r)
			return
		}
		s.config.AddResponseHeaders(w.Header())
		fs.ServeHTTP(w, r)
	}
}

func (s *Server) staticRoot(w http.ResponseWriter, r *http.Request) {
	s.config.AddResponseHeaders(w.Header())
	http.FileServer(http.Dir("public")).ServeHTTP(w, r)
}

func (s *Server) staticRootOrNotFound(w http.ResponseWriter, r *http.Request) {
	cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if cleanPath == "." || strings.HasPrefix(cleanPath, "..") {
		s.notFound(w, r)
		return
	}
	info, err := os.Stat(filepath.Join("public", cleanPath))
	if err != nil || info.IsDir() {
		s.notFound(w, r)
		return
	}
	s.staticRoot(w, r)
}

func sanitizeKey(key string) string {
	var b strings.Builder
	for _, r := range key {
		if r == '_' || r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func setNoStoreHeaders(header http.Header) {
	header.Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
	header.Set("Pragma", "no-cache")
	header.Set("Expires", "0")
}

func copyHeaders(dst, src http.Header, keys []string) {
	for _, key := range keys {
		if value := src.Get(key); value != "" {
			dst.Set(key, value)
		}
	}
}

func findAsset(assets []immich.Asset, id string) (immich.Asset, bool) {
	for _, asset := range assets {
		if asset.ID == id {
			return asset, true
		}
	}
	return immich.Asset{}, false
}

func (s *Server) respondInvalid(w http.ResponseWriter, defaultStatus int, message string) {
	mode := s.config.IPP.CustomInvalidResponse
	switch {
	case mode.RedirectURL != "":
		s.logger.Info("redirect invalid request", "location", mode.RedirectURL, "message", message)
		w.Header().Set("Location", mode.RedirectURL)
		w.WriteHeader(http.StatusFound)
	case mode.Drop:
		s.dropConnection(w, defaultStatus, message)
	case mode.StatusCode > 0:
		s.logger.Info("return invalid response", "status", mode.StatusCode, "message", message)
		w.WriteHeader(mode.StatusCode)
	default:
		s.logger.Info("return invalid response", "status", defaultStatus, "message", message)
		w.WriteHeader(defaultStatus)
	}
}

func (s *Server) dropConnection(w http.ResponseWriter, fallbackStatus int, message string) {
	s.logger.Info("drop invalid request", "message", message)
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		w.WriteHeader(fallbackStatus)
		return
	}
	conn, _, err := hijacker.Hijack()
	if err != nil {
		w.WriteHeader(fallbackStatus)
		return
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetLinger(0)
	}
	_ = conn.Close()
}
