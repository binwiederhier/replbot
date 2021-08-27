package config

import (
	"errors"
)

// ParseSize converts a size string to a Size
func ParseSize(size string) (*Size, error) {
	switch size {
	case Tiny.Name, Small.Name, Medium.Name, Large.Name:
		return Sizes[size], nil
	default:
		return nil, errors.New("invalid size")
	}
}
