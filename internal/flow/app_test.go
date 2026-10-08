//nolint:wsl_v5 // The harness setup is intentionally kept together for behavior tests.
package flow_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/flow"
	"github.com/totallynotdavid/botkit/internal/flow/fsm"
	"github.com/totallynotdavid/botkit/internal/sqlite"
	"github.com/totallynotdavid/botkit/internal/whatsapp/fake"
)

const (
	welcome          = "¡Bienvenidx! Es un placer ayudarte a empezar."
	welcomeBack      = "Qué gusto verte de nuevo."
	unsupportedMedia = "Lo siento, no puedo procesar ese tipo de mensaje. Por favor, envíame un mensaje de texto. 😊"
	testName         = "Ana"
	templateName     = "name"
)

const appWaitLimit = 5 * time.Second

type spy struct {
	app  bot.App
	done chan<- error
}

func (s spy) Handle(ctx context.Context, msg bot.Message, chat *bot.Chat) error {
	err := s.app.Handle(ctx, msg, chat)
	s.done <- err

	return err //nolint:wrapcheck // the runtime sees the app's error as it is.
}

type rig struct {
	client     *fake.Client
	store      *flow.Store
	db         *sql.DB
	flow       *fsm.Flow
	voucherDir string
	done       chan error
	stop       context.CancelFunc
	stopped    chan struct{}
	read       int
	app        *flow.App
}

type settings struct {
	flow  *fsm.Flow
	delay time.Duration
}

func withFlow(parsed *fsm.Flow) func(*settings) {
	return func(s *settings) { s.flow = parsed }
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "flow.db"), sqlite.WithoutSync())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		err := database.Close()
		if err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	return database
}

func start(t *testing.T, opts ...func(*settings)) *rig {
	t.Helper()
	var set settings
	for _, opt := range opts {
		opt(&set)
	}
	if set.flow == nil {
		var err error
		set.flow, err = fsm.EngagingExample(knownAction)
		if err != nil {
			t.Fatal(err)
		}
	}

	database := openDB(t)
	store, err := flow.NewStore(t.Context(), database)
	if err != nil {
		t.Fatal(err)
	}
	env := &rig{
		client:     fake.New(),
		store:      store,
		db:         database,
		flow:       set.flow,
		voucherDir: t.TempDir(),
		done:       make(chan error, 1),
		stopped:    make(chan struct{}),
	}

	app := flow.New(set.flow, store, flow.NewActions(env.voucherDir), flow.Config{TypingDelay: set.delay}, nil)
	env.app = app
	runner := bot.New(env.client, spy{app: app, done: env.done}, nil, bot.Options{})
	ctx, cancel := context.WithCancel(t.Context())
	env.stop = cancel
	go func() {
		defer close(env.stopped)
		err := runner.Run(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run() error = %v, want cancellation", err)
		}
	}()

	waiting, cancelWait := context.WithTimeout(t.Context(), appWaitLimit)
	defer cancelWait()
	err = env.client.WaitConnected(waiting)
	if err != nil {
		t.Fatalf("bot never connected: %v", err)
	}
	t.Cleanup(func() {
		env.stop()
		<-env.stopped
	})

	return env
}

func (r *rig) result(t *testing.T) error {
	t.Helper()
	select {
	case err := <-r.done:
		return err
	case <-time.After(appWaitLimit):
		t.Fatal("Handle did not return")

		return nil
	}
}

func (r *rig) send(t *testing.T, msg bot.Message) error {
	t.Helper()

	msg.Sender = userAna
	if msg.PushName == "" {
		msg.PushName = testName
	}
	r.client.Deliver(msg)

	return r.result(t)
}

func (r *rig) say(t *testing.T, text string) error {
	t.Helper()

	return r.send(t, bot.Message{Text: text})
}

func (r *rig) sendMedia(t *testing.T, kind bot.MediaKind, raw any) error {
	t.Helper()

	return r.send(t, bot.Message{Media: &bot.Media{Kind: kind, MIME: "application/octet-stream", Raw: raw}})
}

func (r *rig) seed(t *testing.T, node string, changes ...func(*flow.State)) {
	t.Helper()
	err := r.store.Turn(t.Context(), userAna, func(state *flow.State) ([]flow.StoredMessage, error) {
		state.CurrentNode = node
		state.UserName = testName
		for _, change := range changes {
			change(state)
		}

		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (r *rig) age(t *testing.T) {
	t.Helper()
	_, err := r.db.ExecContext(t.Context(), `UPDATE user_state SET last_updated = ? WHERE user_id = ?`, time.Now().Add(-25*time.Hour).UTC(), userAna)
	if err != nil {
		t.Fatal(err)
	}
}

func (r *rig) sent() []string {
	sent := r.client.Sent()
	texts := make([]string, 0, len(sent)-r.read)
	for _, message := range sent[r.read:] {
		if message.Text != "" {
			texts = append(texts, message.Text)
		}
	}
	r.read = len(sent)

	return texts
}

func (r *rig) state(t *testing.T) *flow.State {
	t.Helper()

	return r.stateFor(t, userAna)
}

func (r *rig) stateFor(t *testing.T, user bot.JID) *flow.State {
	t.Helper()
	state, err := r.store.Load(t.Context(), user)
	if err != nil {
		t.Fatal(err)
	}

	return state
}

func (r *rig) historyFor(t *testing.T, user bot.JID) []flow.StoredMessage {
	t.Helper()
	history, err := r.store.History(t.Context(), user)
	if err != nil {
		t.Fatal(err)
	}

	return history
}

func (r *rig) text(node, greeting string) string {
	return flow.Render(r.flow.Nodes[node].Message.Content, map[string]string{templateName: testName, "greeting": greeting, "persona": "Luma"})
}

func tiny(t *testing.T, nodes string) *fsm.Flow {
	t.Helper()
	data := `{"start_node":"START","nodes":{` + nodes + `,"NEEDS_ASSISTANCE":{"message":{"content":"a person will help"}}}}`
	parsed, err := fsm.Parse([]byte(data), knownAction)
	if err != nil {
		t.Fatal(err)
	}

	return parsed
}

func knownAction(action string) bool { return flow.NewActions("").Knows(action) }

func requireEqual[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func requireSent(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
}
