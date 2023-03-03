package gdnotify

type RunMode int

//go:generate enumer -type=RunMode -trimprefix RunMode -transform=snake -output run_mode_enumer.gen.go $GOFILE
const (
	RunModeCLI RunMode = iota
	RunModeWebhook
	RunModeMaintainer
	RunModeSyncer
)
