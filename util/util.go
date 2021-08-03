package util

import (
	"math/rand"
	"time"
)

var (
	random          = rand.New(rand.NewSource(time.Now().UnixNano()))
	charsetRandomID = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

func RandomID(length int) string {
	return RandomStringWithCharset(10, charsetRandomID)
}

// RandomStringWithCharset returns a random string with a given length, using the defined charset
func RandomStringWithCharset(length int, charset string) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[random.Intn(len(charset))]
	}
	return string(b)
}

func StringContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
