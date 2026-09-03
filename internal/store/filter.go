package store

import (
	"encoding/json"
	"fmt"
)

func encodeFilter(filter []Condition) (string, error) {
	for _, c := range filter {
		if !safeIdentifier(c.Column) || (c.Op != "=" && c.Op != "!=" && c.Op != "<" && c.Op != "<=" && c.Op != ">" && c.Op != ">=" && c.Op != "LIKE") {
			return "", fmt.Errorf("invalid filter condition")
		}
	}
	data, err := json.Marshal(filter)
	return string(data), err
}

func decodeFilter(value string) ([]Condition, error) {
	if value == "" {
		return nil, nil
	}
	var filter []Condition
	return filter, json.Unmarshal([]byte(value), &filter)
}
