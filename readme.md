# [bot]: alfred

[![CodeQL](https://github.com/totallynotdavid/BoTeX/actions/workflows/codeql.yml/badge.svg)](https://github.com/totallynotdavid/BoTeX/actions/workflows/codeql.yml)
[![lint-and-testing](https://github.com/totallynotdavid/BoTeX/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/totallynotdavid/BoTeX/actions/workflows/golangci-lint.yml)
[![test](https://github.com/totallynotdavid/BoTeX/actions/workflows/test.yml/badge.svg)](https://github.com/totallynotdavid/BoTeX/actions/workflows/test.yml)

WhatsApp bot for rendering LaTeX equations. Built with Go and
[whatsmeow](https://github.com/tulir/whatsmeow), includes structured logging,
rate limiting, performance tracking, and rank-based permissions.

## Installation

The bot requires TeX Live for rendering equations, ImageMagick to rasterize the
rendered PDF, and `cwebp` to convert it. ImageMagick's PDF delegate needs
Ghostscript, or `convert` fails; install system dependencies first:

```bash
sudo apt-get install gcc build-essential imagemagick ghostscript webp
```

Install TeX Live using the provided script, or follow the
[quick install guide](https://www.tug.org/texlive/quickinstall.html) and add
these packages: `amsmath amsfonts physics standalone preview bm`

```bash
./utils/latex.sh
```

On Debian/Ubuntu, `apt-get` can install TeX Live instead of the script above.
This is the exact combination `.github/workflows/test.yml` installs and runs the
render-bound tests against (`docker/render-test-packages.txt`):

```bash
sudo apt-get install texlive-latex-base texlive-latex-recommended \
    texlive-latex-extra texlive-fonts-recommended texlive-science \
    texlive-pictures
```

Without any of that installed locally, `mise run test:render` builds the same
toolchain into a disposable Docker image (`docker/render-test.Dockerfile`) and
runs the full test suite, including the render-bound tests, against it.

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

Set `BOTEX_OWNER_JIDS` to a comma-separated list of WhatsApp JIDs (for example
`15551234567@s.whatsapp.net`) before first startup. Every JID listed there is
granted the owner rank each time the bot starts, so there is no need to touch
the database by hand to bootstrap a fresh install. Seeding is idempotent: it
never creates duplicates, and it never downgrades or changes the rank of a user
who is already registered with a different rank (a warning is logged instead). A
malformed JID in `BOTEX_OWNER_JIDS` fails configuration loading with a clear
error rather than being silently ignored.

By default, the bot ignores messages sent from its own WhatsApp account so it
cannot be triggered by its own replies. Set `BOTEX_PROCESS_OWN_MESSAGES=true` to
let commands sent from the bot's own account run.

## Running

Start the bot and scan the QR code when prompted:

```bash
mise run dev
```

The bot requires authentication before responding to commands. Once your
WhatsApp JID is listed in `BOTEX_OWNER_JIDS`, it is registered with the owner
rank automatically.

The rank system has three levels: owner (full access), admin (user management),
and user (basic commands), but only `help` and `latex` are wired up as chat
commands today; there is no `!register_user` or `!register_group` command yet.
To bring in further users or groups, insert directly into the `users` or
`registered_groups` tables (see [pkg/auth/readme.md](pkg/auth/readme.md) for the
schema and the `RegisterUser`/`RegisterGroup` API those tables back), or add
more JIDs to `BOTEX_OWNER_JIDS` if they should also be owners.

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
