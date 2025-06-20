package utils

import "fmt"

// NumberToHex converts any numeric type to a hex string with "0x" prefix
// Supports int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64
func NumberToHex[T ~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64](num T) string {
	return fmt.Sprintf("0x%x", num)
}
