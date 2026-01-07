// Package gdnotifyevent provides types for gdnotify EventBridge event payloads.
// These types can be used in Lambda functions to unmarshal gdnotify events.
//
//	func handler(ctx context.Context, event gdnotifyevent.Event) error {
//	    fmt.Println(event.DetailType)
//	    fmt.Println(event.Detail.Subject)
//	}
package gdnotifyevent

import "time"

// Event represents the full EventBridge event from gdnotify.
type Event struct {
	Version    string    `json:"version"`
	ID         string    `json:"id"`
	DetailType string    `json:"detail-type"`
	Source     string    `json:"source"`
	AccountID  string    `json:"account"`
	Time       time.Time `json:"time"`
	Region     string    `json:"region"`
	Resources  []string  `json:"resources"`
	Detail     Detail    `json:"detail"`
}

// Detail is the event detail payload.
type Detail struct {
	Subject string  `json:"subject"`
	Entity  *Entity `json:"entity"`
	Actor   *User   `json:"actor"`
	Change  *Change `json:"change"`
	S3Copy  *S3Copy `json:"s3Copy,omitempty"`
}

// Entity represents the file or drive that was changed.
type Entity struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name,omitempty"`
	CreatedTime string `json:"createdTime,omitempty"`
}

// User represents a Google Drive user.
type User struct {
	Kind         string `json:"kind"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress,omitempty"`
	PhotoLink    string `json:"photoLink,omitempty"`
	Me           bool   `json:"me,omitempty"`
	PermissionID string `json:"permissionId,omitempty"`
}

// Change represents a change to a file or shared drive.
type Change struct {
	Kind       string `json:"kind"`
	ChangeType string `json:"changeType"`
	Time       string `json:"time"`
	Removed    bool   `json:"removed,omitempty"`
	FileID     string `json:"fileId,omitempty"`
	File       *File  `json:"file,omitempty"`
	DriveID    string `json:"driveId,omitempty"`
	Drive      *Drive `json:"drive,omitempty"`
}

// File represents a Google Drive file.
type File struct {
	Kind              string   `json:"kind"`
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	MimeType          string   `json:"mimeType"`
	Size              string   `json:"size,omitempty"`
	Version           string   `json:"version,omitempty"`
	CreatedTime       string   `json:"createdTime,omitempty"`
	ModifiedTime      string   `json:"modifiedTime,omitempty"`
	TrashedTime       string   `json:"trashedTime,omitempty"`
	Trashed           bool     `json:"trashed,omitempty"`
	Parents           []string `json:"parents,omitempty"`
	LastModifyingUser *User    `json:"lastModifyingUser,omitempty"`
	TrashingUser      *User    `json:"trashingUser,omitempty"`
}

// Drive represents a Google shared drive.
type Drive struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	CreatedTime string `json:"createdTime,omitempty"`
}

// S3Copy contains information about a file copied to S3.
type S3Copy struct {
	S3URI       string    `json:"s3Uri"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	CopiedAt    time.Time `json:"copiedAt"`
}
