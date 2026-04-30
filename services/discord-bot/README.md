# TalePanel Discord Bot

Receives GitHub webhook events and posts formatted embeds to TalePanel's Discord
channels via the `biti` bot token.

## Events handled

- `release` (published) → `🎁┃releases`
- `pull_request` (opened/closed/reopened) → `🔀┃pull-requests`
- `push` (default branch) → `💾┃commits`
- `issues` (opened/closed/reopened) → `🐛┃bug-reports`
- `star` (created) → `🎁┃releases`

## Environment

| Variable | Description |
|---|---|
| `DISCORD_TOKEN` | Bot token (Developer Portal → Bot → Reset Token) |
| `GITHUB_WEBHOOK_SECRET` | Shared secret configured on the GitHub webhook |
| `CH_RELEASES` | Channel ID for releases & stars |
| `CH_PULL_REQUESTS` | Channel ID for PRs |
| `CH_COMMITS` | Channel ID for commits |
| `CH_BUG_REPORTS` | Channel ID for issues |
| `PORT` | Listen port (default `3030`) |

## GitHub webhook config

- Payload URL: `https://<your-host>/webhook/github`
- Content type: `application/json`
- Secret: same as `GITHUB_WEBHOOK_SECRET`
- Events: `Releases`, `Pull requests`, `Pushes`, `Issues`, `Stars`

## Run locally

```bash
cd services/discord-bot
DISCORD_TOKEN=... GITHUB_WEBHOOK_SECRET=... CH_RELEASES=... \
  CH_PULL_REQUESTS=... CH_COMMITS=... CH_BUG_REPORTS=... \
  npm install && npm start
```
