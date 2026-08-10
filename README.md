<p align="center">
  <img src=".github/img/Discursive.png" alt="Discursive" width="600" />
</p>

<div align="center">
  <blockquote>
    <em>/dĭ-skûr′sĭv/ - proceeding coherently from topic to topic; marked by analytical reasoning</em>
  </blockquote>
  <p>A gateway proxy that enables <a href="https://cursor.com">Cursor</a>'s full agentic workflow with alternative providers.</p>
</div>

<p align="center">
  <a href="https://platform.kimi.ai/"><img src=".github/img/moonshot-white.svg" alt="Moonshot Kimi" height="35" valign="middle" /></a>
  &ensp;&middot;&ensp;
  <a href="https://api-docs.deepseek.com/"><img src=".github/img/deepseek.svg" alt="DeepSeek" height="35" valign="middle" /></a>
  &ensp;&middot;&ensp;
  <a href="https://thaura.ai/"><img src=".github/img/thaura.png" alt="Thaura AI" height="35" valign="middle" /></a>
  &ensp;&middot;&ensp;
  <a href="https://docs.z.ai/"><img src=".github/img/zai.svg" alt="Z.AI" height="35" valign="middle" /></a>
</p>

<h3 align="center">Written in <a href="https://go.dev/"><img src=".github/img/go.svg" alt="Go" height="28" valign="middle" /></a></h3>

---

### Table of Contents <!-- omit in toc -->

