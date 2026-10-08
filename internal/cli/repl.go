package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/whatsapp/fake"
)

const (
	replUser bot.JID = "51900000001@s.whatsapp.net"
)

type replOptions struct {
	owner bool
}

// RunREPL builds a bot over SQLite state and an in-memory transport. It never
// opens a WhatsApp session, so callers can run a complete text conversation
// without pairing a phone. The REPL user is a normal configured rank by
// default; the command-line runner exposes an owner switch for administration
// and debugging.
func RunREPL(ctx context.Context, cmd Command, env *config.Env, log *slog.Logger, input io.Reader, out io.Writer) error {
	cfg, err := readSettings(cmd, env)
	if err != nil {
		return err
	}

	return repl(ctx, cfg, log, input, out, replOptions{})
}

func repl(ctx context.Context, cfg settings, log *slog.Logger, input io.Reader, out io.Writer, opts replOptions) (err error) {
	if opts.owner {
		cfg.shared.Owners = append(cfg.shared.Owners, string(replUser))
	}

	setup, err := prepare(ctx, cfg, log)
	if err != nil {
		return err
	}

	defer func() { err = errors.Join(err, setup.close()) }()

	if !opts.owner {
		err = registerREPLUser(ctx, setup.users, cfg)
		if err != nil {
			return err
		}
	}

	return runREPLSession(ctx, setup, cfg, log, input, out)
}

func registerREPLUser(ctx context.Context, users *auth.Service, cfg settings) error {
	if len(cfg.ranks) == 0 {
		return nil
	}

	err := users.RegisterUser(ctx, string(replUser), cfg.ranks[0].Name, "offline-repl")
	if errors.Is(err, auth.ErrUserExists) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("register offline REPL user: %w", err)
	}

	return nil
}

func runREPLSession(ctx context.Context, setup prepared, cfg settings, log *slog.Logger, input io.Reader, out io.Writer) error {
	client := fake.New()

	runCtx, stop := context.WithCancel(ctx)

	defer stop()

	done := startREPLRuntime(runCtx, client, setup, cfg, log)

	err := client.WaitConnected(runCtx)
	if err != nil {
		stop()
		<-done

		return fmt.Errorf("wait for fake connection: %w", err)
	}

	err = writeREPLHeader(out)
	if err != nil {
		stop()
		<-done

		return err
	}

	loopErr := runREPLLoop(runCtx, client, input, out)

	stop()

	runErr := <-done

	if loopErr != nil {
		return loopErr
	}

	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return fmt.Errorf("REPL bot: %w", runErr)
	}

	return nil
}

func startREPLRuntime(ctx context.Context, client *fake.Client, setup prepared, cfg settings, log *slog.Logger) <-chan error {
	runtime := bot.New(client, setup.built.App, log, bot.Options{
		Groups:      setup.built.Groups,
		OwnMessages: cfg.shared.OwnMessages,
		AllowOnly:   toJIDs(cfg.shared.AllowOnly),
		Limiter:     setup.limiter,
		MaxInFlight: cfg.shared.MaxInFlight,
	})

	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()

	return done
}

func toJIDs(values []string) []bot.JID {
	result := make([]bot.JID, len(values))
	for i, value := range values {
		result[i] = bot.JID(value)
	}

	return result
}

func writeREPLHeader(out io.Writer) error {
	_, err := fmt.Fprintln(out, "Offline bot REPL — no WhatsApp login is used. Type :quit to exit.")
	if err != nil {
		return fmt.Errorf("write REPL header: %w", err)
	}

	return nil
}

func runREPLLoop(ctx context.Context, client *fake.Client, input io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(input)

	for {
		err := writePrompt(out)
		if err != nil {
			return err
		}

		if !scanner.Scan() {
			break
		}

		text := strings.TrimSpace(scanner.Text())
		if text == ":quit" || text == ":exit" {
			return nil
		}

		if text == "" {
			continue
		}

		err = runREPLTurn(ctx, client, text, out)
		if err != nil {
			return err
		}
	}

	err := scanner.Err()
	if err != nil {
		return fmt.Errorf("read REPL input: %w", err)
	}

	return nil
}

func writePrompt(out io.Writer) error {
	_, err := fmt.Fprint(out, "you> ")
	if err != nil {
		return fmt.Errorf("write REPL prompt: %w", err)
	}

	return nil
}

func runREPLTurn(ctx context.Context, client *fake.Client, text string, out io.Writer) error {
	beforeSent := len(client.Sent())
	beforeReactions := len(client.Reactions())
	client.Deliver(bot.Message{Sender: replUser, PushName: "REPL user", Text: text})

	err := client.WaitForWork(ctx)
	if err != nil {
		return fmt.Errorf("wait for bot reply: %w", err)
	}

	return printReplies(out, client, beforeSent, beforeReactions)
}

func printReplies(out io.Writer, client *fake.Client, sent, reactions int) error {
	replies := client.Sent()[sent:]

	newReactions := client.Reactions()[reactions:]
	if len(replies) == 0 && len(newReactions) == 0 {
		_, err := fmt.Fprintln(out, "bot> (no reply)")
		if err != nil {
			return fmt.Errorf("write no-reply notice: %w", err)
		}
	}

	for _, reply := range replies {
		err := printReply(out, reply)
		if err != nil {
			return err
		}
	}

	for _, reaction := range newReactions {
		_, err := fmt.Fprintf(out, "bot> reaction %s\n", reaction.Emoji)
		if err != nil {
			return fmt.Errorf("write bot reaction: %w", err)
		}
	}

	return nil
}

func printReply(out io.Writer, reply fake.Sent) error {
	if reply.Image != nil {
		_, err := fmt.Fprintf(out, "bot> [image %s, %d bytes]\n", reply.Image.MIME, len(reply.Image.Data))
		if err != nil {
			return fmt.Errorf("write bot reply: %w", err)
		}

		return nil
	}

	_, err := fmt.Fprintf(out, "bot> %s\n", reply.Text)
	if err != nil {
		return fmt.Errorf("write bot reply: %w", err)
	}

	return nil
}
