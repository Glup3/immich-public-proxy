package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/alangrainger/immich-public-proxy/internal/config"
	"github.com/alangrainger/immich-public-proxy/internal/immich"
	"github.com/alangrainger/immich-public-proxy/internal/invalid"
	"github.com/alangrainger/immich-public-proxy/internal/render"
	"github.com/alangrainger/immich-public-proxy/internal/session"
	"github.com/alangrainger/immich-public-proxy/internal/types"
)

type App struct {
	Config   config.Config
	Immich   *immich.Client
	Renderer *render.Renderer
	Session  *session.Manager
	Invalid  invalid.Handler
	Router   chi.Router
}

func New(cfg config.Config, immichClient *immich.Client, sessions *session.Manager) (*App, error) {
	invalidHandler := invalid.New(cfg)
	renderer, err := render.New(cfg, immichClient, invalidHandler)
	if err != nil {
		return nil, err
	}
	app := &App{
		Config:   cfg,
		Immich:   immichClient,
		Renderer: renderer,
		Session:  sessions,
		Invalid:  invalidHandler,
		Router:   chi.NewRouter(),
	}
	app.routes()
	return app, nil
}

func (a *App) routes() {
	a.Router.Get("/healthcheck", a.healthcheck)
	a.Router.Get("/share/healthcheck", a.healthcheck)

	a.Router.Get("/share/static/*", a.static("/share/static/"))
	a.Router.Get("/robots.txt", a.staticRoot)
	a.Router.Get("/favicon.ico", a.staticRoot)

	a.Router.Get("/{shareType:share|s}/{key}", a.share)
	a.Router.Get("/{shareType:share|s}/{key}/download", a.share)
	a.Router.Post("/share/unlock", a.unlock)
	a.Router.Post("/{shareType:share|s}/{key}", a.redirectPostShare)
	a.Router.Post("/{shareType:share|s}/{key}/download", a.redirectPostShare)

	a.Router.Get("/share/{type:photo|video}/{key}/{id}", a.asset)
	a.Router.Get("/share/{type:photo|video}/{key}/{id}/{size}", a.asset)

	if a.Config.IPP.ShowHomePage {
		a.Router.Get("/", a.home)
		a.Router.Get("/share", a.home)
		a.Router.Get("/share/", a.home)
	}
	a.Router.Get("/*", a.staticRootOrNotFound)
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.Router.ServeHTTP(w, r)
}

func (a *App) healthcheck(w http.ResponseWriter, _ *http.Request) {
	if a.Immich.Accessible() {
		_, _ = w.Write([]byte("ok"))
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
}

func (a *App) share(w http.ResponseWriter, r *http.Request) {
	a.Config.AddResponseHeaders(w.Header())
	shareType := chi.URLParam(r, "shareType")
	key := chi.URLParam(r, "key")
	mode := ""
	if strings.HasSuffix(r.URL.Path, "/download") {
		mode = "download"
	}
	keyType := immich.KeyTypeFromShare(shareType)
	if keyType == types.KeyTypeSlug && !a.Config.IPP.AllowSlugLinks {
		a.Invalid.Respond(w, http.StatusNotFound, "Slug links are disabled in config.json")
		return
	}
	password := a.Session.Password(r, key)
	a.handleShareRequest(w, r, types.IncomingShareRequest{
		Request:  r,
		Key:      key,
		KeyType:  keyType,
		Mode:     mode,
		Password: password,
	})
}

func (a *App) handleShareRequest(w http.ResponseWriter, r *http.Request, incoming types.IncomingShareRequest) {
	if !immich.IsKey(incoming.Key) {
		a.Invalid.Respond(w, http.StatusNotFound, "Wrong key format "+incoming.Key)
		return
	}

	sharedLinkRes := a.Immich.GetShareByKey(incoming.Key, incoming.Password, incoming.KeyType)
	if !sharedLinkRes.Valid {
		a.Invalid.Respond(w, http.StatusNotFound, "Invalid request")
		return
	}

	invalidPassword := sharedLinkRes.PasswordRequired && incoming.Password != ""
	if invalidPassword {
		invalid.Log("Invalid password for key " + incoming.Key)
		a.Session.ClearKey(w, r, incoming.Key)
	}
	if sharedLinkRes.PasswordRequired || incoming.Password != "" {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
	}
	if sharedLinkRes.PasswordRequired {
		key := sanitizeKey(incoming.Key)
		if invalidPassword {
			w.WriteHeader(http.StatusUnauthorized)
		}
		a.Renderer.Password(w, key, incoming.Password != "")
		return
	}
	if sharedLinkRes.Link == nil {
		a.Invalid.Respond(w, http.StatusNotFound, "Unknown error with key "+incoming.Key)
		return
	}
	link := sharedLinkRes.Link

	if incoming.Password != "" && a.Session.Password(r, link.Key) == "" {
		a.Session.SetPassword(w, r, link.Key, incoming.Password)
	}

	if incoming.Mode == "download" && render.CanDownload(a.Config, link) {
		a.Renderer.DownloadAll(w, link)
		return
	}
	if len(link.Assets) == 1 {
		invalid.Log("Serving link " + incoming.Key)
		asset := link.Assets[0]
		if asset.Type == types.AssetTypeImage && !a.Config.IPP.SingleImageGallery && incoming.Password == "" {
			a.Renderer.AssetBuffer(incoming, w, asset, types.ImageSizePreview)
			return
		}
		openItem := 0
		if a.Config.IPP.SingleItemAutoOpen {
			openItem = 1
		}
		a.Renderer.Gallery(w, r, link, openItem)
		return
	}

	invalid.Log("Serving link " + incoming.Key)
	a.Renderer.Gallery(w, r, link, 0)
}

func (a *App) unlock(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key      string `json:"key"`
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Key != "" {
		a.Session.SetPassword(w, r, body.Key, body.Password)
	}
}

func (a *App) redirectPostShare(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, r.URL.RequestURI(), http.StatusSeeOther)
}

