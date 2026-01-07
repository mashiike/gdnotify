// Package gdnotify provides a Google Drive change notification system for AWS.
//
// gdnotify monitors changes in Google Drive using the Push Notifications API
// and forwards them to Amazon EventBridge. It manages notification channels,
// handles webhook callbacks from Google Drive, and maintains channel state
// using DynamoDB or local file storage.
//
// # Architecture
//
// The package consists of three main components:
//
//   - [App]: Core application that coordinates channel management and change processing
//   - [Storage]: Persistent storage for notification channel state (DynamoDB or file-based)
//   - [Notification]: Event delivery to downstream systems (EventBridge or file-based)
//
// # Usage
//
// For CLI usage, create a [CLI] instance and call Run:
//
//	var cli gdnotify.CLI
//	ctx := context.Background()
//	exitCode := cli.Run(ctx)
//
// For programmatic usage, create an [App] instance:
//
//	storage, _ := gdnotify.NewStorage(ctx, storageOption)
//	notification, _ := gdnotify.NewNotification(ctx, notificationOption)
//	app, _ := gdnotify.New(appOption, storage, notification)
//	defer app.Close()
//
// # Google Drive Integration
//
// gdnotify uses the Google Drive API v3 Push Notifications feature.
// When changes occur in Google Drive, Google sends webhook callbacks
// to the configured endpoint. The app then fetches the actual changes
// using the Changes API and forwards them to EventBridge.
//
// # AWS Integration
//
// The package integrates with AWS services:
//   - DynamoDB for channel state persistence
//   - EventBridge for change event delivery
//   - Lambda for serverless deployment (via [github.com/fujiwara/ridge])
//
// # Deployment Modes
//
// gdnotify supports multiple deployment modes:
//   - Local HTTP server for development
//   - AWS Lambda with Function URL or API Gateway
//   - Hybrid mode supporting both local and Lambda environments
package gdnotify
