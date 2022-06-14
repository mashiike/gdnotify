// Code generated by "enumer -type=RunMode -trimprefix RunMode -transform=snake -output run_mode_enumer.gen.go"; DO NOT EDIT.

package gdnotify

import (
	"fmt"
	"strings"
)

const _RunModeName = "cliwebhookmaintainer"

var _RunModeIndex = [...]uint8{0, 3, 10, 20}

const _RunModeLowerName = "cliwebhookmaintainer"

func (i RunMode) String() string {
	if i < 0 || i >= RunMode(len(_RunModeIndex)-1) {
		return fmt.Sprintf("RunMode(%d)", i)
	}
	return _RunModeName[_RunModeIndex[i]:_RunModeIndex[i+1]]
}

// An "invalid array index" compiler error signifies that the constant values have changed.
// Re-run the stringer command to generate them again.
func _RunModeNoOp() {
	var x [1]struct{}
	_ = x[RunModeCLI-(0)]
	_ = x[RunModeWebhook-(1)]
	_ = x[RunModeMaintainer-(2)]
}

var _RunModeValues = []RunMode{RunModeCLI, RunModeWebhook, RunModeMaintainer}

var _RunModeNameToValueMap = map[string]RunMode{
	_RunModeName[0:3]:        RunModeCLI,
	_RunModeLowerName[0:3]:   RunModeCLI,
	_RunModeName[3:10]:       RunModeWebhook,
	_RunModeLowerName[3:10]:  RunModeWebhook,
	_RunModeName[10:20]:      RunModeMaintainer,
	_RunModeLowerName[10:20]: RunModeMaintainer,
}

var _RunModeNames = []string{
	_RunModeName[0:3],
	_RunModeName[3:10],
	_RunModeName[10:20],
}

// RunModeString retrieves an enum value from the enum constants string name.
// Throws an error if the param is not part of the enum.
func RunModeString(s string) (RunMode, error) {
	if val, ok := _RunModeNameToValueMap[s]; ok {
		return val, nil
	}

	if val, ok := _RunModeNameToValueMap[strings.ToLower(s)]; ok {
		return val, nil
	}
	return 0, fmt.Errorf("%s does not belong to RunMode values", s)
}

// RunModeValues returns all values of the enum
func RunModeValues() []RunMode {
	return _RunModeValues
}

// RunModeStrings returns a slice of all String values of the enum
func RunModeStrings() []string {
	strs := make([]string, len(_RunModeNames))
	copy(strs, _RunModeNames)
	return strs
}

// IsARunMode returns "true" if the value is listed in the enum definition. "false" otherwise
func (i RunMode) IsARunMode() bool {
	for _, v := range _RunModeValues {
		if i == v {
			return true
		}
	}
	return false
}