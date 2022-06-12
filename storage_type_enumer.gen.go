// Code generated by "enumer -type=StorageType -yaml -trimprefix StorageType -output storage_type_enumer.gen.go"; DO NOT EDIT.

package gdnotify

import (
	"fmt"
	"strings"
)

const _StorageTypeName = "DynamoDBFile"

var _StorageTypeIndex = [...]uint8{0, 8, 12}

const _StorageTypeLowerName = "dynamodbfile"

func (i StorageType) String() string {
	if i < 0 || i >= StorageType(len(_StorageTypeIndex)-1) {
		return fmt.Sprintf("StorageType(%d)", i)
	}
	return _StorageTypeName[_StorageTypeIndex[i]:_StorageTypeIndex[i+1]]
}

// An "invalid array index" compiler error signifies that the constant values have changed.
// Re-run the stringer command to generate them again.
func _StorageTypeNoOp() {
	var x [1]struct{}
	_ = x[StorageTypeDynamoDB-(0)]
	_ = x[StorageTypeFile-(1)]
}

var _StorageTypeValues = []StorageType{StorageTypeDynamoDB, StorageTypeFile}

var _StorageTypeNameToValueMap = map[string]StorageType{
	_StorageTypeName[0:8]:       StorageTypeDynamoDB,
	_StorageTypeLowerName[0:8]:  StorageTypeDynamoDB,
	_StorageTypeName[8:12]:      StorageTypeFile,
	_StorageTypeLowerName[8:12]: StorageTypeFile,
}

var _StorageTypeNames = []string{
	_StorageTypeName[0:8],
	_StorageTypeName[8:12],
}

// StorageTypeString retrieves an enum value from the enum constants string name.
// Throws an error if the param is not part of the enum.
func StorageTypeString(s string) (StorageType, error) {
	if val, ok := _StorageTypeNameToValueMap[s]; ok {
		return val, nil
	}

	if val, ok := _StorageTypeNameToValueMap[strings.ToLower(s)]; ok {
		return val, nil
	}
	return 0, fmt.Errorf("%s does not belong to StorageType values", s)
}

// StorageTypeValues returns all values of the enum
func StorageTypeValues() []StorageType {
	return _StorageTypeValues
}

// StorageTypeStrings returns a slice of all String values of the enum
func StorageTypeStrings() []string {
	strs := make([]string, len(_StorageTypeNames))
	copy(strs, _StorageTypeNames)
	return strs
}

// IsAStorageType returns "true" if the value is listed in the enum definition. "false" otherwise
func (i StorageType) IsAStorageType() bool {
	for _, v := range _StorageTypeValues {
		if i == v {
			return true
		}
	}
	return false
}

// MarshalYAML implements a YAML Marshaler for StorageType
func (i StorageType) MarshalYAML() (interface{}, error) {
	return i.String(), nil
}

// UnmarshalYAML implements a YAML Unmarshaler for StorageType
func (i *StorageType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	var err error
	*i, err = StorageTypeString(s)
	return err
}
