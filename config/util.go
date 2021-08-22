package config

import (
	"errors"
)

// ConvertSize converts a size string to a Size
func ConvertSize(size string) (*Size, error) {
	switch size {
	case Tiny.Name, Small.Name, Medium.Name, Large.Name:
		return Sizes[size], nil
	default:
		return nil, errors.New("invalid size")
	}
}
