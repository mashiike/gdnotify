package gdnotify

import (
	"fmt"

	"github.com/mashiike/gdnotify/pkg/gdnotifyevent"
	"google.golang.org/api/drive/v3"
)

func ConvertChange(c *drive.Change) *gdnotifyevent.Change {
	if c == nil {
		return nil
	}
	return &gdnotifyevent.Change{
		Kind:       c.Kind,
		ChangeType: c.ChangeType,
		Time:       c.Time,
		Removed:    c.Removed,
		FileID:     c.FileId,
		File:       ConvertFile(c.File),
		DriveID:    c.DriveId,
		Drive:      ConvertDrive(c.Drive),
	}
}

// ConvertToDetail converts a drive.Change to a gdnotifyevent.Detail with Subject, Entity, and Actor populated.
func ConvertToDetail(c *drive.Change) *gdnotifyevent.Detail {
	if c == nil {
		return nil
	}
	change := ConvertChange(c)
	detail := &gdnotifyevent.Detail{
		Change: change,
	}

	detailType := DetailType(change)

	switch detailType {
	case DetailTypeFileRemoved:
		detail.Subject = fmt.Sprintf("FileID %s was removed at %s", c.FileId, c.Time)
	case DetailTypeFileTrashed:
		if c.File != nil {
			if c.File.TrashingUser != nil {
				user := formatUser(c.File.TrashingUser)
				detail.Subject = fmt.Sprintf("File %s (%s) moved to trash by %s at %s", c.File.Name, c.FileId, user, c.File.TrashedTime)
				detail.Actor = ConvertUser(c.File.TrashingUser)
			} else {
				detail.Subject = fmt.Sprintf("File %s (%s) moved to trash at %s", c.File.Name, c.FileId, c.Time)
			}
		} else {
			detail.Subject = fmt.Sprintf("FileID %s moved to trash at %s", c.FileId, c.Time)
		}
	case DetailTypeFileChanged:
		if c.File != nil {
			if c.File.LastModifyingUser != nil {
				user := formatUser(c.File.LastModifyingUser)
				detail.Subject = fmt.Sprintf("File %s (%s) changed by %s at %s", c.File.Name, c.FileId, user, c.File.ModifiedTime)
				detail.Actor = ConvertUser(c.File.LastModifyingUser)
			} else {
				detail.Subject = fmt.Sprintf("File %s (%s) changed at %s", c.File.Name, c.FileId, c.Time)
			}
		} else {
			detail.Subject = fmt.Sprintf("FileID %s changed at %s", c.FileId, c.Time)
		}
	case DetailTypeDriveRemoved:
		detail.Subject = fmt.Sprintf("DriveId %s was removed at %s", c.DriveId, c.Time)
	case DetailTypeDriveChanged:
		if c.Drive != nil {
			detail.Subject = fmt.Sprintf("Drive %s (%s) changed at %s", c.Drive.Name, c.DriveId, c.Time)
		} else {
			detail.Subject = fmt.Sprintf("DriveId %s changed at %s", c.DriveId, c.Time)
		}
	}

	if detail.Actor == nil {
		detail.Actor = &gdnotifyevent.User{
			Kind:        "drive#user",
			DisplayName: "Unknown User",
		}
	}

	switch {
	case c.Drive != nil:
		detail.Entity = &gdnotifyevent.Entity{
			ID:          c.Drive.Id,
			Kind:        c.Drive.Kind,
			Name:        c.Drive.Name,
			CreatedTime: c.Drive.CreatedTime,
		}
	case c.File != nil:
		detail.Entity = &gdnotifyevent.Entity{
			ID:          c.File.Id,
			Kind:        c.File.Kind,
			Name:        c.File.Name,
			CreatedTime: c.File.CreatedTime,
		}
	case c.DriveId != "":
		detail.Entity = &gdnotifyevent.Entity{
			ID:   c.DriveId,
			Kind: "drive#drive",
		}
	case c.FileId != "":
		detail.Entity = &gdnotifyevent.Entity{
			ID:   c.FileId,
			Kind: "drive#file",
		}
	}

	return detail
}

func formatUser(u *drive.User) string {
	if u == nil {
		return "Unknown User"
	}
	if u.EmailAddress == "" {
		return u.DisplayName
	}
	return fmt.Sprintf("%s [%s]", u.DisplayName, u.EmailAddress)
}

func ConvertFile(f *drive.File) *gdnotifyevent.File {
	if f == nil {
		return nil
	}
	return &gdnotifyevent.File{
		Kind:              f.Kind,
		ID:                f.Id,
		Name:              f.Name,
		MimeType:          f.MimeType,
		Size:              formatSize(f.Size),
		Version:           formatVersion(f.Version),
		CreatedTime:       f.CreatedTime,
		ModifiedTime:      f.ModifiedTime,
		TrashedTime:       f.TrashedTime,
		Trashed:           f.Trashed,
		Parents:           f.Parents,
		LastModifyingUser: ConvertUser(f.LastModifyingUser),
		TrashingUser:      ConvertUser(f.TrashingUser),
	}
}

func ConvertUser(u *drive.User) *gdnotifyevent.User {
	if u == nil {
		return nil
	}
	return &gdnotifyevent.User{
		Kind:         u.Kind,
		DisplayName:  u.DisplayName,
		EmailAddress: u.EmailAddress,
		PhotoLink:    u.PhotoLink,
		Me:           u.Me,
		PermissionID: u.PermissionId,
	}
}

func ConvertDrive(d *drive.Drive) *gdnotifyevent.Drive {
	if d == nil {
		return nil
	}
	return &gdnotifyevent.Drive{
		Kind:        d.Kind,
		ID:          d.Id,
		Name:        d.Name,
		CreatedTime: d.CreatedTime,
	}
}

func formatSize(size int64) string {
	if size == 0 {
		return ""
	}
	return fmt.Sprintf("%d", size)
}

func formatVersion(version int64) string {
	if version == 0 {
		return ""
	}
	return fmt.Sprintf("%d", version)
}

// DetailType returns the EventBridge detail-type for a change.
func DetailType(c *gdnotifyevent.Change) string {
	if c == nil {
		return "Unexpected Changed"
	}
	switch c.ChangeType {
	case "file":
		switch {
		case c.Removed:
			return DetailTypeFileRemoved
		case c.File != nil && c.File.Trashed:
			return DetailTypeFileTrashed
		default:
			return DetailTypeFileChanged
		}
	case "drive":
		switch {
		case c.Removed:
			return DetailTypeDriveRemoved
		default:
			return DetailTypeDriveChanged
		}
	default:
		return "Unexpected Changed"
	}
}

func eventSource(sourcePrefix string, c *gdnotifyevent.Change) string {
	if c == nil {
		return sourcePrefix
	}
	switch c.ChangeType {
	case "file":
		return fmt.Sprintf("%s/file/%s", sourcePrefix, c.FileID)
	case "drive":
		return fmt.Sprintf("%s/drive/%s", sourcePrefix, c.DriveID)
	default:
		return fmt.Sprintf("%s/%s", sourcePrefix, c.ChangeType)
	}
}
