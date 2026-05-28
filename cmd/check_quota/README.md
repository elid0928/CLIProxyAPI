# Codex Quota Checker

Small standalone Codex quota checker for CLIProxyAPI auth JSON files.

It reads a single auth JSON file or a directory of `*.json` files, calls:

```text
https://chatgpt.com/backend-api/wham/usage
```

and prints the Codex 5-hour and weekly quota windows.

## Usage

```bash
go run ./cmd/check_quota auths/1-codex.json
go run ./cmd/check_quota auths
go run ./cmd/check_quota -json auths
```

Useful options:

```bash
# Include disabled auth files.
go run ./cmd/check_quota -include-disabled auths

# Refresh expired access tokens before querying.
go run ./cmd/check_quota -refresh-expired auths

# Refresh expired tokens and write rotated tokens back to the auth JSON file.
go run ./cmd/check_quota -write-refreshed auths/1-codex.json
```

## Auth File

The expected format matches CLIProxyAPI Codex auth files, for example `auths/1-codex.json`.
Required fields are:

```json
{
  "type": "codex",
  "access_token": "...",
  "account_id": "..."
}
```

If `account_id` is missing, the checker also accepts `chatgpt_account_id` or extracts it from `id_token`.
