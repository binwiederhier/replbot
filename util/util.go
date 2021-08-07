package util

import (
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"time"
)

var (
	random                  = rand.New(rand.NewSource(time.Now().UnixNano()))
	charsetRandomID         = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	invalidTmuxIDCharsRegex = regexp.MustCompile(`[^A-Za-z0-9]`)
)

func SanitizeID(id string) string {
	id = fmt.Sprintf("replbot_%s", id)
	return invalidTmuxIDCharsRegex.ReplaceAllString(id, "_")
}

// RandomID generates a random string ID
func RandomID(length int) string {
	return RandomStringWithCharset(length, charsetRandomID)
}

// RandomStringWithCharset returns a random string with a given length, using the defined charset
func RandomStringWithCharset(length int, charset string) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[random.Intn(len(charset))]
	}
	return string(b)
}

// StringContains returns true if the needle is contained in the haystack
func StringContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// FileExists returns true if a file with the given filename exists
func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}
