# BoTeX

[![CodeQL](https://github.com/totallynotdavid/BoTeX/actions/workflows/codeql.yml/badge.svg)](https://github.com/totallynotdavid/BoTeX/actions/workflows/codeql.yml) [![lint-and-testing](https://github.com/totallynotdavid/BoTeX/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/totallynotdavid/BoTeX/actions/workflows/golangci-lint.yml)

BoTeX is a WhatsApp bot that renders LaTeX equations into images. I created this project while learning Go, drawing inspiration from how [matterbridge](https://github.com/42wim/matterbridge) implements WhatsApp integration using the [whatsmeow](https://github.com/tulir/whatsmeow) library. While it currently has a limited set of commands (`!latex` for rendering equations and `!help` for viewing available commands), it serves as a simple example of building a WhatsApp bot in Go.

## Getting Started

**Setting up BoTeX requires a few tools**: [Go](https://golang.org/dl/) for running the bot, [TeX Live](https://www.tug.org/texlive/quickinstall.html) for rendering equations, and [ImageMagick](https://imagemagick.org/script/download.php) for image processing. Let's walk through the setup process.

First, you'll need Go installed (we're using version 1.24.2). If you don't have it already, here's how to get it running:

```bash
curl -OL https://go.dev/dl/go1.24.2.linux-amd64.tar.gz
sudo tar -C /usr/local -xvf go1.24.2.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
source ~/.profile
```

> [!NOTE]
> These commands download the Go tarball, extract it to `/usr/local`, and add the Go binary directory to your `PATH`. You can check the installation with `go version`.

Next up is TeX Live, which handles the actual equation rendering. The installation takes a while, but it's straightforward:

```bash
sudo apt-get install perl
cd /tmp
curl -L -o install-tl-unx.tar.gz https://mirror.ctan.org/systems/texlive/tlnet/install-tl-unx.tar.gz
tar -xzf install-tl-unx.tar.gz
cd install-tl-*
sudo perl ./install-tl --no-interaction
echo 'export PATH=$PATH:/usr/local/texlive/2024/bin/x86_64-linux' >> ~/.profile
source ~/.profile
```

> [!NOTE]  
> You may need to install LaTeX packages using `tlmgr install <package>`. For our purposes, `amsmath`, `amsfonts`, `physics` and `bm` are needed.

Finally, let's get the bot up and running. You'll need some build tools and the bot code itself:

```bash
# Install required system packages
sudo apt-get install gcc build-essential

# Get the bot (either clone with git or download ZIP from GitHub)
git clone https://github.com/totallynotdavid/BoTeX
cd BoTeX

# Run the bot
export CGO_ENABLED=1
go run .
```

## Using the Bot

Once you've got everything installed, using the bot is simple. Run it with `go run .` and you'll see a QR code in your terminal - scan this with WhatsApp to link the bot. After that, you can start rendering equations by sending messages in this format:

```txt
!latex <your_equation>
```

For example, try these:

```txt
!latex x = \frac{-b \pm \sqrt{b^2 - 4ac}}{2a}
```

or

```txt
!latex \int_{a}^{b} f(x)\,dx = F(b) - F(a)
```

## When Things Go Wrong

If something's not working, it's usually one of three things:

1. The installation didn't complete properly - run `pdflatex --version` and `convert --version` to check if TeX Live and ImageMagick are installed correctly.
2. `whatsmeow` have known issues regarding some messages showing up only on WhatsApp web but not on a phone. If you're not seeing messages, try seeing if they show up on WhatsApp web.
3. The Go dependencies need updating - try `go clean -modcache` followed by `go get -u`.

If you're running into an issue that isn't solved by checking these, feel free to [open an issue](https://github.com/totallynotdavid/BoTeX/issues) on GitHub. I'm always happy to help get things running smoothly.

This project is open source, so feel free to use it, modify it, or suggest improvements. Thanks to the teams behind whatsmeow and matterbridge for their excellent work that made this project possible!
