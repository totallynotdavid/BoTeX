//nolint:goconst // Node names and texts repeat across cases; literals keep each case readable against the example flow.
package flow_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/flow"
	"github.com/totallynotdavid/botkit/internal/flow/fsm"
	"github.com/totallynotdavid/botkit/internal/sqlite"
	"github.com/totallynotdavid/botkit/internal/whatsapp/fake"
)

// What the app says in its own words.
const (
	welcome          = "¡Bienvenidx! Es un placer ayudarte a empezar."
	welcomeBack      = "Qué gusto verte de nuevo."
	unsupportedMedia = "Lo siento, no puedo procesar ese tipo de mensaje. Por favor, envíame un mensaje de texto. 😊"
	invalidName      = "No pude reconocer eso como un nombre. ¿Podrías intentarlo de nuevo, por favor?"
	actionFailed     = "Hubo un problema al procesar tu mensaje. Una asesora te ayudará a continuar. Disculpa las molestias."
	menuFallback     = "No entendí tu respuesta 😊 Por favor, revisa las opciones:\n\n"
)

// spy reports what each Handle call returned, so a test can wait for the
// message it delivered and read the error the runtime would see.
type spy struct {
	app  bot.App
	done chan<- error
}

func (s spy) Handle(ctx context.Context, msg bot.Message, chat *bot.Chat) error {
	err := s.app.Handle(ctx, msg, chat)
	s.done <- err

	return err //nolint:wrapcheck // the runtime sees the app's error as it is.
}

// handledMessage is what the app logs once a turn is stored, before it sends
// the replies.
const handledMessage = "message handled"

// tap passes records on and signals each time the app logs handledMessage, so
// a test can act at the moment a turn is stored instead of polling for it.
type tap struct {
	slog.Handler

	handled chan<- struct{}
}

func (h tap) Handle(ctx context.Context, record slog.Record) error {
	err := h.Handler.Handle(ctx, record)

	if record.Message == handledMessage {
		select {
		case h.handled <- struct{}{}:
		default:
		}
	}

	return err //nolint:wrapcheck // the wrapped handler's error as it is.
}

func (h tap) WithAttrs(attrs []slog.Attr) slog.Handler {
	return tap{Handler: h.Handler.WithAttrs(attrs), handled: h.handled}
}

func (h tap) WithGroup(name string) slog.Handler {
	return tap{Handler: h.Handler.WithGroup(name), handled: h.handled}
}

// rig is the flow app running under bot.Run over a fake client, a real store
// in a temporary SQLite file, and vouchers in a temporary directory.
type rig struct {
	client     *fake.Client
	store      *flow.Store
	db         *sql.DB
	flow       *fsm.Flow
	voucherDir string
	logs       *bytes.Buffer
	handled    chan struct{}
	done       chan error
	stop       context.CancelFunc
	stopped    chan struct{}
	read       int
}

type settings struct {
	flow  *fsm.Flow
	delay time.Duration
}

func withFlow(parsed *fsm.Flow) func(*settings) {
	return func(s *settings) { s.flow = parsed }
}

func withDelay(delay time.Duration) func(*settings) {
	return func(s *settings) { s.delay = delay }
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()

	database, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "flow.db"), sqlite.WithoutSync())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		closeErr := database.Close()
		if closeErr != nil {
			t.Errorf("close database: %v", closeErr)
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
		example, err := fsm.Example()
		if err != nil {
			t.Fatal(err)
		}

		set.flow = example
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
		voucherDir: filepath.Join(t.TempDir(), "vouchers"),
		logs:       &bytes.Buffer{},
		handled:    make(chan struct{}, 1),
		done:       make(chan error, waitLimit.Milliseconds()),
		stopped:    make(chan struct{}),
	}

	log := slog.New(tap{
		Handler: slog.NewJSONHandler(env.logs, &slog.HandlerOptions{Level: slog.LevelDebug}),
		handled: env.handled,
	})

	app, err := flow.New(set.flow, store, flow.NewActions(env.voucherDir), flow.Config{TypingDelay: set.delay}, log)
	if err != nil {
		t.Fatal(err)
	}

	runner := bot.New(env.client, spy{app: app, done: env.done}, slog.New(slog.DiscardHandler), bot.Options{})

	ctx, cancel := context.WithCancel(t.Context())
	env.stop = cancel

	t.Cleanup(func() {
		cancel()
		<-env.stopped
	})

	go func() {
		defer close(env.stopped)

		err := runner.Run(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run() error = %v, want the cancellation that stopped it", err)
		}
	}()

	waiting, giveUp := context.WithTimeout(t.Context(), waitLimit)
	defer giveUp()

	err = env.client.WaitConnected(waiting)
	if err != nil {
		t.Fatalf("bot never connected: %v", err)
	}

	return env
}

