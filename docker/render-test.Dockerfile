# Throwaway image for running BoTeX's render-bound tests (pkg/commands
# TestRenderLatex_*) against a real pdflatex/convert/cwebp/prlimit
# toolchain. Not used to build or deploy the bot itself.
#
# The package list is shared with .github/workflows/test.yml through
# render-test-packages.txt, so the CI workflow and this image install the
# exact same render toolchain. prlimit itself needs no package: it ships in
# Debian's base util-linux, already present in this base image.
FROM golang:1.27-bookworm

COPY docker/render-test-packages.txt /tmp/render-test-packages.txt
RUN apt-get update \
    && xargs -a /tmp/render-test-packages.txt apt-get install -y --no-install-recommends \
    && rm -rf /var/lib/apt/lists/* /tmp/render-test-packages.txt

# Debian's ImageMagick-6 policy.xml denies the PDF coder by default, which
# breaks the render's `convert` step reading pdflatex's output, so the
# restriction is removed in this test image.
RUN sed -i '/pattern="PDF"/d' /etc/ImageMagick-6/policy.xml

ENV BOTEX_REQUIRE_RENDER_TOOLS=1

WORKDIR /src
