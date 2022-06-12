// Code generated by "enumer -type=NotificationType -yaml -trimprefix NotificationType -output notification_type_enumer.gen.go"; DO NOT EDIT.

package gdnotify

import (
	"fmt"
	"strings"
)

const _NotificationTypeName = "EventBridgeFile"

var _NotificationTypeIndex = [...]uint8{0, 11, 15}

const _NotificationTypeLowerName = "eventbridgefile"

func (i NotificationType) String() string {
	if i < 0 || i >= NotificationType(len(_NotificationTypeIndex)-1) {
		return fmt.Sprintf("NotificationType(%d)", i)
	}
	return _NotificationTypeName[_NotificationTypeIndex[i]:_NotificationTypeIndex[i+1]]
}

// An "invalid array index" compiler error signifies that the constant values have changed.
// Re-run the stringer command to generate them again.
func _NotificationTypeNoOp() {
	var x [1]struct{}
	_ = x[NotificationTypeEventBridge-(0)]
	_ = x[NotificationTypeFile-(1)]
}

var _NotificationTypeValues = []NotificationType{NotificationTypeEventBridge, NotificationTypeFile}

var _NotificationTypeNameToValueMap = map[string]NotificationType{
	_NotificationTypeName[0:11]:       NotificationTypeEventBridge,
	_NotificationTypeLowerName[0:11]:  NotificationTypeEventBridge,
	_NotificationTypeName[11:15]:      NotificationTypeFile,
	_NotificationTypeLowerName[11:15]: NotificationTypeFile,
}

var _NotificationTypeNames = []string{
	_NotificationTypeName[0:11],
	_NotificationTypeName[11:15],
}

// NotificationTypeString retrieves an enum value from the enum constants string name.
// Throws an error if the param is not part of the enum.
func NotificationTypeString(s string) (NotificationType, error) {
	if val, ok := _NotificationTypeNameToValueMap[s]; ok {
		return val, nil
	}

	if val, ok := _NotificationTypeNameToValueMap[strings.ToLower(s)]; ok {
		return val, nil
	}
	return 0, fmt.Errorf("%s does not belong to NotificationType values", s)
}

// NotificationTypeValues returns all values of the enum
func NotificationTypeValues() []NotificationType {
	return _NotificationTypeValues
}

// NotificationTypeStrings returns a slice of all String values of the enum
func NotificationTypeStrings() []string {
	strs := make([]string, len(_NotificationTypeNames))
	copy(strs, _NotificationTypeNames)
	return strs
}

// IsANotificationType returns "true" if the value is listed in the enum definition. "false" otherwise
func (i NotificationType) IsANotificationType() bool {
	for _, v := range _NotificationTypeValues {
		if i == v {
			return true
		}
	}
	return false
}

// MarshalYAML implements a YAML Marshaler for NotificationType
func (i NotificationType) MarshalYAML() (interface{}, error) {
	return i.String(), nil
}

// UnmarshalYAML implements a YAML Unmarshaler for NotificationType
func (i *NotificationType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	var err error
	*i, err = NotificationTypeString(s)
	return err
}
