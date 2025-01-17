package myhome

import (
	"crypto/rand"
	"math/big"
	"unsafe"
)

// https://stackoverflow.com/questions/22892120/how-to-generate-a-random-string-of-a-fixed-length-in-go
func RandStringBytesMaskImprRandReaderUnsafe(length uint) (string, error) {
	const (
		charset     = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		charIdxBits = 6                  // 6 bits to represent a letter index
		charIdxMask = 1<<charIdxBits - 1 // All 1-bits, as many as charIdxBits
		charIdxMax  = 63 / charIdxBits   // # of letter indices fitting in 63 bits
	)

	buffer := make([]byte, length)
	charsetLength := len(charset)
	max := big.NewInt(int64(1 << uint64(charsetLength)))

	limit, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}

	for index, cache, remain := int(length-1), limit.Int64(), charIdxMax; index >= 0; {
		if remain == 0 {
			limit, err = rand.Int(rand.Reader, max)
			if err != nil {
				return "", err
			}

			cache, remain = limit.Int64(), charIdxMax
		}

		if idx := int(cache & charIdxMask); idx < charsetLength {
			buffer[index] = charset[idx]
			index--
		}

		cache >>= charIdxBits
		remain--
	}

	return *(*string)(unsafe.Pointer(&buffer)), nil
}