// result waits for one Handle call and returns its error.
func (r *rig) result(t *testing.T) error {
	t.Helper()

	select {
	case err := <-r.done:
		return err
	case <-time.After(waitLimit):
		t.Fatal("Handle did not return")

		return nil
	}
}

// send delivers msg from userAna and returns what Handle returned.
func (r *rig) send(t *testing.T, msg bot.Message) error {
	t.Helper()

	msg.Sender = userAna
	if msg.PushName == "" {
		msg.PushName = "Ana"
	}

	r.client.Deliver(msg)

	return r.result(t)
}

func (r *rig) say(t *testing.T, text string) error {
	t.Helper()

	return r.send(t, bot.Message{Text: text})
}

// sendMedia delivers a message with an attachment whose bytes are raw.
func (r *rig) sendMedia(t *testing.T, kind bot.MediaKind, raw any) error {
	t.Helper()

	return r.send(t, bot.Message{Media: &bot.Media{Kind: kind, MIME: "application/octet-stream", Raw: raw}})
}

// seed puts userAna on node as a user who told the bot their name is Ana.
func (r *rig) seed(t *testing.T, node string, change ...func(*flow.State)) {
	t.Helper()

	err := r.store.Turn(t.Context(), userAna, func(state *flow.State) ([]flow.StoredMessage, error) {
		state.CurrentNode = node
		state.UserName = "Ana"

		for _, apply := range change {
			apply(state)
		}

		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// age makes userAna's last message older than a day.
func (r *rig) age(t *testing.T) {
	t.Helper()

	_, err := r.db.ExecContext(t.Context(), `UPDATE user_state SET last_updated = ? WHERE user_id = ?`,
		time.Now().Add(-25*time.Hour).UTC(), userAna)
	if err != nil {
		t.Fatal(err)
	}
}

// sent returns what the bot has sent since the last call.
func (r *rig) sent() []string {
	unread := r.client.Sent()[r.read:]
	texts := make([]string, 0, len(unread))

	for _, message := range unread {
		texts = append(texts, message.Text)
	}

	r.read = len(r.client.Sent())

	return texts
}

func (r *rig) state(t *testing.T) *flow.State {
	t.Helper()

	state, err := r.store.Load(t.Context(), userAna)
	if err != nil {
		t.Fatal(err)
	}

	return state
}

// exchange is one stored message reduced to what the tests compare.
type exchange struct {
	direction flow.Direction
	node      string
	content   string
}

func (r *rig) history(t *testing.T) []exchange {
	t.Helper()

	messages, err := r.store.History(t.Context(), userAna)
	if err != nil {
		t.Fatal(err)
	}

	history := make([]exchange, 0, len(messages))
	for _, message := range messages {
		history = append(history, exchange{message.Direction, message.NodeID, message.Content})
	}

	return history
}

// text is what the flow says on entering node, for a user called Ana.
func (r *rig) text(node, greeting string) string {
	return flow.Render(r.flow.Nodes[node].Message.Content, map[string]string{"name": "Ana", "greeting": greeting})
}

// logged returns the log records the app wrote.
func (r *rig) logged(t *testing.T) []map[string]any {
	t.Helper()

	var records []map[string]any

	for line := range bytes.Lines(r.logs.Bytes()) {
		var record map[string]any

		err := json.Unmarshal(line, &record)
		if err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}

		records = append(records, record)
	}

	return records
}

// tiny parses a flow of the given nodes, which get the help node beside them.
func tiny(t *testing.T, nodes string) *fsm.Flow {
	t.Helper()

	data := `{"start_node":"START","nodes":{` + nodes +
		`,"NEEDS_ASSISTANCE":{"message":{"type":"text","content":"a person will help"}}}}`

	parsed, err := fsm.Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}

	return parsed
}

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

func requireHistory(t *testing.T, got []exchange, want ...exchange) {
	t.Helper()

	if !slices.Equal(got, want) {
		t.Errorf("history = %+v, want %+v", got, want)
	}
}

func TestNewUserIsGreeted(t *testing.T) {
	t.Parallel()

	env := start(t)

	err := env.send(t, bot.Message{Text: "hola", PushName: "Ana Pérez"})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	greeting := env.text("GREETING_INTRO", welcome)
	requireSent(t, env.sent(), greeting)

	state := env.state(t)
	requireEqual(t, "CurrentNode", state.CurrentNode, "GREETING_INTRO")
	requireEqual(t, "UserName", state.UserName, "Ana Pérez")
	requireEqual(t, "RepromptCount", state.RepromptCount, 0)

	if time.Since(state.LastUpdated) > time.Minute {
		t.Errorf("LastUpdated = %v, want now", state.LastUpdated)
	}

	requireHistory(t, env.history(t),
		exchange{flow.Inbound, "GREETING_INTRO", "hola"},
		exchange{flow.Outbound, "GREETING_INTRO", greeting},
	)
}

func TestNewUsersFirstMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		wantNode string
		// answer is the node whose text follows the greeting, or "" when the
		// message answers nothing the greeting asked.
		answer string
		check  func(t *testing.T, state *flow.State)
	}{
		{"an option of the menu is answered", "1", "INTERESTED_IN_BEGINNER", "INTERESTED_IN_BEGINNER", func(t *testing.T, state *flow.State) {
			t.Helper()
			requireEqual(t, "CourseInterest", state.CourseInterest, "beginner")
		}},
		{"a request for a person is answered", "necesito ayuda", "NEEDS_ASSISTANCE", "NEEDS_ASSISTANCE", func(t *testing.T, state *flow.State) {
			t.Helper()

			if !state.RequiresHumanAgent {
				t.Error("RequiresHumanAgent = false, want the help node's action to have run")
			}
		}},
		{"anything else is only stored", "buenas tardes, tengo 21 años", "GREETING_INTRO", "", func(t *testing.T, state *flow.State) {
			t.Helper()
			requireEqual(t, "RepromptCount", state.RepromptCount, 0)
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			env := start(t)

			err := env.say(t, test.text)
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			greeting := env.text("GREETING_INTRO", welcome)
			want := []string{greeting}
			history := []exchange{
				{flow.Inbound, "GREETING_INTRO", test.text},
				{flow.Outbound, "GREETING_INTRO", greeting},
			}

			if test.answer != "" {
				answer := env.text(test.answer, welcome)
				want = append(want, answer)
				history = append(history, exchange{flow.Outbound, test.answer, answer})
			}

			requireSent(t, env.sent(), want...)
			requireHistory(t, env.history(t), history...)

			state := env.state(t)
			requireEqual(t, "CurrentNode", state.CurrentNode, test.wantNode)
			test.check(t, state)
		})
	}
}

