package config

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestConvertSize(t *testing.T) {
	tiny, _ := ParseSize("tiny")
	small, _ := ParseSize("small")
	medium, _ := ParseSize("medium")
	large, _ := ParseSize("large")
	assert.Equal(t, Tiny, tiny)
	assert.Equal(t, Small, small)
	assert.Equal(t, Medium, medium)
	assert.Equal(t, Large, large)

	nothing, err := ParseSize("invalid")
	assert.Error(t, err)
	assert.Nil(t, nothing)
}
