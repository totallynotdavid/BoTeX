# [bot]: alfred

[![CodeQL](https://github.com/totallynotdavid/BoTeX/actions/workflows/codeql.yml/badge.svg)](https://github.com/totallynotdavid/BoTeX/actions/workflows/codeql.yml)
[![lint-and-testing](https://github.com/totallynotdavid/BoTeX/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/totallynotdavid/BoTeX/actions/workflows/golangci-lint.yml)

WhatsApp bot for rendering LaTeX equations. Built with Go and
[whatsmeow](https://github.com/tulir/whatsmeow), includes structured logging,
rate limiting, performance tracking, and rank-based permissions.

## Installation

The bot requires TeX Live for rendering equations and ImageMagick for image
processing. Install system dependencies first:

```bash
sudo apt-get install gcc build-essential imagemagick webp
```

Install TeX Live using the provided script, or follow the
[quick install guide](https://www.tug.org/texlive/quickinstall.html) and add
these packages: `amsmath amsfonts physics standalone preview bm`

```bash
./utils/latex.sh
```

Install [mise](https://mise.jdx.dev/) for managing Go and tooling:

```bash
curl https://mise.run | sh
```

Clone the repository and set up the project:

```bash
git clone https://github.com/totallynotdavid/BoTeX
cd BoTeX
mise install
go mod download
```

## Configuration

Copy the example config and edit the values:

```bash
cp .env.example .env
```

The bot needs at minimum a log level and database path. Log level controls
verbosity and accepts DEBUG, INFO, WARN, or ERROR. Use INFO or WARN in
production. Debug mode logs all WhatsApp events and operation timing.

Rate limiting defaults to five requests per minute. Adjust with
`BOTEX_RATE_LIMIT_REQUESTS` and `BOTEX_RATE_LIMIT_PERIOD`. The period accepts Go
duration strings like "1m" or "30s".

The bot auto-detects binary paths for pdflatex, convert, and cwebp. Override
with explicit paths if detection fails: `BOTEX_PDFLATEX_PATH`,
`BOTEX_CONVERT_PATH`, `BOTEX_CWEBP_PATH`.

Database defaults to `file:botex.db?_foreign_keys=on&_journal_mode=WAL`. Change
the path or disable WAL mode with `BOTEX_DB_PATH` if needed.

Performance tracking has three modes set via `BOTEX_TIMING_LEVEL`: disabled,
basic (logs slow operations), or detailed (logs all operation timing).

## Running

Start the bot and scan the QR code when prompted:

```bash
mise run dev
```

The bot requires authentication before responding to commands. After first
startup, register yourself as owner by adding your WhatsApp JID to the database.
Your JID appears in logs when you send a message:

```bash
sqlite3 botex.db
INSERT INTO users (id, rank, registered_at, registered_by)
VALUES ('your_number@s.whatsapp.net', 'owner', datetime('now'), 'system');
```

The rank system has three levels: owner (full access), admin (user management),
and user (basic commands). Groups must also be registered before the bot
responds in them. See [pkg/auth/readme.md](pkg/auth/readme.md) for permission
details.

## Usage

Commands use the `!` prefix. Send `!help` to see available commands:

```
!help
!latex \frac{a}{b}
```

The bot renders equations as WebP images. Rate limiting applies automatically
with cleanup of expired limits.

---

Built with [whatsmeow](https://github.com/tulir/whatsmeow), inspired by
[matterbridge](https://github.com/42wim/matterbridge).
