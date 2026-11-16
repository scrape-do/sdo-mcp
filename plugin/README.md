# Scrape.do plugin for Claude

Lets Claude scrape any public web page through Scrape.do's anti-bot proxy
network, pull structured Google Search, Amazon and Walmart data from Scrape.do
plugins, and run batches of up to 1,000 URLs as async jobs.

Works in Claude Code and in Cowork running on your computer. It runs the
`sdo-mcp` program on your own machine.

## Setup (about 2 minutes)

### 1. Get your API token

Sign in at [app.scrape.do](https://app.scrape.do) (a free plan is available)
and copy the API token shown on the dashboard.

### 2. Install the `sdo-mcp` program

**macOS or Linux** — open Terminal and paste:

```
brew install scrape-do/tap/sdo-mcp
```

No Homebrew? Install it first from [brew.sh](https://brew.sh).

**Windows** — open PowerShell and paste these two lines:

```
scoop bucket add scrape-do https://github.com/scrape-do/scoop-bucket
scoop install scrape-do/sdo-mcp
```

No Scoop? Install it first from [scoop.sh](https://scoop.sh).

Check it worked: `sdo-mcp version` prints a version number.

### 3. Install the plugin

Install Scrape.do from the plugin directory, or in Claude Code run:

```
/plugin install scrape-do --marketplace scrape-do/sdo-mcp
```

When asked, paste your API token. It is kept in your system's secure
credential store, not in a settings file.

### 4. Try it

- "Scrape https://example.com and summarize it"
- "Search Google for best running shoes 2026 in the US"
- "Get the Amazon product page for ASIN B0C7BKZ883"
- "Create an async job for these 20 URLs and show me the results"

## Tools

| Tool | What it does | Spends credits |
| --- | --- | --- |
| `scrape` | Fetches a page (rendering, geo-targeting, sticky sessions) | Yes |
| `scrape_plugin` | Calls a Scrape.do plugin such as `google/search` or `amazon/pdp` | Yes |
| `async_job_create`, `async_plugin_job_create` | Submit an async batch | Yes |
| `async_job_status`, `async_jobs_list`, `async_task_result`, `async_account` | Read async jobs and account | No |
| `async_job_cancel` | Cancel an async job (cannot be undone) | No |

Every result shows its HTTP status and credit cost. Scraped content is marked
as untrusted third-party data, so instructions inside a page are not treated
as commands.

## Troubleshooting

- **The Scrape.do tools don't appear:** run `sdo-mcp version` in a terminal.
  If it isn't found, repeat step 2, then restart Claude.
- **"refused token" or no credits:** check the token and your plan at
  [app.scrape.do](https://app.scrape.do). To change the token, reinstall the
  plugin or update it in the plugin's settings.

## Privacy Policy

The plugin runs on your computer and sends requests only to the Scrape.do API
(`api.scrape.do` and `q.scrape.do`), authenticated with your API token. The
`sdo-mcp` program has no telemetry and does not store your token, your
conversations or the pages it fetches. Scrape.do processes the URLs and
parameters you send under its privacy policy:
[scrape.do/privacy-policy](https://scrape.do/privacy-policy/). Questions:
[scrape.do](https://scrape.do).

## License

Apache-2.0. See [LICENSE](LICENSE).
