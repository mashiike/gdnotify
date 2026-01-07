package gdnotify

// DetailType constants for EventBridge events.
const (
	DetailTypeFileRemoved  = "File Removed"
	DetailTypeFileTrashed  = "File Move to trash"
	DetailTypeFileChanged  = "File Changed"
	DetailTypeDriveRemoved = "Shared Drive Removed"
	DetailTypeDriveChanged = "Drive Status Changed"
)
