package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/MythicMeta/MythicContainer/logging"
)

type config struct {
	Instances []instanceConfig `json:"instances"`
}

type instanceConfig struct {
	Name                 string `json:"name"`
	APIKey               string `json:"api_key"`
	BaseURL              string `json:"base_url"`
	Organization         string `json:"organization"`
	Project              string `json:"project"`
	Purpose              string `json:"purpose"`
	ChannelID            string `json:"channel_id"`
	RequestPrefix        string `json:"request_prefix"`
	ResponsePrefix       string `json:"response_prefix"`
	TransportKey         string `json:"transport_key"`
	PollIntervalSeconds  int    `json:"poll_interval_seconds"`
	DeleteProcessedFiles bool   `json:"delete_processed_files"`
	Debug                bool   `json:"debug"`
	MaxFileBytes         int64  `json:"max_file_bytes"`
	MythicHost           string `json:"mythic_host"`
	MythicPort           int    `json:"mythic_port"`
}

var Config = config{}

func InitializeLocalConfig() {
	configPath := filepath.Join(getCwdFromExe(), "config.json")
	if !fileExists(configPath) {
		if _, err := os.Create(configPath); err != nil {
			logging.LogFatalError(err, "config.json doesn't exist and couldn't be created")
		}
	}
	fileData, err := os.ReadFile(configPath)
	if err != nil {
		logging.LogFatalError(err, "failed to read config.json")
	}
	if err := json.Unmarshal(fileData, &Config); err != nil {
		logging.LogFatalError(err, "failed to unmarshal config bytes")
	}
	logging.LogInfo("successfully read config.json", "path", configPath)
}

func (c *instanceConfig) applyDefaults(index int) error {
	if c.Name == "" {
		c.Name = fmt.Sprintf("instance-%d", index)
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	if c.BaseURL == "" {
		c.BaseURL = "https://api.openai.com/v1"
	}
	if c.Purpose == "" {
		c.Purpose = "batch"
	}
	if c.ChannelID == "" {
		c.ChannelID = "mythic"
	}
	if c.RequestPrefix == "" {
		c.RequestPrefix = "mythic_to_server"
	}
	if c.ResponsePrefix == "" {
		c.ResponsePrefix = "mythic_to_agent"
	}
	if c.PollIntervalSeconds <= 0 {
		c.PollIntervalSeconds = 5
	}
	if c.MaxFileBytes <= 0 {
		c.MaxFileBytes = 10 << 20
	}
	if c.APIKey == "" || c.APIKey == "REPLACE_ME" {
		return errors.New("api_key is required")
	}
	if c.TransportKey == "" || c.TransportKey == "REPLACE_ME" {
		return errors.New("transport_key is required")
	}
	if !strings.HasSuffix(c.RequestPrefix, "_") {
		c.RequestPrefix += "_"
	}
	if !strings.HasSuffix(c.ResponsePrefix, "_") {
		c.ResponsePrefix += "_"
	}
	return nil
}

func getCwdFromExe() string {
	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("failed to get path to current executable: %v", err)
	}
	return filepath.Dir(exe)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