func TestNameIsSaved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node string
		text string
		want string
	}{
		{"asked for at the prompt", "CHANGE_NAME_PROMPT", "Marta", "Marta"},
		{"introduced anywhere", "MAIN_MENU", "me llamo Laura", "Laura"},
		{"replacing the old one", "MAIN_MENU", "Mi nombre es Carla", "Carla"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			env := start(t)
			env.seed(t, test.node)

			err := env.say(t, test.text)
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			requireEqual(t, "UserName", env.state(t).UserName, test.want)
			requireEqual(t, "CurrentNode", env.state(t).CurrentNode, "NAME_CHANGE_CONFIRMED")
			requireSent(t, env.sent(), "Perfecto, "+test.want+". ¿Qué más necesitas? Escribe *menú* para ver las opciones.")
		})
	}
}

func TestInvalidNameIsAskedAgain(t *testing.T) {
	t.Parallel()

	env := start(t)
	env.seed(t, "CHANGE_NAME_PROMPT")

	err := env.say(t, "xyz")
	if err != nil {
		t.Fatalf("Handle() error = %v, want none: a wrong name is not a failure", err)
	}

	requireSent(t, env.sent(), invalidName)
	requireEqual(t, "CurrentNode", env.state(t).CurrentNode, "CHANGE_NAME_PROMPT")
	requireEqual(t, "UserName", env.state(t).UserName, "Ana")
	requireHistory(t, env.history(t),
		exchange{flow.Inbound, "CHANGE_NAME_PROMPT", "xyz"},
		exchange{flow.Outbound, "CHANGE_NAME_PROMPT", invalidName},
	)

	var warnings int

	for _, record := range env.logged(t) {
		if record["msg"] != "action failed" {
			continue
		}

		warnings++

		requireEqual[any](t, "level", record["level"], "WARN")
		requireEqual[any](t, "action", record["action"], "save_user_name")
	}

	requireEqual(t, "logged action failures", warnings, 1)
}

