package gdnotify

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func (app *App) setupRoute() {
	app.router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, http.StatusOK, http.StatusText(http.StatusOK))
	})
	sub := app.router.NewRoute().Subrouter()
	sub.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			app.checkWebhookAddress(r)
			next.ServeHTTP(w, r)
		})
	})
	sub.HandleFunc("/", app.handleWebhook).Methods(http.MethodPost)
	sub.HandleFunc("/sync", app.handleSync).Methods(http.MethodPost)
}

func (app *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	app.router.ServeHTTP(w, r)
}

func (app *App) handleWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	channelID := r.Header.Get("X-Goog-Channel-Id")
	state := r.Header.Get("X-Goog-Resource-State")
	userAgent := r.Header.Get("User-Agent")
	resourceID := r.Header.Get("X-Goog-Resource-Id")
	slog.InfoContext(ctx, "Received webhook request",
		"method", coalesce(r.Method, "-"),
		"uri", coalesce(r.URL.String(), "-"),
		"user_agent", url.QueryEscape(coalesce(userAgent, "-")),
		"channel_id", coalesce(channelID, "-"),
		"resource_id", coalesce(resourceID, "-"),
		"resource_state", coalesce(state, "-"),
		"message_number", coalesce(r.Header.Get("X-Goog-Message-Number"), "-"),
		"forwarded_for", coalesce(r.Header.Get("X-Forwarded-For"), "-"),
		"channel_expiration", coalesce(r.Header.Get("X-Goog-Channel-Expiration"), "-"),
	)
	defer r.Body.Close()
	if d, err := httputil.DumpRequest(r, true); err == nil {
		slog.DebugContext(ctx, "Received request dump", "request", string(d))
	}
	if !strings.HasPrefix(userAgent, "APIs-Google;") {
		slog.WarnContext(ctx, "Unexpected user-agent, returning 404", "user_agent", userAgent)
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, http.StatusText(http.StatusNotFound))
		return
	}
	if state == "sync" {
		slog.InfoContext(ctx, "Sync accepted", "channel_id", coalesce(channelID, "-"), "resource_id", coalesce(resourceID, "-"))
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, http.StatusText(http.StatusOK))
		return
	}
	if state != "change" {
		slog.WarnContext(ctx, "Unknown state", "state", state, "channel_id", coalesce(channelID, "-"), "resource_id", coalesce(resourceID, "-"))
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, http.StatusText(http.StatusOK))
		return
	}
	slog.InfoContext(ctx, "Change accepted", "channel_id", coalesce(channelID, "-"), "resource_id", coalesce(resourceID, "-"))
	changes, item, err := app.ChangesList(ctx, channelID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get changes list", "channel_id", coalesce(channelID, "-"), "resource_id", coalesce(resourceID, "-"), "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, http.StatusText(http.StatusInternalServerError))
		return
	}
	if len(changes) > 0 {
		slog.DebugContext(ctx, "Sending changes", "channel_id", coalesce(channelID, "-"), "resource_id", coalesce(resourceID, "-"))
		if err := app.SendNotification(ctx, item, changes); err != nil {
			slog.ErrorContext(ctx, "Failed to send changes", "channel_id", coalesce(channelID, "-"), "resource_id", coalesce(resourceID, "-"), "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, http.StatusText(http.StatusInternalServerError))
			return
		}
	} else {
		slog.DebugContext(ctx, "No changes", "channel_id", coalesce(channelID, "-"), "resource_id", coalesce(resourceID, "-"))
	}
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, http.StatusText(http.StatusOK))
}

func (app *App) handleSync(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	var hasErr bool
	if err := app.maintenanceChannels(ctx, false); err != nil {
		slog.WarnContext(ctx, "Maintenance channels failed", "details", err)
		hasErr = true
	}
	if err := app.syncChannels(ctx); err != nil {
		slog.WarnContext(ctx, "Sync channels failed", "details", err)
		hasErr = true
	}
	if hasErr {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, http.StatusText(http.StatusInternalServerError))
		return
	}
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, http.StatusText(http.StatusOK))
}

func coalesce(strs ...string) string {
	for _, str := range strs {
		if str != "" {
			return str
		}
	}
	return ""
}
