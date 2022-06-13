package gdnotify

import (
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	logx "github.com/mashiike/go-logx"
)

func (app *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	channelID := r.Header.Get("X-Goog-Channel-Id")
	state := r.Header.Get("X-Goog-Resource-State")
	userAgent := r.Header.Get("User-Agent")
	resourceID := r.Header.Get("X-Goog-Resource-Id")
	logx.Printf(ctx, "[info] method:%s uri:%s user_agent:%s channel_id:%s resource_id:%s resource_state:%s message_number:%s forwarded_for:%s",
		coalesce(r.Method, "-"),
		coalesce(r.URL.String(), "-"),
		url.QueryEscape(coalesce(userAgent, "-")),
		coalesce(channelID, "-"),
		coalesce(resourceID, "-"),
		coalesce(state, "-"),
		coalesce(r.Header.Get("X-Goog-Message-Number"), "-"),
		coalesce(r.Header.Get("X-Forwarded-For"), "-"),
	)
	defer r.Body.Close()
	if d, err := httputil.DumpRequest(r, true); err == nil {
		logx.Println(ctx, "[debug] receive request\n", string(d))
	}
	if !strings.HasPrefix(userAgent, "APIs-Google;") {
		logx.Printf(ctx, "[warn]  user-agent unexpected return 404: `%s`", userAgent)
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, http.StatusText(http.StatusNotFound))
		return
	}
	if state == "sync" {
		logx.Printf(ctx, "[info] sync accepted channel_id:%s resource_id:%s",
			coalesce(channelID, "-"),
			coalesce(resourceID, "-"),
		)
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, http.StatusText(http.StatusOK))
		return
	}
	if state != "change" {
		logx.Printf(ctx, "[warn] unknown state:%s channel_id:%s resource_id:%s",
			coalesce(state, "-"),
			coalesce(channelID, "-"),
			coalesce(resourceID, "-"),
		)
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, http.StatusText(http.StatusOK))
		return
	}
	logx.Printf(ctx, "[info] change accepted channel_id:%s resource_id:%s",
		coalesce(channelID, "-"),
		coalesce(resourceID, "-"),
	)
	changes, item, err := app.ChangesList(ctx, channelID)
	if err != nil {
		logx.Printf(ctx, "[error] get changes list failed channel_id:%s resource_id:%s err:%s",
			coalesce(channelID, "-"),
			coalesce(resourceID, "-"),
			err.Error(),
		)
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, http.StatusText(http.StatusInternalServerError))
		return
	}
	if len(changes) > 0 {
		logx.Printf(ctx, "[debug] send changes channel_id:%s resource_id:%s",
			coalesce(channelID, "-"),
			coalesce(resourceID, "-"),
		)
		if err := app.notification.SendChanges(ctx, item, changes); err != nil {
			logx.Printf(ctx, "[error] send changes failed channel_id:%s resource_id:%s err:%s",
				coalesce(channelID, "-"),
				coalesce(resourceID, "-"),
				err.Error(),
			)
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, http.StatusText(http.StatusInternalServerError))
			return
		}
	} else {
		logx.Printf(ctx, "[debug] no changes channel_id:%s resource_id:%s",
			coalesce(channelID, "-"),
			coalesce(resourceID, "-"),
		)
	}
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, http.StatusText(http.StatusOK))
	return
}

func coalesce(strs ...string) string {
	for _, str := range strs {
		if str != "" {
			return str
		}
	}
	return ""
}
