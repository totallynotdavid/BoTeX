package whatsapp_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/totallynotdavid/botkit/internal/whatsapp"
)

func TestLoggerWritesModuleAndRespectsLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	root := whatsapp.NewLogger(
		slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
		"Client",
	)
	socket := root.Sub("Socket")

	socket.Debugf("frame %d", 1)
	socket.Infof("connected to %s", "web.whatsapp.com")
	root.Errorf("failed: %v", "boom")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d records, want 2 (debug filtered out):\n%s", len(lines), buf.String())
	}

	for index, want := range []string{
		`level=INFO msg="connected to web.whatsapp.com" module=Client/Socket`,
		`level=ERROR msg="failed: boom" module=Client`,
	} {
		if !strings.Contains(lines[index], want) {
			t.Errorf("record %d = %q, want it to contain %q", index, lines[index], want)
		}
	}
}