func TestUnsupportedMedia(t *testing.T) {
	t.Parallel()

	for _, kind := range []bot.MediaKind{bot.MediaAudio, bot.MediaSticker, bot.MediaVideo, bot.MediaDocument} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			env := start(t)
			env.seed(t, "MAIN_MENU")

			err := env.sendMedia(t, kind, []byte("data"))
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			requireSent(t, env.sent(), unsupportedMedia)
			requireEqual(t, "CurrentNode", env.state(t).CurrentNode, "MAIN_MENU")
			requireEqual(t, "RepromptCount", env.state(t).RepromptCount, 0)
			requireHistory(t, env.history(t),
				exchange{flow.Inbound, "MAIN_MENU", ""},
				exchange{flow.Outbound, "MAIN_MENU", unsupportedMedia},
			)
		})
	}
}

func TestMediaOfTheWrongKindNamesWhatTheNodeAsksFor(t *testing.T) {
	t.Parallel()

	env := start(t)
	env.seed(t, "ENROLLMENT_PROCESS")

	err := env.sendMedia(t, bot.MediaVideo, []byte("data"))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	want := "Parece que enviaste un tipo de archivo incorrecto. Por favor, asegúrate de enviar una *imagen* (foto) para que pueda procesarlo. Gracias 😊"
	requireSent(t, env.sent(), want)
	requireEqual(t, "CurrentNode", env.state(t).CurrentNode, "ENROLLMENT_PROCESS")
	requireEqual(t, "RepromptCount", env.state(t).RepromptCount, 1)
}

func TestNodeThatTakesAnyMediaAcceptsIt(t *testing.T) {
	t.Parallel()

	env := start(t)
	env.seed(t, "NEEDS_ASSISTANCE")

	err := env.sendMedia(t, bot.MediaAudio, []byte("data"))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	requireSent(t, env.sent(), env.text("ASSISTANCE_ATTACHMENT", welcome))
}

func TestFallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node string
		text string
		// want is the reply, or "" for silence.
		want func(env *rig) string
	}{
		{"a menu node asks its options again", "MAIN_MENU", "zzzz", func(env *rig) string {
			return flow.Render(menuFallback+env.flow.Nodes["MAIN_MENU"].Message.Content, map[string]string{"name": "Ana"})
		}},
		{"a node with its own message says it", "CONFIRM_ENROLLMENT_BEGINNER", "zzzz", func(env *rig) string {
			return env.flow.Nodes["CONFIRM_ENROLLMENT_BEGINNER"].FallbackMessage
		}},
		{"a node with no way out is silent", "URGENT_ASSISTANCE", "gracias", func(*rig) string { return "" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			env := start(t)
			env.seed(t, test.node)

			err := env.say(t, test.text)
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			want := test.want(env)
			history := []exchange{{flow.Inbound, test.node, test.text}}

			if want == "" {
				requireSent(t, env.sent())
			} else {
				requireSent(t, env.sent(), want)

				history = append(history, exchange{flow.Outbound, test.node, want})
			}

			requireEqual(t, "CurrentNode", env.state(t).CurrentNode, test.node)
			requireEqual(t, "RepromptCount", env.state(t).RepromptCount, 1)
			requireHistory(t, env.history(t), history...)
		})
	}
}

func TestThirdFallbackEscalatesToTheHelpNode(t *testing.T) {
	t.Parallel()

	env := start(t)
	env.seed(t, "MAIN_MENU")

	for count := 1; count <= 2; count++ {
		err := env.say(t, "zzzz")
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}

		state := env.state(t)
		requireEqual(t, "RepromptCount", state.RepromptCount, count)
		requireEqual(t, "RequiresHumanAgent", state.RequiresHumanAgent, false)
	}

	// Progress in between starts the count over.
	err := env.say(t, "precios")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	requireEqual(t, "RepromptCount after progress", env.state(t).RepromptCount, 0)
	env.seed(t, "MAIN_MENU")

	for range 2 {
		err = env.say(t, "zzzz")
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	}

	env.sent()

	err = env.say(t, "zzzz")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	state := env.state(t)
	requireEqual(t, "CurrentNode", state.CurrentNode, "NEEDS_ASSISTANCE")
	requireEqual(t, "RequiresHumanAgent", state.RequiresHumanAgent, true)
	requireEqual(t, "RepromptCount", state.RepromptCount, 0)
	requireSent(t, env.sent(), env.text("NEEDS_ASSISTANCE", welcomeBack))
}

