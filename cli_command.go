package gdnotify

type CLICommand int

//go:generate enumer -type=CLICommand -trimprefix CLICommand -transform=snake -output cli_command_enumer.gen.go $GOFILE
const (
	CLICommandList CLICommand = iota
	CLICommandServe
	CLICommandRegister
	CLICommandMaintenance
	CLICommandCleanup
	CLICommandSync
)

func (cmd CLICommand) Description() string {
	switch cmd {
	case CLICommandList:
		return "list notification channels"
	case CLICommandServe:
		return "serve webhook server"
	case CLICommandRegister:
		return "register a new notification channel for a drive for which a notification channel has not yet been set"
	case CLICommandMaintenance:
		return "re-register expired notification channels or register new unregistered channels."
	case CLICommandCleanup:
		return "remove all notification channels"
	default:
		return ""
	}
}