func (a *App) asset(w http.ResponseWriter, r *http.Request) {
	a.Config.AddResponseHeaders(w.Header())
	assetType := chi.URLParam(r, "type")
	key := chi.URLParam(r, "key")
	id := chi.URLParam(r, "id")
	size := chi.URLParam(r, "size")

	if !immich.IsKey(key) || !immich.IsID(id) {
		a.Invalid.Respond(w, http.StatusNotFound, "Invalid key or ID for "+r.URL.Path)
		return
	}
	if size != "" && !immich.IsImageSize(size) {
		a.Invalid.Respond(w, http.StatusNotFound, "Invalid size parameter "+r.URL.Path)
		return
	}

	password := a.Session.Password(r, key)
	share := a.Immich.GetShareByKey(key, password, types.KeyTypeKey)
	if !share.Valid {
		a.Invalid.Respond(w, http.StatusNotFound, "Invalid share link")
		return
	}
	if share.PasswordRequired {
		http.Redirect(w, r, "/share/"+key, http.StatusFound)
		return
	}
	if share.Link == nil {
		a.Invalid.Respond(w, http.StatusNotFound, "Invalid share link")
		return
	}

	var matched *types.Asset
	for i := range share.Link.Assets {
		if share.Link.Assets[i].ID == id {
			matched = &share.Link.Assets[i]
			break
		}
	}
	if matched == nil {
		a.Invalid.Respond(w, http.StatusNotFound, "Asset not found in share")
		return
	}
	asset := *matched
	if assetType == "video" {
		asset.Type = types.AssetTypeVideo
	} else {
		asset.Type = types.AssetTypeImage
	}
	a.Renderer.AssetBuffer(types.IncomingShareRequest{
		Request: r,
		Key:     key,
		Range:   r.Header.Get("Range"),
	}, w, asset, size)
}

func (a *App) home(w http.ResponseWriter, _ *http.Request) {
	a.Renderer.Home(w)
}

func (a *App) notFound(w http.ResponseWriter, r *http.Request) {
	a.Invalid.Respond(w, http.StatusNotFound, "Invalid route "+r.URL.Path)
}

func (a *App) static(prefix string) http.HandlerFunc {
	fs := http.StripPrefix(prefix, http.FileServer(http.Dir("public")))
	return func(w http.ResponseWriter, r *http.Request) {
		cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, prefix))
		if cleanPath == "." || strings.HasPrefix(cleanPath, "..") {
			a.notFound(w, r)
			return
		}
		info, err := os.Stat(filepath.Join("public", cleanPath))
		if err != nil || info.IsDir() {
			a.notFound(w, r)
			return
		}
		a.Config.AddResponseHeaders(w.Header())
		fs.ServeHTTP(w, r)
	}
}

func (a *App) staticRoot(w http.ResponseWriter, r *http.Request) {
	a.Config.AddResponseHeaders(w.Header())
	http.FileServer(http.Dir("public")).ServeHTTP(w, r)
}

func (a *App) staticRootOrNotFound(w http.ResponseWriter, r *http.Request) {
	cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if cleanPath == "." || strings.HasPrefix(cleanPath, "..") {
		a.notFound(w, r)
		return
	}
	info, err := os.Stat(filepath.Join("public", cleanPath))
	if err != nil || info.IsDir() {
		a.notFound(w, r)
		return
	}
	a.staticRoot(w, r)
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

func Run(app *App) error {
	port := os.Getenv("IPP_PORT")
	if port == "" {
		port = "3000"
	}
	server := &http.Server{
		Addr:    ":" + port,
		Handler: app,
	}
	errCh := make(chan error, 1)
	go func() {
		invalid.Log("Server started on port " + port)
		errCh <- server.ListenAndServe()
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGTERM, os.Interrupt)

	select {
	case sig := <-signalCh:
		invalid.Log("Received " + sig.String() + ". Gracefully shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
