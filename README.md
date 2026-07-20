<p align="center">
  <img src=".github/img/Discursive.png" alt="Discursive" width="600" />
</p>

A local OpenAI-compatible gateway that lets [Cursor](https://cursor.com) use
[Moonshot Kimi](https://platform.kimi.ai/) and [DeepSeek](https://api-docs.deepseek.com/)
on macOS and Linux — with full agentic and tool calling support.

--- 

### Table of Contents <!-- omit in toc -->

- [📦 Quickstart](#-quickstart)
- [☁️ Setting up Cloudflare](#️-setting-up-cloudflare)
- [🛠 Tech Stack](#-tech-stack)
- [📁 File Structure](#-file-structure)
- [🧠 Supported Models \& Mappings](#-supported-models--mappings)
- [🖥 CLI Commands](#-cli-commands)
- [🌍 Environment Variables](#-environment-variables)
- [🔒 Security](#-security)
- [🧪 Methodology](#-methodology)
- [📜 License](#-license)


---

## 📦 Quickstart

### 1. Install <!-- omit in toc -->

```bash
go install github.com/commoddity/discursive@latest
```

Or download a [release binary](https://github.com/commoddity/discursive/releases) and put it on your `PATH`.

### Dependencies <!-- omit in toc -->

- [Go](https://go.dev/dl/) 1.26.5+
- [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)

### 2. Start the gateway <!-- omit in toc -->

```bash
discursive start --background
```

On first run, the gateway auto-invokes an interactive wizard that prompts for:

- **Moonshot/Kimi API key** — get one at [platform.kimi.ai](https://platform.kimi.ai/)
- **DeepSeek API key** — get one at [platform.deepseek.com](https://platform.deepseek.com/)
- **Cloudflare tunnel token** — see [Setting up Cloudflare](#-setting-up-cloudflare) below
- **Public HTTPS URL** — the hostname you configured in the tunnel setup with `/v1` appended

Keys are encrypted at rest. Secrets are never sent to Cursor or logged.

The gateway listens on `127.0.0.1:4001`. It logs the `gateway_key` and
`public_url` you'll need for the next step:

```bash
discursive status | jq
```

### 3. Configure Cursor <!-- omit in toc -->

Open **Cursor Settings → Models** and enter:

| Setting                  | Value                                                        |
| ------------------------ | ------------------------------------------------------------ |
| OpenAI API Key           | `gateway_key` from `discursive status` output                |
| Override OpenAI Base URL | `public_url` from `discursive status` output (ends in `/v1`) |
| Model                    | Pick an alias from the table below (e.g. `gpt-5-high`)       |

Reload Cursor: **Cmd+Shift+P → Reload Window**. You should see
`Connection verified` above the Base URL field.

### 4. Switch providers <!-- omit in toc -->

Change the model alias in Cursor's model picker — no restart needed:

| Cursor alias  | Provider | Real model          | Use                 |
| ------------- | -------- | ------------------- | ------------------- |
| `gpt-5-high`  | Moonshot | `kimi-k3`           | Planning / flagship |
| `gpt-5-codex` | Moonshot | `kimi-k2.7-code`    | Coding              |
| `gpt-4o`      | DeepSeek | `deepseek-v4-pro`   | Harder execution    |
| `gpt-4o-mini` | DeepSeek | `deepseek-v4-flash` | Cheap execution     |

### 5. Switch back to Cursor's models <!-- omit in toc -->

In Cursor Settings → Models: turn off "Override OpenAI API Key" and
"Override OpenAI Base URL", then pick a Cursor-native model.

---

## ☁️ Setting up Cloudflare

Cursor's cloud cannot reach `localhost`. A Cloudflare tunnel gives the gateway
a public HTTPS URL.

1. Go to [Cloudflare Zero Trust → Tunnels](https://one.dash.cloudflare.com/)
2. Click **Add a tunnel**, choose **Cloudflared**, give it a name
3. Copy the tunnel token — you'll paste it into the Discursive wizard
4. Under **Public Hostname**, add a route:
   - **Subdomain**: anything you like (e.g. `discursive`)
   - **Domain**: choose from your Cloudflare zones
   - **Service**: `http://127.0.0.1:4001`
5. The public URL you'll enter in the wizard is the hostname from step 4
   with `/v1` appended (e.g. `https://discursive.yourdomain.com/v1`)

---

## 🛠 Tech Stack

| Component     | Technology                                                                                                                 |
| ------------- | -------------------------------------------------------------------------------------------------------------------------- |
| Language      | Go 1.26.5+                                                                                                                 |
| CLI framework | [Cobra](https://cobra.dev/)                                                                                                |  |
| Tunnel        | [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/) Quick Tunnel or named tunnel |
| Upstream APIs | OpenAI-compatible chat completions (Moonshot + DeepSeek)                                                                   |

---

## 📁 File Structure

```
main.go                   # Entry point
internal/
  cli/                    # Cobra command tree (start, stop, doctor, …)
    wizard/               # Interactive init wizard
  config/                 # App settings, paths, upstream URL helpers
  crypto/                 # Encrypt upstream keys + gateway key gen
  gateway/                # HTTP server, sanitizer, optimizer, proxy, auth
  tunnel/                 # cloudflared supervisor
  doctor/                 # Health checks
  usage/                  # Pricing tables, token/cost store, slog helpers
.cursor/rules/            # Agent conventions
.claude/skills/           # Invocable workflows
planning/phases/          # MVP task sequence (T01–T10)
```

---

## 🧠 Supported Models & Mappings

Switching providers is choosing the Cursor alias. The gateway maps it and
picks the right upstream key + base URL. 

| Cursor alias  | Provider | Real model          | Notes                                    |
| ------------- | -------- | ------------------- | ---------------------------------------- |
| `gpt-5-high`  | Moonshot | `kimi-k3`           | Flagship planning model; supports vision |
| `gpt-5-codex` | Moonshot | `kimi-k2.7-code`    | Code-optimized                           |
| `gpt-4o`      | DeepSeek | `deepseek-v4-pro`   | Harder execution                         |
| `gpt-4o-mini` | DeepSeek | `deepseek-v4-flash` | Cheap, fast execution                    |

Provider choice is the alias — Cursor always talks to Discursive, never to
Moonshot/DeepSeek directly.

---

## 🖥 CLI Commands

All output is JSON on stdout. Pipe through `jq` for readability.

| Command                                           | Description                                                                                                                                                                 |
| ------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `discursive start`                                | Start gateway on `127.0.0.1:4001`. `--background` forks to daemon. `--tunnel` (named/none/quick), `--public-url`. Auto-invokes `init` if config is incomplete on first run. |
| `discursive stop`                                 | Send SIGTERM via PID file. No-op if not running.                                                                                                                            |
| `discursive status`                               | Config dump + runtime state: PID alive? uptime? log file path/size, tunnel mode, model mapping.                                                                             |
| `discursive logs`                                 | Pretty-print `gateway.log` with colored level prefixes. `--follow` (`-f`) for live tail. `-n N` for last N lines.                                                           |
| `discursive log-level [debug\|info\|warn\|error]` | Show or set log verbosity. Set persists per-process; hints how to export `DISCURSIVE_LOG_LEVEL` for persistence.                                                            |
| `discursive doctor`                               | Health checks: keys present, port available, local/public HTTP health, tunnel mode, cloudflared binary, logs writable.                                                      |
| `discursive usage`                                | Token + cost estimates per session/model.                                                                                                                                   |
| `discursive set-moonshot-key`                     | Save Moonshot/Kimi API key (encrypted at rest).                                                                                                                             |
| `discursive set-deepseek-key`                     | Save DeepSeek API key (encrypted at rest).                                                                                                                                  |
| `discursive set-tunnel-token`                     | Save Cloudflare tunnel token.                                                                                                                                               |
| `discursive set-public-url`                       | Save public HTTPS base URL (`https://<host>/v1`).                                                                                                                           |
| `discursive set-model`                            | Persist preferred Cursor alias (`gpt-5-high`, `gpt-4o-mini`, etc.).                                                                                                         |
| `discursive rotate-gateway-key`                   | Generate a new gateway API key.                                                                                                                                             |
| `discursive version`                              | Print version.                                                                                                                                                              |

JSON slog on **stdout**, interactive prompts on **stderr** — pipe-friendly.

---

## 🌍 Environment Variables

| Variable                       | Purpose                                                   | Default                      |
| ------------------------------ | --------------------------------------------------------- | ---------------------------- |
| `DISCURSIVE_LOG_LEVEL`         | Log verbosity: `debug`, `info`, `warn`, `error`           | `info`                       |
| `DISCURSIVE_USAGE_IDLE`        | Idle window before emitting a usage summary (Go duration) | `30s`                        |
| `DISCURSIVE_MOONSHOT_BASE_URL` | Override Moonshot API root                                | `https://api.moonshot.ai/v1` |
| `DISCURSIVE_DEEPSEEK_BASE_URL` | Override DeepSeek API root                                | `https://api.deepseek.com`   |

---

## 🔒 Security

- Upstream Moonshot and DeepSeek keys are **encrypted at rest** and never sent
  to Cursor, never appear in logs
- Cursor receives only the generated gateway key (`sk-...`)
- Gateway binds to loopback (`127.0.0.1`); the Cloudflare tunnel is the only
  public surface
- All output is JSON on stdout — never emit secrets or raw headers

---

## 🧪 Methodology

<div align="center">
  <a href="https://github.com/commoddity/turboplan">
    <img src=".github/img/turboplan.png" alt="Turboplan" width="400" />
  </a>
</div>

---

## 📜 License

MIT
