package invalid

import (
	"log/slog"
	"net"
	"net/http"

	"github.com/glup3/immich-public-proxy/internal/config"
)

type Handler struct {
	mode   config.InvalidResponseMode
	logger *slog.Logger
}

func New(mode config.InvalidResponseMode, logger *slog.Logger) Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return Handler{mode: mode, logger: logger}
}

func (h Handler) Respond(w http.ResponseWriter, defaultResponse int, logMessage string) {
	switch {
	case h.mode.RedirectURL != "":
		h.logger.Info("redirect invalid request", "location", h.mode.RedirectURL, "message", logMessage)
		w.Header().Set("Location", h.mode.RedirectURL)
		w.WriteHeader(http.StatusFound)
	case h.mode.Drop:
		h.drop(w, defaultResponse, logMessage)
	case h.mode.StatusCode > 0:
		h.logger.Info("return invalid response", "status", h.mode.StatusCode, "message", logMessage)
		w.WriteHeader(h.mode.StatusCode)
	default:
		h.logger.Info("return invalid response", "status", defaultResponse, "message", logMessage)
		w.WriteHeader(defaultResponse)
	}
}

func (h Handler) drop(w http.ResponseWriter, fallbackStatus int, logMessage string) {
	h.logger.Info("drop invalid request", "message", logMessage)
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
