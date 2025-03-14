package gdnotify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Songmu/flextime"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/drive/v3"
)

type stubHandler struct {
	mu                 sync.RWMutex
	t                  *testing.T
	router             *mux.Router
	channels           map[string]drive.Channel
	channelIdByDriveId map[string]string
	changes            map[string][]*drive.Change
}

func NewStub(t *testing.T) (*httptest.Server, *stubHandler) {
	t.Helper()
	stub := &stubHandler{
		t:                  t,
		router:             mux.NewRouter(),
		channels:           make(map[string]drive.Channel),
		channelIdByDriveId: make(map[string]string),
		changes:            make(map[string][]*drive.Change),
	}
	stub.setupRoute()
	return httptest.NewServer(stub), stub
}

func (h *stubHandler) setupRoute() {
	h.router.HandleFunc("/drives", h.handleList).Methods(http.MethodGet)
	h.router.HandleFunc("/changes/startPageToken", h.handleStartPageToken).Methods(http.MethodGet)
	h.router.HandleFunc("/changes/watch", h.handleWatch).Methods(http.MethodPost)
	h.router.HandleFunc("/changes", h.handleChangeList).Methods(http.MethodGet)
}

func (h *stubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func (*stubHandler) handleList(w http.ResponseWriter, r *http.Request) {
	var resp drive.DriveList
	if pageToken := r.URL.Query().Get("pageToken"); pageToken == "next" {
		resp = drive.DriveList{
			Drives: []*drive.Drive{
				{Id: "drive3", Name: "Drive Three", Kind: "drive#drive", CreatedTime: "2024-01-01T12:00:00Z"},
				{Id: "drive4", Name: "Drive Four", Kind: "drive#drive", CreatedTime: "2024-02-01T12:00:00Z"},
			},
		}
	} else if pageToken == "" {
		resp = drive.DriveList{
			Drives: []*drive.Drive{
				{Id: "drive1", Name: "Drive One", Kind: "drive#drive", CreatedTime: "2024-01-01T12:00:00Z"},
				{Id: "drive2", Name: "Drive Two", Kind: "drive#drive", CreatedTime: "2024-02-01T12:00:00Z"},
			},
			NextPageToken: "next",
		}
	} else {
		http.Error(w, "invalid page token", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (*stubHandler) handleStartPageToken(w http.ResponseWriter, r *http.Request) {
	resp := &drive.StartPageToken{
		StartPageToken: "0",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *stubHandler) handleWatch(w http.ResponseWriter, r *http.Request) {
	var payload drive.Channel
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	driveId := r.URL.Query().Get("driveId")
	if driveId == "" {
		driveId = DefaultDriveID
	}
	uuidObj, err := uuid.NewRandom()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	payload.ResourceId = uuidObj.String()
	payload.ResourceUri = "https://www.googleapis.com/drive/v3/changes"
	payload.Expiration = flextime.Now().Add(24 * 7 * time.Hour).UnixMilli()
	payload.Type = "web_hook"
	payload.Kind = "api#channel"
	h.setChannel(driveId, payload)
	h.sendNotification(payload.Id, "sync")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(payload)
	require.NoError(h.t, err)
}

func (h *stubHandler) setChannel(driveId string, channel drive.Channel) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.channels[channel.Id] = channel
	h.channelIdByDriveId[driveId] = channel.Id
}

func (h *stubHandler) getChannel(channelId string) (drive.Channel, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	channel, ok := h.channels[channelId]
	return channel, ok
}

func (h *stubHandler) sendNotification(channelId string, state string) {
	channel, ok := h.getChannel(channelId)
	if !ok {
		h.t.Error("sendNotification but channel not found")
		return
	}
	req, err := http.NewRequest(http.MethodPost, channel.Address, nil)
	if err != nil {
		h.t.Error("failed to create request", err)
		return
	}
	req.Header.Set("X-Goog-Channel-Id", channel.Id)
	req.Header.Set("X-Goog-Resource-Id", channel.ResourceId)
	req.Header.Set("X-Goog-Resource-State", state)
	req.Header.Set("User-Agent", "APIs-Google;")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Error("failed to send notification", err)
		return
	}
	resp.Body.Close()
	require.Equal(h.t, http.StatusOK, resp.StatusCode)
}

func (h *stubHandler) getChannelByDriveId(driveId string) (drive.Channel, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	channelId, ok := h.channelIdByDriveId[driveId]
	if !ok {
		channelId, ok = h.channelIdByDriveId[DefaultDriveID]
		if !ok {
			return drive.Channel{}, false
		}
	}
	channel, ok := h.channels[channelId]
	return channel, ok
}

func (h *stubHandler) getChanges(driveID string) ([]*drive.Change, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	changes, ok := h.changes[driveID]
	return changes, ok
}

func (h *stubHandler) handleChangeList(w http.ResponseWriter, r *http.Request) {
	driveId := r.URL.Query().Get("driveId")
	if driveId == "" {
		driveId = DefaultDriveID
	}
	pageToken := r.URL.Query().Get("pageToken")
	nextPageToken := pageToken
	if pageToken == "" {
		http.Error(w, "missing pageToken", http.StatusBadRequest)
		return
	}
	changes, ok := h.getChanges(driveId)
	if !ok {
		changes = make([]*drive.Change, 0)
	}
	index, err := strconv.Atoi(pageToken)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if index < len(changes) {
		changes = changes[index:]
		nextPageToken = strconv.Itoa(len(changes))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(drive.ChangeList{
		Changes:           changes,
		NewStartPageToken: nextPageToken,
		Kind:              "drive#changeList",
	})
	require.NoError(h.t, err)
}

func (h *stubHandler) appendChanges(driveId string, changes []*drive.Change) {
	h.mu.Lock()
	defer h.mu.Unlock()
	currentChanges, ok := h.changes[driveId]
	if !ok {
		currentChanges = make([]*drive.Change, 0)
	}
	h.changes[driveId] = append(currentChanges, changes...)
}

func (h *stubHandler) AppendChanges(driveId string, changes ...*drive.Change) {
	h.appendChanges(driveId, changes)
	channel, ok := h.getChannelByDriveId(driveId)
	if !ok {
		return
	}
	h.sendNotification(channel.Id, "change")
}
