# FDE roles collector

Go program that pulls **live Forward Deployed / Field Deployment Engineer (FDE)** listings across companies and writes **matching `.txt` and `.xlsx` files**.

Two data sources:

| Mode | When it runs | Source |
| --- | --- | --- |
| `auto` (default) | Google keys present → Google; otherwise career boards | picks for you |
| `google` | `GOOGLE_API_KEY` + `GOOGLE_CSE_ID` required | [Custom Search JSON API](https://developers.google.com/custom-search/v1/overview) |
| `live` | no Google keys needed | public Greenhouse / Lever / Ashby job APIs |

## Requirements

- [Go](https://go.dev/dl/) 1.22+ (this machine has Go 1.27 at `C:\Program Files\Go\bin`)
- Internet access
- For Google search: a Custom Search API key and Programmable Search Engine ID

If Command Prompt says `'go' is not recognized`, either open a **new** terminal after installing Go, or run:

```bat
set PATH=C:\Program Files\Go\bin;%PATH%
```

## Run

```bat
cd C:\Users\dell\OneDrive\Desktop\GO
go run .
```

Or after a build:

```bat
go build -o fde-roles.exe .
fde-roles.exe
```

Without Google keys, it falls back to live career boards and still writes txt + Excel.

### Google Custom Search (optional)

1. Enable [Custom Search API](https://console.cloud.google.com/apis/library/customsearch.googleapis.com)
2. Create an [API key](https://console.cloud.google.com/apis/credentials)
3. Create a [Programmable Search Engine](https://programmablesearchengine.google.com/) that searches the **entire web**, then copy the engine ID (`cx`)

```bat
set GOOGLE_API_KEY=your-key
set GOOGLE_CSE_ID=your-cx
go run . -source google
```

PowerShell:

```powershell
$env:GOOGLE_API_KEY="your-key"
$env:GOOGLE_CSE_ID="your-cx"
go run .
```

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-source` | `auto` | `google`, `live`, or `auto` |
| `-companies` | built-in list | comma-separated names, e.g. `"Palantir,OpenAI,Anthropic"` |
| `-out` | `output` | output folder |
| `-pages` | `1` | Google result pages per company (1–10, 10 hits each) |
| `-key` | `GOOGLE_API_KEY` | API key |
| `-cx` | `GOOGLE_CSE_ID` or `GOOGLE_CX` | search engine ID |
| `-timeout` | `20s` | HTTP timeout |

Examples:

```bat
go run . -companies "Palantir,OpenAI,Anthropic" -out output
go run . -source live
go run . -source google -pages 2
```

## Output

Files land in `output\`:

- Per company: `palantir.txt` / `palantir.xlsx`, `openai.txt` / `openai.xlsx`, …
- Combined: `all_fde_roles.txt` / `all_fde_roles.xlsx`

Columns: company, title, URL, host, snippet, search query, fetched-at (UTC).

Empty company files mean that board had no matching FDE titles or the public API returned nothing (common for Google, Meta, Microsoft, Amazon without the Custom Search keys).

## Default companies

Palantir, OpenAI, Anthropic, Scale AI, Anduril, Databricks, Stripe, Google, Meta, Microsoft, Amazon, Tesla, SpaceX, Snowflake, CrowdStrike, Applied Intuition, Shield AI, C3.ai, Helsing, Glean.

## Project layout

```
main.go      Google Custom Search + CLI
live.go      Greenhouse / Lever / Ashby fallback
output\      generated txt and Excel (gitignored)
```
