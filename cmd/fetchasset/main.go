package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	url := flag.String("url", "", "URL to download")
	out := flag.String("out", "", "Output file path (written relative to the generator file's directory)")
	wantHex := flag.String("sha256", "", "Expected SHA256 hex (optional; if empty, tool prints the downloaded SHA256)")
	timeout := flag.Duration("timeout", 20*time.Second, "HTTP timeout")
	offlineOK := flag.Bool("offline-ok", true, "If true, allow using existing file when network fails (only if it matches expected sha256)")
	userAgent := flag.String("ua", "go-generate-fetchasset/1.0", "User-Agent")
	flag.Parse()

	if *url == "" || *out == "" {
		fatalf("usage: fetchasset -url=<url> -out=<file> [-sha256=<hex>] [flags]")
	}

	// Normalize expected hash (if provided)
	var want []byte
	if *wantHex != "" {
		h, err := decodeHex(*wantHex)
		if err != nil || len(h) != sha256.Size {
			fatalf("invalid -sha256: must be 64 hex chars")
		}
		want = h
	}

	// If we have an expected hash and the file already matches, skip download.
	if len(want) == sha256.Size {
		if ok, err := fileMatchesSHA256(*out, want); err == nil && ok {
			fmt.Printf("OK (cached): %s\n", *out)
			return
		}
	}

	// Try downloading.
	body, got, err := download(*url, *timeout, *userAgent)
	if err != nil {
		// If offline-ok and existing file matches expected hash, accept.
		if *offlineOK && len(want) == sha256.Size {
			if ok, ferr := fileMatchesSHA256(*out, want); ferr == nil && ok {
				fmt.Printf("OK (offline, using cached file): %s\n", *out)
				return
			}
		}
		fatalf("download failed: %v", err)
	}

	// If expected hash provided, verify.
	if len(want) == sha256.Size && !bytes.Equal(got, want) {
		fatalf("sha256 mismatch for %s\nwant: %s\ngot : %s",
			*url, hex.EncodeToString(want), hex.EncodeToString(got))
	}

	// If expected hash NOT provided, print it so you can paste it in.
	if len(want) == 0 {
		fmt.Printf("Downloaded: %s\nSHA256: %s\n", *url, hex.EncodeToString(got))
	}

	// Ensure parent dir exists, then write atomically.
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil && filepath.Dir(*out) != "." {
		fatalf("mkdir: %v", err)
	}
	if err := writeFileAtomic(*out, body, 0o644); err != nil {
		fatalf("write: %v", err)
	}
	fmt.Printf("Wrote: %s\n", *out)
}

func download(url string, timeout time.Duration, ua string) ([]byte, []byte, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, nil, err
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, nil, fmt.Errorf("http %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(data)
	return data, sum[:], nil
}

func fileMatchesSHA256(path string, want []byte) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(b)
	return bytes.Equal(sum[:], want), nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	f, err := os.CreateTemp(dir, base+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}()

	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Chmod(perm); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func decodeHex(s string) ([]byte, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "sha256:")
	if len(s) != 64 {
		return nil, errors.New("wrong length")
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}
