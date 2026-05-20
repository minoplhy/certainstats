package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"strings"
)

type NetHeader struct {
	Token      [33]byte
	Flags      uint8
	StatsCount uint8
}

func (h *NetHeader) Version() uint8 {
	return h.Flags & 0x7F
}

func (h *NetHeader) IncludesDetails() bool {
	return (h.Flags>>7)&0x01 == 1
}

func decodeToJSON(encoded string) (string, error) {
	// Reverse the URL-style replacements:
	// %2F -> /
	// %2B -> +
	decodedURL, err := url.QueryUnescape(encoded)
	if err != nil {
		return "", fmt.Errorf("url decode failed: %w", err)
	}

	// Remove any accidental spaces/newlines
	decodedURL = strings.TrimSpace(decodedURL)

	// Base64 decode
	gzipData, err := base64.StdEncoding.DecodeString(decodedURL)
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}

	// Gunzip
	gzReader, err := gzip.NewReader(bytes.NewReader(gzipData))
	if err != nil {
		return "", fmt.Errorf("gzip reader failed: %w", err)
	}
	defer gzReader.Close()

	jsonBytes, err := io.ReadAll(gzReader)
	if err != nil {
		return "", fmt.Errorf("gzip read failed: %w", err)
	}

	return string(jsonBytes), nil
}

func main() {
	data := []byte{
		113, 116, 49, 88, 49, 106, 78, 106, 107, 89, 71, 52, 88, 76, 52, 52,
		114, 72, 57, 100, 110, 78, 100, 89, 116, 65, 108, 102, 122, 118, 110,
		105, 0, 1, 64,
	}

	var h NetHeader

	copy(h.Token[:], data[:33])
	h.Flags = data[33]
	h.StatsCount = data[34]

	fmt.Println("Token:", string(h.Token[:]))
	fmt.Println("Version:", h.Version())
	fmt.Println("IncludesDetails:", h.IncludesDetails())
	fmt.Println("StatsCount:", h.StatsCount)

	encoded := `H4sIAAAAAAAAA1WRu7ICIQxA%2F4XaQp2rhbWNtV%2BAEF1mXcIlsI46%2Frs4CaBVzknCI%2FBUM0Ry6NVOqYU6HvYM%2BgI%2BFVwWzgSRs0gcR4gerswDUvJ6ArbkKkX4j3BCTKw59JIJeUJbNyhGaEZI1NxghG5piKBtdwoAthnDTXOkepy0053jFbVdddx8ZYWjnsg9oEnP3nT4qXwSTU75fG5itBmkzzoa5Q7Ol2ErY6Da7mSIh%2BxmB47emdod5r9GWxkZvXxWgklWlg%2BapZrrC2Bw%2FiJHBVo1Wn%2Fo9QYvgHHi%2BAEAAA==`

	jsonStr, err := decodeToJSON(encoded)
	if err != nil {
		panic(err)
	}

	fmt.Println(jsonStr)
}
