package gdnotify

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sebdah/goldie/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

func TestApp(t *testing.T) {
	stubServer, stub := NewStub(t)
	defer stubServer.Close()
	tmpDir := t.TempDir()
	ctx := context.Background()
	dataFilePath := filepath.Join(tmpDir, "gdnotify.data")
	storage, err := NewStorage(ctx, StorageOption{
		Type:     "file",
		LockFile: filepath.Join(tmpDir, "gdnotify.lock"),
		DataFile: dataFilePath,
	})
	eventFilePath := filepath.Join(tmpDir, "gdnotify.json")
	require.NoError(t, err)
	notification, err := NewNotification(ctx, NotificationOption{
		Type:      "file",
		EventFile: eventFilePath,
	})
	require.NoError(t, err)
	app, err := New(AppOption{}, storage, notification, option.WithoutAuthentication(), option.WithEndpoint(stubServer.URL))
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	appServer := httptest.NewServer(app)
	defer appServer.Close()
	req := httptest.NewRequest(http.MethodPost, appServer.URL+"/sync", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	app.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	stub.AppendChanges("drive1", &drive.Change{
		ChangeType: "kind#change",
		Drive: &drive.Drive{
			Kind: "kind#drive",
			Id:   "drive1",
		},
		File: &drive.File{
			Kind: "kind#file",
			Id:   "file1",
		},
	})

	g := goldie.New(
		t,
		goldie.WithFixtureDir("testdata"),
		goldie.WithNameSuffix(".golden.json"),
	)
	bs, err := os.ReadFile(eventFilePath)
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(bs, &m))
	g.AssertJson(t, "event", m)
}
