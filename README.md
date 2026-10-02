# AI_file

ai_file is a Mythic C2 profile that uses the documented OpenAI Files API workflow to exchange implant messages through .jsonl files instead of a traditional HTTP/S listener. It relies on the Files API behavior defined by OpenAI in the official documentation: https://developers.openai.com/api/docs/guides/file-inputs?api-mode=responses.


The listener polls `/v1/files` for `.jsonl` files with `purpose=batch`, decrypts each request envelope locally, forwards the raw Mythic message bytes to Mythic's `/agent_message`, encrypts Mythic's response, and uploads a response `.jsonl` file back to the Files API.

<img width="2255" height="937" alt="image" src="https://github.com/user-attachments/assets/285e8889-1738-4cc4-ba07-c8b618131d3a" />



<img width="1010" height="767" alt="image" src="https://github.com/user-attachments/assets/bd564bbc-fa4a-451f-987f-931410fd97e3" />




<img width="835" height="155" alt="image" src="https://github.com/user-attachments/assets/51a1b8d9-8696-44ff-a604-bff31a78800d" />




## Listener Configuration

Edit `C2_Profiles/openai_file/openai_file/c2_code/config.json` before starting the profile:

```json
{
  "instances": [
    {
      "name": "default",
      "api_key": "REPLACE_ME",
      "base_url": "https://api.openai.com/v1",
      "organization": "",
      "project": "",
      "purpose": "batch",
      "channel_id": "mythic",
      "request_prefix": "mythic_to_server",
      "response_prefix": "mythic_to_agent",
      "transport_key": "REPLACE_ME",
      "poll_interval_seconds": 5,
      "delete_processed_files": true,
      "debug": true,
      "max_file_bytes": 10485760,
      "mythic_host": "",
      "mythic_port": 0
    }
  ]
}
```

Required values are `api_key`, `transport_key`, and a matching `channel_id` between the listener and payload profile config. `transport_key` can be `base64:<32 raw bytes>` or a high-entropy passphrase; passphrases are SHA-256 derived before envelope encryption use.

## Install

From the Mythic directory:

```bash
sudo ./mythic-cli install folder /path/to/openai_file
sudo ./mythic-cli c2 start openai_file
```

Protocol details for payload integration are in `documentation-c2/openai_file/_index.md`.
