package gdnotify_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mashiike/gdnotify"
	"github.com/sebdah/goldie/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/drive/v3"
)

func TestConvertToDetailMarshalJSON(t *testing.T) {
	g := goldie.New(t,
		goldie.WithFixtureDir("./testdata"),
		goldie.WithNameSuffix(".golden.json"),
	)
	cases := []struct {
		name   string
		change *drive.Change
	}{
		{
			name:   "all blank",
			change: &drive.Change{},
		},
		{
			name: "changed removed",
			change: &drive.Change{
				Kind:       "drive#change",
				ChangeType: "file",
				FileId:     "XXXXXXXXXX",
				Removed:    true,
				Time:       "2022-06-15T00:03:55.849Z",
			},
		},
		{
			name: "changed file",
			change: &drive.Change{
				Kind:       "drive#change",
				ChangeType: "file",
				FileId:     "XXXXXXXXXX",
				File: &drive.File{
					Id:   "XXXXXXXXXX",
					Kind: "drive#file",
					LastModifyingUser: &drive.User{
						DisplayName: "hoge",
						Kind:        "drive#user",
					},
					MimeType:     "application/vnd.google-apps.spreadsheet",
					ModifiedTime: "2022-06-15T00:03:45.843Z",
					Name:         "gdnotify",
					Version:      20,
					Size:         1500,
				},
				Time: "2022-06-15T00:03:55.849Z",
			},
		},
		{
			name: "changed file with email",
			change: &drive.Change{
				Kind:       "drive#change",
				ChangeType: "file",
				FileId:     "XXXXXXXXXX",
				File: &drive.File{
					Id:   "XXXXXXXXXX",
					Kind: "drive#file",
					LastModifyingUser: &drive.User{
						DisplayName:  "hoge",
						EmailAddress: "hoge@example.com",
						Kind:         "drive#user",
					},
					MimeType:     "application/vnd.google-apps.spreadsheet",
					ModifiedTime: "2022-06-15T00:03:45.843Z",
					Name:         "gdnotify",
					Version:      20,
					Size:         1500,
				},
				Time: "2022-06-15T00:03:55.849Z",
			},
		},
		{
			name: "changed file no file detail",
			change: &drive.Change{
				Kind:       "drive#change",
				ChangeType: "file",
				FileId:     "XXXXXXXXXX",
				Time:       "2022-06-15T00:03:55.849Z",
			},
		},
		{
			name: "trashed file by unknown user",
			change: &drive.Change{
				Kind:       "drive#change",
				ChangeType: "file",
				FileId:     "XXXXXXXXXX",
				File: &drive.File{
					Id:   "XXXXXXXXXX",
					Kind: "drive#file",
					LastModifyingUser: &drive.User{
						DisplayName: "hoge",
						Kind:        "drive#user",
					},
					MimeType:     "application/vnd.google-apps.spreadsheet",
					ModifiedTime: "2022-06-15T00:03:45.843Z",
					Name:         "gdnotify",
					Trashed:      true,
					Version:      20,
					Size:         1500,
				},
				Time: "2022-06-15T00:03:55.849Z",
			},
		},
		{
			name: "trashed file",
			change: &drive.Change{
				Kind:       "drive#change",
				ChangeType: "file",
				FileId:     "XXXXXXXXXX",
				File: &drive.File{
					Id:   "XXXXXXXXXX",
					Kind: "drive#file",
					LastModifyingUser: &drive.User{
						DisplayName: "hoge",
						Kind:        "drive#user",
					},
					MimeType:     "application/vnd.google-apps.spreadsheet",
					ModifiedTime: "2022-06-15T00:03:45.843Z",
					Name:         "gdnotify",
					Trashed:      true,
					TrashingUser: &drive.User{
						DisplayName: "fuga",
						Kind:        "drive#user",
					},
					TrashedTime: "2022-06-15T00:03:52.347Z",
					Version:     20,
					Size:        1500,
				},
				Time: "2022-06-15T00:03:55.849Z",
			},
		},
		{
			name: "drive removed",
			change: &drive.Change{
				Kind:       "drive#change",
				ChangeType: "drive",
				DriveId:    "XXXXXXXXXX",
				Removed:    true,
				Time:       "2022-06-15T00:03:55.849Z",
			},
		},
		{
			name: "drive change",
			change: &drive.Change{
				Kind:       "drive#change",
				ChangeType: "drive",
				DriveId:    "XXXXXXXXXX",
				Drive: &drive.Drive{
					Id:   "XXXXXXXXXX",
					Name: "gdnotify",
					Kind: "drive#drive",
				},
				Time: "2022-06-15T00:03:55.849Z",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			detail := gdnotify.ConvertToDetail(c.change)
			bs, err := json.MarshalIndent(detail, "", "  ")
			require.NoError(t, err, "marshal")
			g.Assert(t, strings.ReplaceAll(c.name, " ", "_"), bs)
		})
	}
}