func TestAVoucherImageIsSavedAndStored(t *testing.T) {
	t.Parallel()

	env := start(t)
	env.seed(t, "ENROLLMENT_PROCESS")

	err := env.send(t, photo("jpeg-bytes"))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	state := env.state(t)
	requireEqual(t, "CurrentNode", state.CurrentNode, "PAYMENT_CONFIRMED")

	if filepath.Dir(state.VoucherPath) != env.voucherDir {
		t.Fatalf("VoucherPath = %q, want a file in %s", state.VoucherPath, env.voucherDir)
	}

	data, err := os.ReadFile(state.VoucherPath)
	if err != nil {
		t.Fatal(err)
	}

	requireEqual(t, "voucher", string(data), "jpeg-bytes")

	thanks := env.text("PAYMENT_CONFIRMED", welcome)
	requireSent(t, env.sent(), thanks)
	requireHistory(t, env.history(t),
		exchange{flow.Inbound, "ENROLLMENT_PROCESS", ""},
		exchange{flow.Outbound, "PAYMENT_CONFIRMED", thanks},
	)
}

func TestAnythingButAnImageEscalatesInsteadOfSavingAVoucher(t *testing.T) {
	t.Parallel()

	receipt := tiny(t, `
		"START":{"message":{"type":"text","content":"send the receipt"},
			"transitions":[{"condition":{"type":"media"},"target":"DONE","action":"save_payment_voucher"}]},
		"DONE":{"message":{"type":"text","content":"thanks"}}`)
	env := start(t, withFlow(receipt))
	env.seed(t, "START")

	err := env.sendMedia(t, bot.MediaDocument, []byte("pdf-bytes"))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	state := env.state(t)
	requireEqual(t, "RequiresHumanAgent", state.RequiresHumanAgent, true)
	requireEqual(t, "VoucherPath", state.VoucherPath, "")
	requireSent(t, env.sent(), "thanks")

	_, err = os.Stat(env.voucherDir)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("voucher directory: Stat error = %v, want it not to exist", err)
	}
}

func TestFailedDownloadIsLoggedAndReturned(t *testing.T) {
	t.Parallel()

	env := start(t)
	env.seed(t, "ENROLLMENT_PROCESS")

	err := env.sendMedia(t, bot.MediaImage, "not bytes, so the fake cannot download it")
	if !errors.Is(err, fake.ErrForeignMedia) {
		t.Fatalf("Handle() error = %v, want the download error", err)
	}

	// The user is not left waiting: a person takes over and they are told so.
	requireSent(t, env.sent(), actionFailed)

	state := env.state(t)
	requireEqual(t, "CurrentNode", state.CurrentNode, "NEEDS_ASSISTANCE")
	requireEqual(t, "RequiresHumanAgent", state.RequiresHumanAgent, true)
	requireEqual(t, "VoucherPath", state.VoucherPath, "")
	requireHistory(t, env.history(t),
		exchange{flow.Inbound, "ENROLLMENT_PROCESS", ""},
		exchange{flow.Outbound, "NEEDS_ASSISTANCE", actionFailed},
	)

	var failures int

	for _, record := range env.logged(t) {
		if record["msg"] != "action failed" {
			continue
		}

		failures++

		requireEqual[any](t, "level", record["level"], "ERROR")
		requireEqual[any](t, "user", record["user"], string(userAna))
		requireEqual[any](t, "node", record["node"], "ENROLLMENT_PROCESS")
		requireEqual[any](t, "action", record["action"], "save_payment_voucher")
	}

	requireEqual(t, "action failures logged", failures, 1)
}

