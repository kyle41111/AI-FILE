# openai_file

## Transport

Agents and the listener exchange OpenAI Files API `.jsonl` files using `purpose=batch`.

Request filename:

```text
<request_prefix>_<channel_id>_<request_id>.jsonl
```

Response filename:

```text
<response_prefix>_<channel_id>_<request_id>.jsonl
```

Default request prefix is `mythic_to_server`; default response prefix is `mythic_to_agent`; default channel is `mythic`.

## JSONL Envelope

Each file contains one or more JSON objects, one per line:

```json
{"v":1,"profile":"openai_file","channel":"mythic","direction":"request","id":"req_001","alg":"aes-256-cbc-hmac-sha256+base64url","nonce":"...","ciphertext":"...","created_at":0}
```

`ciphertext` is AES-256-CBC output with a trailing HMAC-SHA256 tag over the raw Mythic message bytes. `nonce` is the CBC IV. `nonce` and `ciphertext` are unpadded base64url. AAD is:

```text
v|profile|channel|direction|id|alg
```

For example:

```text
1|openai_file|mythic|request|req_001|aes-256-cbc-hmac-sha256+base64url
```

The listener also accepts legacy `aes-256-gcm+base64url` envelopes and replies with the same envelope algorithm used by the request.

## API Calls

Upload request:

```bash
curl -sS https://api.openai.com/v1/files \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -F purpose=batch \
  -F file=@mythic_to_server_mythic_req_001.jsonl
```

List response candidates:

```bash
curl -sS "https://api.openai.com/v1/files?purpose=batch&order=asc" \
  -H "Authorization: Bearer $OPENAI_API_KEY"
```

Download file content:

```bash
curl -sS https://api.openai.com/v1/files/file-abc123/content \
  -H "Authorization: Bearer $OPENAI_API_KEY"
```

Delete processed files:

```bash
curl -sS -X DELETE https://api.openai.com/v1/files/file-abc123 \
  -H "Authorization: Bearer $OPENAI_API_KEY"
```
