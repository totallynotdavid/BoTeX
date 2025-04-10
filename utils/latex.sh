#!/bin/bash

set -e

# Helper functions
log_info() { echo -e "\n► $1"; }
log_success() { echo -e "✓ $1"; }
log_error() { echo -e "✗ $1" >&2; }

# Installation directory
TEXLIVE_DIR="$HOME/texlive"

log_info "Starting TexLive installation"

# Create temporary directory for installation
TEMP_DIR=$(mktemp -d)
cd "$TEMP_DIR"

log_info "Downloading TexLive installer"
if ! wget -q https://mirror.ctan.org/systems/texlive/tlnet/install-tl-unx.tar.gz; then
    log_error "Failed to download installer"
    exit 1
fi

log_info "Extracting installer"
tar -xzf install-tl-unx.tar.gz
cd "$(find . -maxdepth 1 -type d -name "install-tl-*" | head -1)" || exit 1

log_info "Creating basic installation profile"
cat > texlive.profile << EOF
selected_scheme scheme-basic
TEXDIR $TEXLIVE_DIR
TEXMFCONFIG \$TEXMFSYSCONFIG
TEXMFHOME \$TEXMFLOCAL
TEXMFLOCAL $TEXLIVE_DIR/texmf-local
TEXMFSYSCONFIG $TEXLIVE_DIR/texmf-config
TEXMFSYSVAR $TEXLIVE_DIR/texmf-var
TEXMFVAR \$TEXMFSYSVAR
tlpdbopt_autobackup 0
tlpdbopt_install_docfiles 0
tlpdbopt_install_srcfiles 0
option_doc 0
option_src 0
EOF

log_info "Installing TexLive (basic scheme)"
./install-tl --profile=texlive.profile --no-interaction

# Find binary directory
TEXLIVE_BIN=$(find "$TEXLIVE_DIR/bin" -type d -name "x86_64*" | head -1)

if [ -z "$TEXLIVE_BIN" ]; then
    log_error "Failed to locate TexLive binary directory"
    exit 1
fi

log_info "Setting up PATH for TexLive"
export PATH="$TEXLIVE_BIN:$PATH"

# Install a few recommended additional packages
log_info "Installing additional essential packages"
tlmgr install \
    collection-fontsrecommended \
    collection-latexrecommended \
    standalone \
    --verify-repo=none

log_info "Adding TexLive to PATH permanently"
if ! grep -q "texlive/bin" "$HOME/.bashrc"; then
    echo -e "\n# TexLive path\nexport PATH=\"$TEXLIVE_BIN:\$PATH\"" >> "$HOME/.bashrc"
    log_success "TexLive path added to .bashrc"
fi

# Cleanup
cd
rm -rf "$TEMP_DIR"

log_success "TexLive installation complete!"
log_info "Installed size:"
du -sh "$TEXLIVE_DIR"
log_info "You may need to run 'source ~/.bashrc' or restart your terminal to use TexLive"

# Verify installation
if command -v pdflatex >/dev/null 2>&1; then
    log_success "Verified: pdflatex is now available ($(pdflatex --version | head -n1))"
else
    log_error "Verification failed: pdflatex command not found"
    echo "Try running: source ~/.bashrc"
fi