func TestVoucherIsRemovedWhenTheTurnCannotBeSaved(t *testing.T) {
	t.Parallel()

	env := start(t)
	env.seed(t, "ENROLLMENT_PROCESS")

	_, err := env.db.ExecContext(t.Context(), `
		CREATE TRIGGER refuse_replies BEFORE INSERT ON conversation_history
		WHEN NEW.direction = 'outbound'
		BEGIN SELECT RAISE(ABORT, 'disk full'); END`)
	if err != nil {
		t.Fatal(err)
	}

	err = env.send(t, photo("jpeg-bytes"))
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Handle() error = %v, want the failed save", err)
	}

	files, err := os.ReadDir(env.voucherDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 0 {
		t.Errorf("voucher directory holds %d files, want none: no state points to them", len(files))
	}

	requireSent(t, env.sent())
	requireEqual(t, "CurrentNode", env.state(t).CurrentNode, "ENROLLMENT_PROCESS")
	requireEqual(t, "VoucherPath", env.state(t).VoucherPath, "")
	requireHistory(t, env.history(t))
}

func TestTwoMessagesOfOneUserAreBothStored(t *testing.T) {
	t.Parallel()

	env := start(t)
	env.seed(t, "MAIN_MENU")

	env.client.Deliver(bot.Message{Sender: userAna, PushName: "Ana", Text: "precios"})
	env.client.Deliver(bot.Message{Sender: userAna, PushName: "Ana", Text: "horarios"})

	for range 2 {
		err := env.result(t)
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	}

	var inbound []string

	history := env.history(t)
	for _, message := range history {
		if message.direction == flow.Inbound {
			inbound = append(inbound, message.content)
		}
	}

	slices.Sort(inbound)

	if !slices.Equal(inbound, []string{"horarios", "precios"}) {
		t.Errorf("inbound messages = %q, want both", inbound)
	}

	requireEqual(t, "stored messages", len(history), 4)
	requireEqual(t, "replies sent", len(env.sent()), 2)
}

func TestSelectedCourseFillsTheTemplate(t *testing.T) {
	t.Parallel()

	env := start(t)
	env.seed(t, "CLUB_MISTERIO")

	err := env.say(t, "inscribirme")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	requireEqual(t, "SelectedCourseID", env.state(t).SelectedCourseID, "CLUB_MISTERIO")

	sent := env.sent()
	if len(sent) != 1 || !strings.Contains(sent[0], "*Club Misterio*") {
		t.Errorf("sent %q, want one reply naming the selected course", sent)
	}
}

func TestReturningUserIsWelcomedBackAfterADay(t *testing.T) {
	t.Parallel()

	env := start(t)

	for _, text := range []string{"hola", "precios"} {
		err := env.say(t, text)
		if err != nil {
			t.Fatalf("Handle(%q) error = %v", text, err)
		}
	}

	env.seed(t, "CONSULTED_PRICE", func(state *flow.State) { state.UserName = "Carla" })
	env.age(t)
	env.sent()

	err := env.say(t, "menu")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	// The conversation starts over, and the name the user gave is kept.
	state := env.state(t)
	requireEqual(t, "CurrentNode", state.CurrentNode, "MAIN_MENU")
	requireEqual(t, "UserName", state.UserName, "Carla")
	requireSent(t, env.sent(),
		env.text("GREETING_INTRO", welcomeBack),
		strings.ReplaceAll(env.text("MAIN_MENU", welcomeBack), "Ana", "Carla"),
	)
}

func TestUserOnANodeTheFlowNoLongerHasStartsOver(t *testing.T) {
	t.Parallel()

	env := start(t)
	env.seed(t, "REMOVED_NODE")

	err := env.say(t, "hola")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	requireEqual(t, "CurrentNode", env.state(t).CurrentNode, "GREETING_INTRO")
	requireSent(t, env.sent(), env.text("GREETING_INTRO", welcome))
}

func TestFallbackInANodeWithoutMessageSendsTheUserToTheStart(t *testing.T) {
	t.Parallel()

	silent := tiny(t, `
		"START":{"message":{"type":"text","content":"welcome"},
			"transitions":[{"condition":{"type":"exact","value":["go"]},"target":"BLANK"}]},
		"BLANK":{"message":{"type":"text","content":""},
			"transitions":[{"condition":{"type":"exact","value":["back"]},"target":"START"}]}`)
	env := start(t, withFlow(silent))
	env.seed(t, "BLANK")

	err := env.say(t, "what")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	requireSent(t, env.sent(), "welcome")
	requireEqual(t, "CurrentNode", env.state(t).CurrentNode, "START")
}

func TestStartNodeActionRunsForANewUser(t *testing.T) {
	t.Parallel()

	escalating := tiny(t, `"START":{"message":{"type":"text","content":"welcome"},"action":"escalate_to_human_agent"}`)
	env := start(t, withFlow(escalating))

	err := env.say(t, "hola")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	requireEqual(t, "RequiresHumanAgent", env.state(t).RequiresHumanAgent, true)
	requireSent(t, env.sent(), "welcome")
}

