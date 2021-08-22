package config

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestConvertSize(t *testing.T) {
	tiny, _ := ConvertSize("tiny")
	small, _ := ConvertSize("small")
	medium, _ := ConvertSize("medium")
	large, _ := ConvertSize("large")
	assert.Equal(t, Tiny, tiny)
	assert.Equal(t, Small, small)
	assert.Equal(t, Medium, medium)
	assert.Equal(t, Large, large)

	nothing, err := ConvertSize("invalid")
	assert.Error(t, err)
	assert.Nil(t, nothing)
}
