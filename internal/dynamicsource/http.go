package dynamicsource

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/media"
)

const probeSampleBytes = 128 * 1024

func cloneHeaders(headers map[string]string) map[string]string {
	result := make(map[string]string, len(headers))
	for name, value := range headers {
		result[name] = value
	}
	return result
}

func validateDefinition(definition Definition) error {
	if !validID(definition.ItemID, false) || !validID(definition.Name, true) || definition.MaxReconnects < 0 || definition.MaxReconnects > 3 {
		return ErrInvalid
	}
	if err := ValidateSubtitleDefinitions(definition.Subtitles); err != nil {
		return err
	}
	return validateHTTPRequest(definition.URL, definition.Headers)
}

func validateHTTPRequest(address string, headers map[string]string) error {
	if len(address) > 8192 || len(headers) > 32 {
		return ErrInvalid
	}
	target, err := url.Parse(address)
	if err != nil || target.Hostname() == "" || target.User != nil || target.Fragment != "" || target.Opaque != "" ||
		(target.Scheme != "http" && target.Scheme != "https") || strings.ContainsAny(address, "\r\n\x00") {
		return ErrInvalid
	}
	total := 0
	seen := make(map[string]bool, len(headers))
	for name, value := range headers {
		if name == "" || len(name) > 128 || len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\x00") {
			return ErrInvalid
		}
		for _, character := range name {
			if !strings.ContainsRune("!#$%&'*+-.^_`|~0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", character) {
				return ErrInvalid
			}
		}
		folded := strings.ToLower(name)
		if seen[folded] {
			return ErrInvalid
		}
		seen[folded] = true
		switch folded {
		case "host", "connection", "content-length", "transfer-encoding", "upgrade", "range", "accept-encoding", "proxy-authorization":
			return ErrInvalid
		}
		total += len(name) + len(value)
	}
	if total > 16*1024 {
		return ErrInvalid
	}
	return nil
}

// HTTPConnector reads a bounded prefix through Go's HTTP transport and probes
// that private file with the existing restricted local-file prober. Probe
// bytes are replayed before the remaining response bytes, without reopening
// the upstream or giving FFmpeg a network protocol or secret URL.
type HTTPConnector struct {
	client  *http.Client
	prober  media.Prober
	tempDir string
	probe   func(context.Context, *os.File) (media.Info, error)
}

func NewHTTPConnector(prober media.Prober, tempDir string) *HTTPConnector {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = 8 * time.Second
	transport.MaxResponseHeaderBytes = 64 * 1024
	transport.MaxIdleConnsPerHost = 4
	transport.DisableCompression = true
	prober.AnalyzeVideoSeek = false
	if prober.Timeout <= 0 || prober.Timeout > 10*time.Second {
		prober.Timeout = 10 * time.Second
	}
	connector := &HTTPConnector{client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		prober: prober, tempDir: tempDir}
	connector.probe = prober.ProbeStreamPrefix
	return connector
}

func (connector *HTTPConnector) Close() error { connector.client.CloseIdleConnections(); return nil }

func (connector *HTTPConnector) Open(ctx context.Context, definition Definition) (*Connection, error) {
	if err := validateDefinition(definition); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, definition.URL, nil)
	if err != nil {
		return nil, ErrInvalid
	}
	for name, value := range definition.Headers {
		request.Header.Set(name, value)
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := connector.client.Do(request)
	if err != nil {
		return nil, ErrUnavailable
	}
	owned := false
	defer func() {
		if !owned {
			_ = response.Body.Close()
		}
	}()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		return nil, ErrUnavailable
	}
	reader := &idleReader{body: response.Body, timeout: 15 * time.Second}
	prefix := make([]byte, probeSampleBytes)
	n, readErr := io.ReadFull(reader, prefix)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) || n == 0 {
		return nil, ErrUnavailable
	}
	prefix = prefix[:n]
	file, err := os.CreateTemp(connector.tempDir, "goby-source-probe-*")
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	if _, err := file.Write(prefix); err != nil {
		return nil, ErrUnavailable
	}
	info, err := connector.probe(ctx, file)
	if err != nil || len(info.Streams) == 0 {
		return nil, ErrUnavailable
	}
	// A partial prefix has no trustworthy duration, size, sample scan, or seek
	// index for the complete input. Unknown values stay unknown during planning.
	if readErr == nil || definition.Infinite {
		info.DurationTicks, info.Size = 0, 0
		info.AudioDurationExact, info.AudioDurationReason = false, "streaming_input"
		info.Chapters = nil
		for index := range info.Streams {
			info.Streams[index].AudioTiming = nil
		}
	}
	info.FileChangeTimeNs = 0
	info.VideoSeekIndexes = nil
	owned = true
	return &Connection{Reader: &prefixReader{Reader: io.MultiReader(bytes.NewReader(prefix), reader), body: reader}, Info: info}, nil
}

type prefixReader struct {
	io.Reader
	body io.Closer
}

func (reader *prefixReader) Close() error { return reader.body.Close() }

// An upstream that stops yielding bytes cannot retain a reader indefinitely.
// Closing the transport interrupts an in-flight HTTP body Read.
type idleReader struct {
	body    io.ReadCloser
	timeout time.Duration
	once    sync.Once
}

func (reader *idleReader) Read(data []byte) (int, error) {
	timer := time.AfterFunc(reader.timeout, func() { _ = reader.Close() })
	n, err := reader.body.Read(data)
	timer.Stop()
	return n, err
}
func (reader *idleReader) Close() error {
	var err error
	reader.once.Do(func() { err = reader.body.Close() })
	return err
}