- [📦 Quickstart](#-quickstart)
- [⚡ Subagent Routing](#-subagent-routing)
  - [What gets downgraded](#what-gets-downgraded)
  - [`discursive start` flags](#discursive-start-flags)
  - [Compression](#compression)
- [☁️ Setting up Cloudflare](#️-setting-up-cloudflare)
- [📊 Usage Dashboard](#-usage-dashboard)
- [🪐 Providers](#-providers)
- [🛠 Tech Stack](#-tech-stack)
- [📁 File Structure](#-file-structure)
- [🖥 CLI Commands](#-cli-commands)
- [⌨️ Shell Completion](#️-shell-completion)
- [🌍 Environment Variables](#-environment-variables)
- [🔄 CI / Release](#-ci--release)
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

### Prerequisites <!-- omit in toc -->

#### Dependencies <!-- omit in toc -->

- [Go](https://go.dev/dl/) 1.26.5+
- [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)

On first run, the interactive wizard also prompts for:

| Item                    | Required | Where to get / notes                                       |
| ----------------------- | -------- | ---------------------------------------------------------- |
| Moonshot (Kimi) API key | ✅ Yes    | [platform.kimi.ai](https://platform.kimi.ai/)              |
| DeepSeek API key        | ✅ Yes    | [platform.deepseek.com](https://platform.deepseek.com/)    |
| Cloudflare tunnel token | ✅ Yes    | See [Setting up Cloudflare](#-setting-up-cloudflare) below |
| Public HTTPS URL        | ✅ Yes    | Hostname from tunnel setup with `/v1` appended             |
| Thaura AI API key       | No       | [thaura.ai](https://thaura.ai/api-platform)                |
| Z.AI API key            | No       | [docs.z.ai](https://docs.z.ai/api-reference/introduction)  |

### 2. Start the gateway <!-- omit in toc -->

```bash
discursive start --background
```

On first run, the gateway auto-invokes the interactive wizard (see [Prerequisites](#prerequisites)).

Keys are encrypted at rest. Secrets are never sent to Cursor or logged.

The gateway listens on `localhost:4001`. It logs the `gateway_key` and
`public_url` you'll need for the next step:

```bash
discursive status --show-key | jq
```

Gateway keys are masked by default. Pass `--show-key` to print the full
`gateway_key` for Cursor setup.

> **💡 Subagent routing is on by default.** The gateway inspects
> every request and, when the content indicates simple, cheap work (short lookups,
> code search, structured extraction, automation), routes it to a cheaper
> model — typically `deepseek-v4-flash` — to cut cost. Complex work
> (editing/refactoring, reasoning) keeps the original model.
> See [Subagent Routing](#-subagent-routing) below, or disable it with
> `discursive start --subagent-router=false`.

### 3. Configure Cursor <!-- omit in toc -->

Open **Cursor Settings → Models** and enter:

| Setting                  | Value                                                 |
| ------------------------ | ----------------------------------------------------- |
| OpenAI API Key           | `gateway_key` from `discursive status --show-key`     |
| Override OpenAI Base URL | `public_url` from `discursive status` (ends in `/v1`) |
| Model                    | Pick an alias from the table below (e.g. `gpt-4o`)    |

Reload Cursor: **Cmd+Shift+P → Reload Window**. You should see
`Connection verified` above the Base URL field.

> **💡 Tip:** You can also copy the Gateway Key and Tunnel URL directly from
> the [Usage Dashboard at http://localhost:4002](http://localhost:4002) —
> hover over the `?` icons next to ☁️ Tunnel and 🔐 Gateway Key for field-specific
> setup instructions.

### 4. Switch providers <!-- omit in toc -->

Change the model alias in Cursor's model picker — no restart needed:


| Cursor alias    | Provider | Real model          | Use                                                       |
| --------------- | -------- | ------------------- | --------------------------------------------------------- |
| `gpt-4o`        | Moonshot | `kimi-k3`           | Planning / flagship                                       |
| `gpt-4o-mini`   | Moonshot | `kimi-k2.7-code`    | Coding; always thinks                                     |
| `o1`            | DeepSeek | `deepseek-v4-pro`   | Harder execution                                          |
| `o3-mini`       | DeepSeek | `deepseek-v4-flash` | Cheap execution                                           |
| `gpt-5-nano`    | Thaura   | `thaura`            | Ethical AI; optional provider                             |
| `gpt-4.1-turbo` | Z.AI     | `glm-5.2`           | Planning; cheaper than K3                                 |
| `gpt-4.1`       | Z.AI     | `glm-4.7`           | Cheap execution                                           |
| `gpt-4-turbo`   | Z.AI     | `glm-5.2`           | Compat alias (Cursor may rewrite `gpt-4.1-turbo` to this) |




### 5. Switch back to Cursor's models <!-- omit in toc -->

In Cursor Settings → Models: turn off "Override OpenAI API Key" and
"Override OpenAI Base URL", then pick a Cursor-native model.

---

## ⚡ Subagent Routing

The gateway can **automatically downgrade individual requests** to a cheaper,
faster model when the work is simple enough — cutting token cost and latency
without changing what you pick in Cursor. Subagent routing is **on by default**
and requires no configuration.

> The router runs entirely **inside the gateway**. Cursor still sends every
> request to the gateway under whatever model alias you chose; the gateway
> inspects each request, may route it to a cheaper model, and proxies upstream.
> Cursor's model picker is unaware of the routing.

### What gets downgraded

Each incoming request is classified by its **content** — the last user message
determines whether the task is cheap enough for a flash model:

| Request type                                          | Action             | Model               |
| ----------------------------------------------------- | ------------------ | ------------------- |
| Simple lookup / explanation                           | downgrade to flash | `deepseek-v4-flash` |
| Code search / exploration                             | downgrade to flash | `deepseek-v4-flash` |
| Structured extraction (`json_object` / `json_schema`) | downgrade to flash | `deepseek-v4-flash` |
| Automation / mechanical work (lint, git, scripts, PR) | downgrade to flash | `deepseek-v4-flash` |
| Editing / refactoring                                 | keep model         | original model      |
| Complex reasoning / architecture                      | keep model         | original model      |
| Unknown / unclassified                                | keep model         | original model      |

### `discursive start` flags

| Flag                | Default  | Purpose                                                                                                                                                                               |
| ------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--subagent-router` | `true`   | Enable the subagent router (content-based classification + flash downgrade). Set `--subagent-router=false` to run the gateway with no automatic model changes.                        |
| `--log-level`       | `info`   | Log verbosity: `debug`, `info`, `warn`, `error`. Use `debug` to see per-request `request_class` and override lines from the router. Overrides `DISCURSIVE_LOG_LEVEL`.                 |
| `--background`      | `false`  | Detach and run in the background. Logs to `{dataRoot}/gateway.log`.                                                                                                                   |
| `--tunnel`          | (config) | Tunnel mode: `named`, `none`, or `quick` (persists to config).                                                                                                                        |
| `--public-url`      | (config) | Public HTTPS base URL ending in `/v1` (persists to config).                                                                                                                           |
| `--compress`        | `false`  | Compress verbose tool results to reduce token cost. Uses a cheap summarizer model (flash). Fail-open: on any error, original content is sent unchanged. |
| `--verbosity`       | `false`  | Enforce terse output on flash turns: injects a terseness directive, caps `max_tokens`, and trims trailing prose. Ignored unless the served model is `deepseek-v4-flash`. |

Examples:

```bash
# Routing on (default) + debug logging
discursive start --subagent-router --log-level debug

# Disable routing entirely
discursive start --subagent-router=false

# Enable compression for cost savings
discursive start --compress --log-level debug
```

> **💡 Tip:** At `--log-level debug`, the router logs one line per request with
> `request_class`. This is the easiest way to see exactly what the router is
> doing and tune your expectations.

### Compression

The `--compress` flag enables context compression to reduce token cost during
multi-turn agent sessions:

1. **Tool-result compression**: Tool output exceeding a character threshold is
   summarized by a cheap model (`deepseek-v4-flash`).

Compression is **fail-open**: if the summarizer model returns an empty or error
result, the original content is sent upstream unchanged — there is no quality
loss. Results are cached by content hash with singleflight deduplication, so
repeated tool results (e.g. `ls` output, test output) are compressed only once.

**When to use:** Multi-turn agent sessions with verbose tools (file reads, test
runs, search results). In testing, compression saved ~42% of input tokens in a
~34-minute EPUB pipeline session with no observable quality degradation.

**Cost:** The summarizer model uses `deepseek-v4-flash` pricing (nearly free per
turn with prompt caching). The savings from reduced upstream tokens far outweigh
the compression cost.

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
  - **Service**: `http://localhost:4001`
5. The public URL you'll enter in the wizard is the hostname from step 4
  with `/v1` appended (e.g. `https://discursive.yourdomain.com/v1`)

---



## 📊 Usage Dashboard

<div align="center">
  <img src=".github/img/usage-dashboard.png" alt="Usage Dashboard" width="500">
</div>
<p align="center">
  <em>Usage Dashboard</em>
</p>

The gateway serves a local usage dashboard at `http://localhost:4002`
(loopback only). It starts automatically with `discursive start` — no extra
process or configuration.

- **System health** - health checks & system uptime
- **Reasoning effort** — per-model `low` / `high` / `max` (and DeepSeek `off`) saved to app settings
- **Provider balances & monthly spend projection** — average daily spend, projected monthly total
- **Month to date spending** — requests, tokens, and estimated cost (USD, EUR, CNY)
- **Spend by period, model, and provider** — clear charts per time period, model, and provider
- **Sessions** — summary stats for the selected range; expand to browse individual sessions

> **💡 Note:** The Usage Dashboard is not exposed via the public tunnel. Only accessible locally on `localhost:4002`.

---

## 🪐 Providers

Models that support configurable reasoning / thinking (`kimi-k3`,
`deepseek-v4-pro`, `deepseek-v4-flash`) can be tuned from the Usage Dashboard
(**Reasoning Effort** card at `http://127.0.0.1:4002`). Values are stored in app
settings and applied to new gateway requests immediately (no restart). Gateway
logs include an `effort` field on request/response/usage lines.

| Model                                   | Options              | Default                                                                                  |
| --------------------------------------- | -------------------- | ---------------------------------------------------------------------------------------- |
| `kimi-k3`                               | `low`, `high`, `max` | `low` (API default is `max`; we default lower for cost)                                  |
| `deepseek-v4-pro` / `deepseek-v4-flash` | `off`, `high`, `max` | `off` (`off` → `thinking: disabled`; otherwise `thinking: enabled` + `reasoning_effort`) |
| `glm-5.2`                               | `off`, `high`, `max` | `off` (`off` → `thinking: disabled`; otherwise `thinking: enabled` + `reasoning_effort`) |

- Lower effort usually means fewer thinking tokens and lower cost. `thaura` does not
expose this control.
- `kimi-k2.7-code` always thinks — thinking is always on and there is no effort
selector: [Kimi K2.7 Code](https://www.kimi.com/resources/kimi-k2-7-code)
- DeepSeek only documents `high`/`max` for effort: [DeepSeek Thinking Mode](https://api-docs.deepseek.com/guides/thinking_mode)).

### 🌙 Moonshot (Kimi) <!-- omit in toc -->

[Moonshot](https://platform.kimi.ai/) provides frontier models with long-context
windows and native reasoning capabilities.


| API model ID     | Cache hit / MTok | Input / MTok | Output / MTok | Role                                      |
| ---------------- | ---------------- | ------------ | ------------- | ----------------------------------------- |
| `kimi-k3`        | $0.30            | $3.00        | $15.00        | Flagship; 1M-token context, always thinks |
| `kimi-k2.7-code` | $0.19            | $0.95        | $4.00         | Coding model; always thinks               |


- Pricing: [https://platform.kimi.ai/docs/pricing/chat](https://platform.kimi.ai/docs/pricing/chat)
- API docs: [https://platform.kimi.ai/docs/](https://platform.kimi.ai/docs/)
- K3 reasoning effort: [Reasoning Effort](https://platform.kimi.ai/docs/guide/use-reasoning-effort)

---



### 🐋 DeepSeek <!-- omit in toc -->

[DeepSeek](https://api-docs.deepseek.com/) provides cost-efficient reasoning
models at a fraction of the cost per token.


| API model ID        | Cache hit / MTok | Cache miss / MTok | Output / MTok | Role                                 |
| ------------------- | ---------------- | ----------------- | ------------- | ------------------------------------ |
| `deepseek-v4-pro`   | $0.003625        | $0.435            | $0.87         | Harder reasoning / agentic execution |
| `deepseek-v4-flash` | $0.0028          | $0.14             | $0.28         | Cheap, high-volume execution         |


- Pricing: [https://api-docs.deepseek.com/quick_start/pricing](https://api-docs.deepseek.com/quick_start/pricing)
- API docs: [https://api-docs.deepseek.com/](https://api-docs.deepseek.com/)
- Thinking mode: [Thinking Mode](https://api-docs.deepseek.com/guides/thinking_mode)

---

### 🪻 Z.AI <!-- omit in toc -->

[Z.AI](https://docs.z.ai/) provides GLM-series models with
thinking support and prompt caching. Z.AI is used via the **GLM Coding Plan**
(subscription, credits quota), which exposes the OpenAI-compatible base URL
`https://api.z.ai/api/coding/paas/v4`.

| API model ID | Cache hit / MTok | Input / MTok | Output / MTok | Role                                                                     |
| ------------ | ---------------- | ------------ | ------------- | ------------------------------------------------------------------------ |
| `glm-5.2`    | $0.26            | $1.40        | $4.40         | Planning model; reasoning_effort + cache                                 |
| `glm-4.7`    | $0.11            | $0.60        | $2.20         | Budget execution; thinking on/off                                        |
| `glm-4.6v`   | $0.05            | $0.30        | $0.90         | Vision worker — describes images for ALL providers (not user-selectable) |

> **Image routing:** any request (any provider) that contains image content is
> intercepted by the gateway and each image is described by Z.AI `glm-4.6v`
> (coding-plan endpoint) before the selected text model is called. A Z.AI API
> key is therefore required to send images. If it is missing or the vision
> model rejects the image, the request **fails fast** with a clear `vision_error`
> rather than silently dropping the image.

- Pricing: [https://docs.z.ai/guides/overview/pricing](https://docs.z.ai/guides/overview/pricing)
- API docs: [https://docs.z.ai/api-reference/introduction](https://docs.z.ai/api-reference/introduction)
- API key: [https://z.ai/manage-apikey/apikey-list](https://z.ai/manage-apikey/apikey-list) (GLM Coding Plan key)

| Parameter          | `glm-5.2`                                                     | `glm-4.7`               |
| ------------------ | ------------------------------------------------------------- | ----------------------- |
| `thinking`         | `{type: "enabled"}` when reasoning; else `{type: "disabled"}` | `{type: "enabled"       | "disabled"}` |
| `reasoning_effort` | Normalized → `off`/`high`/`max`                               | Deleted (not supported) |

---



### 🐪 Thaura <!-- omit in toc -->

[Thaura](https://thaura.ai/) is an AI platform that combines technical
excellence with ethical principles, designed to support Palestinian liberation
and mission-aligned technology development.


| API model ID | Input / MTok | Output / MTok | Role                                |
| ------------ | ------------ | ------------- | ----------------------------------- |
| `thaura`     | $0.50        | $2.00         | OpenAI-compatible chat and tool use |


- Pricing: [https://thaura.ai/api-platform](https://thaura.ai/api-platform)
- API docs: [https://thaura.ai/api-platform](https://thaura.ai/api-platform)

> **🇵🇸 Incubated by Tech for Palestine**
>
> Click to expand  
>
>
> [Tech for Palestine](https://techforpalestine.org/) (T4P) is a coalition of founders, engineers, product marketers, investors, and other professionals working in support of Palestinian liberation.
>
> **What is Tech for Palestine?**
>
> Tech for Palestine is first and foremost an incubator for advocacy projects. They rally volunteers from across the tech world — founders, engineers, marketers, investors, and more — all committed to Palestinian liberation.
>
> The T4P Incubator helps pro-Palestine advocates build, grow, and scale their work towards a Free Palestine. They support projects — whether collections of individuals, registered non-profits, or even companies — whose mission helps Palestine, especially advocacy groups building technical products or in the tech space.
>
> The Incubator is free and provides:
>
> - 👥 **Volunteers** - Access to skilled professionals
> - 📢 **Marketing Support** - Help spreading your message
> - 🎓 **Mentorship** - Guidance from experienced professionals
> - 🔗 **Connections** - Links to the broader Palestinian advocacy ecosystem
>
> **Get Involved:**
>
> - Volunteer your skills
> - Join their Discord
> - Start a project of your own
> - Be a mentor
> - Hire Palestinians
>
> Learn more at [techforpalestine.org](https://techforpalestine.org/)
>
>


---


## 🛠 Tech Stack


| Component     | Technology                                                                                                 |
| ------------- | ---------------------------------------------------------------------------------------------------------- |
| Language      | Go 1.26.5+                                                                                                 |
| CLI framework | [Cobra](https://cobra.dev/)                                                                                |
| Tunnel        | [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/) named tunnel |
| Upstream APIs | OpenAI-compatible chat completions (Moonshot + DeepSeek + Thaura + Z.AI)                                   |

---


## 📁 File Structure

```
main.go                   # Entry point
internal/
  cli/                    # Cobra command tree (start, stop, status, doctor, …)
    start/                # Start gateway / background daemon / tunnel
    setcmd/               # `set` command
    wizard/               # Interactive init wizard
  config/                 # App settings, paths, upstream URL helpers
  crypto/                 # Encrypt upstream keys + gateway key gen
  gateway/                # HTTP server, sanitizer, optimizer, proxy, auth
    vision/               # Image description via glm-4.6v (fail-fast, content-hash cache)
  tunnel/                 # cloudflared supervisor
  doctor/                 # Health checks
  usage/                  # Pricing tables, token/cost store, slog helpers
  usageui/                # Embedded usage dashboard (HTTP, Chart.js)
.cursor/rules/            # Agent conventions
.cursor/skills/           # Invocable workflows
planning/          # MVP task sequence (T01–T10)
```

---



## 🖥 CLI Commands

All output is JSON on stdout. Pipe through `jq` for readability.


| Command                      | Description                                                                                                                                                                                                                                                                                                                                                         |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `discursive start`           | Start gateway on `localhost:4001`. `--background` forks to daemon. `--log-level` (debug/info/warn/error). `--tunnel` (named/none/quick), `--public-url`. `--subagent-router` (on by default), `--compress` (off by default), `--verbosity` (off by default). Auto-invokes `init` if config is incomplete on first run. See [Subagent Routing](#-subagent-routing) and [Compression](#-compression). |
| `discursive stop`            | Send SIGTERM via PID file. No-op if not running.                                                                                                                                                                                                                                                                                                                    |
| `discursive status`          | Config dump + runtime state: PID alive? uptime? log file path/size, tunnel mode, model mapping. Gateway key masked by default; `--show-key` prints the full key.                                                                                                                                                                                                    |
| `discursive logs`            | Pretty-print `gateway.log` with colored level prefixes. `--follow` (`-f`) for live tail (uses fsnotify — no polling). `-n N` for last N lines. File auto-rotates at ~2 MB, keeps 2 backups.                                                                                                                                                                         |
| `discursive log-level [debug | info                                                                                                                                                                                                                                                                                                                                                                | warn | error]`      | Show or set log verbosity. Set persists per-process; hints how to export `DISCURSIVE_LOG_LEVEL` for persistence. |
| `discursive doctor`          | Health checks: keys present, port available, local/public HTTP health, tunnel mode, cloudflared binary, logs writable.                                                                                                                                                                                                                                              |
| `discursive usage`           | Token + cost estimates per session/model.                                                                                                                                                                                                                                                                                                                           |
| `discursive set`             | Configure settings via flags. `--moonshot-key`, `--deepseek-key`, `--thaura-key`, `--zai-key`, `--tunnel-token`, `--public-url`, `--rotate-gateway-key`, `--model`. Combine several in one call. `--show-key` prints the full gateway key.                                                                                                                          |
| `discursive completion [bash | zsh                                                                                                                                                                                                                                                                                                                                                                 | fish | powershell]` | Generate a shell completion script (see [Shell Completion](#️-shell-completion)).                                 |
| `discursive version`         | Print version.                                                                                                                                                                                                                                                                                                                                                      |


JSON slog on **stdout**, interactive prompts on **stderr** — pipe-friendly.

---



## ⌨️ Shell Completion

Cobra's built-in `completion` command generates scripts for bash, zsh, fish, and
PowerShell. After install, Tab completes subcommands, flags, log levels, tunnel
modes, and model aliases.

**zsh** (macOS default):

```bash
# Oh My Zsh
mkdir -p ~/.oh-my-zsh/completions
discursive completion zsh > ~/.oh-my-zsh/completions/_discursive

# Or any zsh with compinit (add to ~/.zshrc, then restart the shell):
discursive completion zsh > "${fpath[1]}/_discursive"
```

**bash** (Linux / macOS with bash-completion):

```bash
# Linux (system-wide)
discursive completion bash | sudo tee /etc/bash_completion.d/discursive >/dev/null

# Or per-session / add to ~/.bashrc:
source <(discursive completion bash)
```

**fish:**

```bash
discursive completion fish > ~/.config/fish/completions/discursive.fish
```

Verify: type `discursive`  then Tab — you should see subcommands.

---



## 🌍 Environment Variables


| Variable                | Purpose                                                   | Default |
| ----------------------- | --------------------------------------------------------- | ------- |
| `DISCURSIVE_LOG_LEVEL`  | Log verbosity: `debug`, `info`, `warn`, `error`           | `info`  |
| `DISCURSIVE_USAGE_IDLE` | Idle window before emitting a usage summary (Go duration) | `30s`   |


---



## 🔄 CI / Release


| Trigger                  | Job                          | What runs                                            |
| ------------------------ | ---------------------------- | ---------------------------------------------------- |
| Push to `main` / PR      | Verify (lint + test + build) | `golangci-lint` + `go test ./...` + `go build ./...` |
| Tag `v*` (e.g. `v0.1.0`) | Release (GoReleaser)         | Cross-compile + publish binaries to GitHub Releases  |


The verify job must pass before release runs. Releases use the built-in
`secrets.GITHUB_TOKEN` (no custom PAT needed).

Binaries are built via [GoReleaser](https://goreleaser.com/) and published at
[https://github.com/commoddity/discursive/releases](https://github.com/commoddity/discursive/releases).

---



## 🔒 Security

- Upstream Moonshot, DeepSeek, Thaura, and Z.AI keys are **encrypted at rest** and never sent
to Cursor, never appear in logs
- Cursor receives only the generated gateway key (`sk-...`)
- Gateway key is **masked by default** in `status` / `rotate-gateway-key`;
pass `--show-key` when you need the full value for Cursor setup
- Gateway binds to loopback (`localhost`); the Cloudflare tunnel is the only
public surface
- All output is JSON on stdout — never emit upstream secrets or raw headers

---



## 🧪 Methodology



Discursive was developed using [Turboplan](https://github.com/commoddity/turboplan),
a methodology for AI-assisted software delivery. Turboplan structures work into
sequenced phases, enforces layered verification ("don't advance until the layer
below passes"), and maintains self-evolving agent rules that capture failure
patterns. Every feature in this project was planned, executed, and verified
through Turboplan's task lifecycle.

---



## 📜 License

MIT