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
	Subject string  `json:"subject" cel:"subject"`
	Entity  *Entity `json:"entity" cel:"entity"`
	Actor   *User   `json:"actor" cel:"actor"`
	Change  *Change `json:"change" cel:"change"`
	S3Copy  *S3Copy `json:"s3Copy,omitempty" cel:"s3Copy"`
}

// Entity represents the file or drive that was changed.
type Entity struct {
	ID          string `json:"id" cel:"id"`
	Kind        string `json:"kind" cel:"kind"`
	Name        string `json:"name,omitempty" cel:"name"`
	CreatedTime string `json:"createdTime,omitempty" cel:"createdTime"`
}

// User represents a Google Drive user.
type User struct {
	Kind         string `json:"kind" cel:"kind"`
	DisplayName  string `json:"displayName" cel:"displayName"`
	EmailAddress string `json:"emailAddress,omitempty" cel:"emailAddress"`
	PhotoLink    string `json:"photoLink,omitempty" cel:"photoLink"`
	Me           bool   `json:"me,omitempty" cel:"me"`
	PermissionID string `json:"permissionId,omitempty" cel:"permissionId"`
}

// Change represents a change to a file or shared drive.
type Change struct {
	Kind       string `json:"kind" cel:"kind"`
	ChangeType string `json:"changeType" cel:"changeType"`
	Time       string `json:"time" cel:"time"`
	Removed    bool   `json:"removed,omitempty" cel:"removed"`
	FileID     string `json:"fileId,omitempty" cel:"fileId"`
	File       *File  `json:"file,omitempty" cel:"file"`
	DriveID    string `json:"driveId,omitempty" cel:"driveId"`
	Drive      *Drive `json:"drive,omitempty" cel:"drive"`
}

// File represents a Google Drive file.
type File struct {
	Kind              string   `json:"kind" cel:"kind"`
	ID                string   `json:"id" cel:"id"`
	Name              string   `json:"name" cel:"name"`
	MimeType          string   `json:"mimeType" cel:"mimeType"`
	Size              string   `json:"size,omitempty" cel:"size"`
	Version           string   `json:"version,omitempty" cel:"version"`
	CreatedTime       string   `json:"createdTime,omitempty" cel:"createdTime"`
	ModifiedTime      string   `json:"modifiedTime,omitempty" cel:"modifiedTime"`
	TrashedTime       string   `json:"trashedTime,omitempty" cel:"trashedTime"`
	Trashed           bool     `json:"trashed,omitempty" cel:"trashed"`
	Parents           []string `json:"parents,omitempty" cel:"parents"`
	Parent            *Folder  `json:"parent,omitempty" cel:"parent"`
	LastModifyingUser *User    `json:"lastModifyingUser,omitempty" cel:"lastModifyingUser"`
	TrashingUser      *User    `json:"trashingUser,omitempty" cel:"trashingUser"`
}

// Drive represents a Google shared drive.
type Drive struct {
	Kind        string `json:"kind" cel:"kind"`
	ID          string `json:"id" cel:"id"`
	Name        string `json:"name" cel:"name"`
	CreatedTime string `json:"createdTime,omitempty" cel:"createdTime"`
}

// Folder represents a Google Drive folder.
type Folder struct {
	ID   string `json:"id" cel:"id"`
	Name string `json:"name" cel:"name"`
}

// S3Copy contains information about a file copied to S3.
type S3Copy struct {
	S3URI       string    `json:"s3Uri" cel:"s3Uri"`
	ContentType string    `json:"contentType" cel:"contentType"`
	Size        int64     `json:"size" cel:"size"`
	CopiedAt    time.Time `json:"copiedAt" cel:"copiedAt"`
}
