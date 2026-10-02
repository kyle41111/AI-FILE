package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type openAIFileClient struct {
	baseURL      string
	apiKey       string
	organization string
	project      string
	httpClient   *http.Client
}

type openAIFile struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Bytes     int64  `json:"bytes"`
	CreatedAt int64  `json:"created_at"`
	Filename  string `json:"filename"`
	Purpose   string `json:"purpose"`
}

type listFilesResponse struct {
	Object  string       `json:"object"`
	Data    []openAIFile `json:"data"`
	FirstID string       `json:"first_id"`
	LastID  string       `json:"last_id"`
	HasMore bool         `json:"has_more"`
}

func newOpenAIFileClient(cfg instanceConfig) *openAIFileClient {
	return &openAIFileClient{
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:       cfg.APIKey,
		organization: cfg.Organization,
		project:      cfg.Project,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *openAIFileClient) listFiles(ctx context.Context, purpose string) ([]openAIFile, error) {
	var out []openAIFile
	after := ""
	for {
		values := url.Values{}
		values.Set("limit", "100")
		values.Set("order", "asc")
		if purpose != "" {
			values.Set("purpose", purpose)
		}
		if after != "" {
			values.Set("after", after)
		}
		endpoint := c.endpoint("/files") + "?" + values.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		c.addHeaders(req)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := readAPIResponse(resp, 1<<20)
		if err != nil {
			return nil, err
		}
		var page listFilesResponse
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Data...)
		if !page.HasMore || page.LastID == "" {
			return out, nil
		}
		after = page.LastID
	}
}

func (c *openAIFileClient) downloadFile(ctx context.Context, fileID string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/files/"+url.PathEscape(fileID)+"/content"), nil)
	if err != nil {
		return nil, err
	}
	c.addHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	return readAPIResponse(resp, maxBytes)
}

func (c *openAIFileClient) uploadFile(ctx context.Context, filename string, purpose string, content []byte) (*openAIFile, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("purpose", purpose); err != nil {
		return nil, err
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(content); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/files"), &body)
	if err != nil {
		return nil, err
	}
	c.addHeaders(req)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	responseBody, err := readAPIResponse(resp, 1<<20)
	if err != nil {
		return nil, err
	}
	var file openAIFile
	if err := json.Unmarshal(responseBody, &file); err != nil {
		return nil, err
	}
	return &file, nil
}

func (c *openAIFileClient) deleteFile(ctx context.Context, fileID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint("/files/"+url.PathEscape(fileID)), nil)
	if err != nil {
		return err
	}
	c.addHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	_, err = readAPIResponse(resp, 1<<20)
	return err
}

func (c *openAIFileClient) endpoint(apiPath string) string {
	return c.baseURL + path.Clean("/"+apiPath)
}

func (c *openAIFileClient) addHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if c.organization != "" {
		req.Header.Set("OpenAI-Organization", c.organization)
	}
	if c.project != "" {
		req.Header.Set("OpenAI-Project", c.project)
	}
}

func readAPIResponse(resp *http.Response, maxBytes int64) ([]byte, error) {
	return readHTTPResponse(resp, maxBytes, "OpenAI API")
}

func readHTTPResponse(resp *http.Response, maxBytes int64, label string) ([]byte, error) {
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("response exceeds %d bytes", maxBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s status %d: %s", label, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}