func TestFailingStartNodeActionEscalatesANewUser(t *testing.T) {
	t.Parallel()

	receipt := tiny(t, `"START":{"message":{"type":"text","content":"welcome"},"action":"save_payment_voucher"}`)
	env := start(t, withFlow(receipt))

	err := env.sendMedia(t, bot.MediaImage, "not bytes, so the fake cannot download it")
	if !errors.Is(err, fake.ErrForeignMedia) {
		t.Fatalf("Handle() error = %v, want the download error", err)
	}

	requireSent(t, env.sent(), actionFailed)

	state := env.state(t)
	requireEqual(t, "CurrentNode", state.CurrentNode, "NEEDS_ASSISTANCE")
	requireEqual(t, "RequiresHumanAgent", state.RequiresHumanAgent, true)
	requireHistory(t, env.history(t),
		exchange{flow.Inbound, "START", ""},
		exchange{flow.Outbound, "NEEDS_ASSISTANCE", actionFailed},
	)

	var failures int

	for _, record := range env.logged(t) {
		if record["msg"] != "action failed" || record["action"] != "save_payment_voucher" {
			continue
		}

		failures++

		requireEqual[any](t, "node", record["node"], "START")
	}

	requireEqual(t, "logged failures of the start action", failures, 1)
}

func TestFailedSendIsReturnedAndTheTurnStaysStored(t *testing.T) {
	t.Parallel()

	env := start(t)
	env.seed(t, "MAIN_MENU")

	env.client.FailSends(errors.ErrUnsupported)

	err := env.say(t, "precios")
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Handle() error = %v, want the send error", err)
	}

	requireEqual(t, "CurrentNode", env.state(t).CurrentNode, "CONSULTED_PRICE")
	requireEqual(t, "stored messages", len(env.history(t)), 2)
}

func TestCancelledContextDuringTheTypingDelaySendsNothing(t *testing.T) {
	t.Parallel()

	env := start(t, withDelay(time.Hour))
	env.seed(t, "MAIN_MENU")

	env.client.Deliver(bot.Message{Sender: userAna, PushName: "Ana", Text: "precios"})

	// The turn is stored before the delay starts.
	select {
	case <-env.handled:
	case <-time.After(waitLimit):
		t.Fatal("the turn was not stored")
	}

	env.stop()

	err := env.result(t)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Handle() error = %v, want the cancellation", err)
	}

	requireSent(t, env.sent())
	requireEqual(t, "CurrentNode", env.state(t).CurrentNode, "CONSULTED_PRICE")
	requireEqual(t, "stored messages", len(env.history(t)), 2)
}

func TestTypingDelayWaitsBeforeEachReply(t *testing.T) {
	t.Parallel()

	const delay = 50 * time.Millisecond

	env := start(t, withDelay(delay))
	begin := time.Now()

	err := env.say(t, "1")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if elapsed := time.Since(begin); elapsed < 2*delay {
		t.Errorf("greeting and answer took %v, want at least a delay before each: %v", elapsed, 2*delay)
	}

	requireEqual(t, "replies sent", len(env.sent()), 2)
}

func TestNewRejectsAnUnknownAction(t *testing.T) {
	t.Parallel()

	typo := tiny(t, `
		"START":{"message":{"type":"text","content":"welcome"},
			"transitions":[{"condition":{"type":"any_text"},"target":"START","action":"save_paymnt_voucher"}]}`)

	app, err := flow.New(typo, newStore(t), flow.NewActions(t.TempDir()), flow.Config{}, nil)
	if !errors.Is(err, flow.ErrUnknownAction) || !strings.Contains(err.Error(), "save_paymnt_voucher") {
		t.Errorf("New() error = %v, want ErrUnknownAction naming the action", err)
	}

	if app != nil {
		t.Errorf("New() = %v, want no app for a flow that cannot run", app)
	}
}

func TestNewAcceptsTheExampleFlow(t *testing.T) {
	t.Parallel()

	example, err := fsm.Example()
	if err != nil {
		t.Fatal(err)
	}

	_, err = flow.New(example, newStore(t), flow.NewActions(t.TempDir()), flow.Config{}, nil)
	if err != nil {
		t.Errorf("New() error = %v", err)
	}
}
