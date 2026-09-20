// goby-notification-receiver is an independent, standard-library-only receiver
// and console client for GobyWebhookV1. It is not a vendor push gateway.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type reference struct {
	Kind      string
	Id        string
	LibraryId string `json:",omitempty"`
}
type message struct {
	Version        int
	EventId        string
	RegistrationId string
	Generation     string
	Kind           string
	OccurredAt     time.Time
	References     []reference
	Recursive      bool
}
type receiver struct {
	credentialFile, targetFile, directory string
	mu                                    sync.Mutex
	failures                              int
}

func id(value string) bool {
	if len(value) != 32 {
		return false
	}
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 16 && strings.ToLower(value) == value
}
func secret(path string) ([]byte, error) {
	stat, err := os.Lstat(path)
	if err != nil || !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 || stat.Size() > 2050 {
		return nil, errors.New("invalid private secret file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("secret unavailable")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 2051))
	if err != nil {
		return nil, errors.New("secret unavailable")
	}
	raw = []byte(strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r"))
	if len(raw) < 16 || len(raw) > 2048 {
		return nil, errors.New("invalid secret length")
	}
	return raw, nil
}
func (r *receiver) serve(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost || req.URL.Path != "/events" || req.URL.RawQuery != "" {
		http.NotFound(w, req)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(req.Body, 32769))
	if err != nil || len(raw) > 32768 {
		http.Error(w, "Invalid payload", 400)
		return
	}
	for _, name := range []string{"X-Goby-Timestamp", "X-Goby-Signature", "X-Goby-Target-Token", "X-Goby-Notification-Version"} {
		if len(req.Header.Values(name)) != 1 {
			http.Error(w, "Invalid authentication", 401)
			return
		}
	}
	timestamp := req.Header.Get("X-Goby-Timestamp")
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(seconds, 0)).Abs() > 5*time.Minute || req.Header.Get("X-Goby-Notification-Version") != "1" {
		http.Error(w, "Invalid authentication", 401)
		return
	}
	credential, err := secret(r.credentialFile)
	if err != nil {
		http.Error(w, "Receiver unavailable", 503)
		return
	}
	defer clear(credential)
	target, err := secret(r.targetFile)
	if err != nil {
		http.Error(w, "Receiver unavailable", 503)
		return
	}
	defer clear(target)
	mac := hmac.New(sha256.New, credential)
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(raw)
	signature, err := hex.DecodeString(req.Header.Get("X-Goby-Signature"))
	if err != nil || !hmac.Equal(mac.Sum(nil), signature) {
		http.Error(w, "Invalid authentication", 401)
		return
	}
	if subtle.ConstantTimeCompare(target, []byte(req.Header.Get("X-Goby-Target-Token"))) != 1 {
		http.Error(w, "Unknown target", 410)
		return
	}
	var msg message
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&msg) != nil || msg.Version != 1 || !id(msg.EventId) || !id(msg.RegistrationId) || len(msg.References) > 4096 {
		http.Error(w, "Invalid payload", 400)
		return
	}
	if _, err := strconv.ParseInt(msg.Generation, 10, 64); err != nil {
		http.Error(w, "Invalid generation", 400)
		return
	}
	switch msg.Kind {
	case "CatalogInvalidated", "UserDataInvalidated", "ResyncRequired", "Test":
	default:
		http.Error(w, "Unknown kind", 400)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failures > 0 {
		r.failures--
		w.Header().Set("Retry-After", "1")
		fmt.Printf("{\"EventId\":%q,\"Status\":503}\n", msg.EventId)
		http.Error(w, "Temporarily unavailable", 503)
		return
	}
	path := filepath.Join(r.directory, msg.EventId+".json")
	entries, err := os.ReadDir(r.directory)
	if err != nil || len(entries) > 4096 {
		http.Error(w, "Receipt capacity reached", 503)
		return
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		stat, statErr := os.Lstat(path)
		if statErr != nil || !stat.Mode().IsRegular() || stat.Size() > 32768 {
			http.Error(w, "Receipt unavailable", 503)
			return
		}
		old, readErr := os.ReadFile(path)
		var prior message
		if readErr != nil || json.Unmarshal(old, &prior) != nil || prior.EventId != msg.EventId || prior.RegistrationId != msg.RegistrationId || prior.Generation != msg.Generation {
			http.Error(w, "Receipt unavailable", 503)
			return
		}
	} else if err != nil {
		http.Error(w, "Receipt unavailable", 503)
		return
	} else {
		_, writeErr := file.Write(raw)
		syncErr := file.Sync()
		closeErr := file.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			_ = os.Remove(path)
			http.Error(w, "Receipt unavailable", 503)
			return
		}
		fmt.Printf("{\"EventId\":%q,\"Accepted\":true}\n", msg.EventId)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"EventId": msg.EventId, "Accepted": true})
}
func consume(ctx context.Context, directory string) error {
	seen := map[string]bool{}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return errors.New("receipt directory unavailable")
		}
		if len(entries) > 4096 {
			return errors.New("receipt capacity reached")
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".json") || !id(strings.TrimSuffix(name, ".json")) || seen[name] {
				continue
			}
			path := filepath.Join(directory, name)
			stat, err := os.Lstat(path)
			if err != nil || !stat.Mode().IsRegular() || stat.Size() > 32768 {
				continue
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var msg message
			if json.Unmarshal(raw, &msg) != nil || msg.EventId+".json" != name {
				continue
			}
			seen[name] = true
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"EventId": msg.EventId, "Kind": msg.Kind, "ReferenceCount": len(msg.References), "Consumed": true, "RequiresRefresh": msg.Kind != "Test"})
		}
	}
}
func run() error {
	mode := flag.String("mode", "receive", "receive or consume")
	listen := flag.String("listen", "127.0.0.1:8443", "HTTPS listen address")
	cert := flag.String("tls-cert", "", "TLS certificate path")
	key := flag.String("tls-key", "", "TLS private key path")
	credential := flag.String("credential-file", "", "private receiver credential file")
	target := flag.String("target-token-file", "", "private target token file")
	directory := flag.String("receipts", "", "private receipt directory")
	failures := flag.Int("transient-failures", 0, "authenticated requests to reject with 503 before accepting; controlled transport exercise")
	flag.Parse()
	if *directory == "" {
		return errors.New("a private receipt directory is required")
	}
	absolute, err := filepath.Abs(*directory)
	if err != nil {
		return errors.New("invalid receipt directory")
	}
	if err = os.MkdirAll(absolute, 0700); err != nil {
		return errors.New("receipt directory unavailable")
	}
	stat, err := os.Lstat(absolute)
	if err != nil || !stat.IsDir() || stat.Mode().Perm()&0077 != 0 {
		return errors.New("receipt directory must be private")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *mode == "consume" {
		return consume(ctx, absolute)
	}
	if *mode != "receive" || *cert == "" || *key == "" || *credential == "" || *target == "" {
		return errors.New("receive mode requires TLS and private credential files")
	}
	if *failures < 0 || *failures > 4 {
		return errors.New("transient failures must be between zero and four")
	}
	r := &receiver{credentialFile: *credential, targetFile: *target, directory: absolute, failures: *failures}
	server := &http.Server{Addr: *listen, Handler: http.HandlerFunc(r.serve), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 16 * 1024}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServeTLS(*cert, *key) }()
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("receiver stopped")
		}
		return nil
	case <-ctx.Done():
		closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		err := server.Shutdown(closeCtx)
		if err != nil {
			_ = server.Close()
		}
		<-done
		return err
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
