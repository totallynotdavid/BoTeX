//nolint:goconst,misspell // Node names and the example flow's Spanish repeat across scripts; literals keep each script readable.
package main_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	flowcmd "github.com/totallynotdavid/botkit/cmd/flow"
	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/flow"
	"github.com/totallynotdavid/botkit/internal/flow/fsm"
	"github.com/totallynotdavid/botkit/internal/whatsapp/fake"
)

// What the example flow says, written out so that a change to its wording
// shows up here.
const (
	greetingIntro = greeting + " Organizamos clubes de lectura todo el año.\n\n1️⃣ Club para lectores nuevos\n2️⃣ Clubs por género\n\nTambién puedes preguntarme por *precios* u *horarios*."

	mainMenuAna   = "¿En qué te puedo ayudar, Ana?\n\n1️⃣ Club para lectores nuevos\n2️⃣ Clubs por género\n\nO escribe *precios*, *horarios* o *inscribirme*."
	mainMenuAmigx = "¿En qué te puedo ayudar, amigx?\n\n1️⃣ Club para lectores nuevos\n2️⃣ Clubs por género\n\nO escribe *precios*, *horarios* o *inscribirme*."
	menuFallback  = "No entendí tu respuesta 😊 Por favor, revisa las opciones:\n\n"

	beginnerClub     = "El *Club Primeras Páginas* es para quien quiere retomar la lectura: un libro corto al mes, sin tareas.\n\nEscribe *inscribirme* para apuntarte o *menú* para volver."
	genres           = "Estos son nuestros géneros:\n\n1️⃣ Ficción\n2️⃣ Poesía\n\nO escribe *catálogo* para ver todos los clubs."
	fictionClubs     = "Clubs de ficción:\n\n• *Misterio*: novela policial\n• *Histórica*: novela histórica\n\nEscribe el nombre del club o *volver*."
	chooseClub       = "Elige un club: *misterio*, *histórica* o *poesía*."
	historicalClub   = "*Club Histórica*: novela histórica, los jueves a las 7 p. m., S/ 40 al mes.\n\nEscribe *inscribirme* o *menú*."
	poetryClubs      = "Club de poesía:\n\n• *Poesía*: un poeta distinto cada mes\n\nEscribe *poesía* o *volver*."
	catalog          = "Todos los clubs: *misterio*, *histórica* y *poesía*. Escribe el nombre del que te interese o *menú*."
	mysteryClub      = "*Club Misterio*: novela policial, los martes a las 7 p. m., S/ 40 al mes.\n\nEscribe *inscribirme* o *menú*."
	poetryClub       = "*Club Poesía*: un poeta distinto cada mes, los sábados a las 11 a. m., S/ 30 al mes.\n\nEscribe *inscribirme* o *menú*."
	prices           = "Los clubs cuestan entre S/ 30 y S/ 40 al mes. La inscripción es gratis.\n\nEscribe *inscribirme*, *horarios* o *menú*."
	schedule         = "Nos reunimos martes y jueves a las 7 p. m. y sábados a las 11 a. m.\n\nEscribe *inscribirme*, *precios* o *menú*."
	whichClub        = "¿A qué club quieres inscribirte?\n\n1️⃣ Club para lectores nuevos\n2️⃣ Un club por género"
	confirmBeginner  = "Vas a inscribirte al *Club Primeras Páginas*. ¿Confirmas? Responde *sí* o *cancelar*."
	confirmBeginner2 = "Responde *sí* para confirmar tu inscripción o *cancelar* para volver."
	confirmMystery   = "Vas a inscribirte al club *Club Misterio*. ¿Confirmas? Responde *sí* o *cancelar*."
	confirmMystery2  = "Responde *sí* para confirmar tu inscripción a Club Misterio o *cancelar* para volver."
	cancelled        = "Listo, no te inscribimos. Si quieres ver otra opción escribe *1*, *2* o *menú*."
	payment          = "Para reservar tu cupo, paga a la cuenta de ejemplo 000-0000000-0-00 de *Librería Página Nueva* y envíanos una *foto* del comprobante.\n\nSi aún no pagaste, escribe *listo* cuando termines."
	waiting          = "Envíame la *foto* del comprobante cuando la tengas 📸"
	received         = "¡Gracias, Ana! Recibimos tu comprobante. Una persona del equipo confirmará tu cupo en las próximas horas."
	receivedAnother  = "Guardamos también esta imagen. Escribe *menú* si necesitas algo más."

	askName       = "¿Cómo quieres que te llame? Escribe solo tu nombre."
	askNameAgain  = "Escribe solo tu nombre, por favor. No entiendo los archivos ni los mensajes vacíos."
	invalidName   = "No pude reconocer eso como un nombre. ¿Podrías intentarlo de nuevo, por favor?"
	nameSaved     = "Perfecto, %s. ¿Qué más necesitas? Escribe *menú* para ver las opciones."
	nameRemoved   = "Listo, ya no uso tu nombre. Escribe *menú* para ver las opciones."
	assistance    = "Avisé a una persona del equipo, te escribirá en breve. Si es *urgente*, dímelo. Puedes enviarme una foto o un archivo con los detalles."
	urgent        = "Marcado como urgente. Te atenderemos lo antes posible."
	attachment    = "Recibimos tu archivo y se lo pasé a la persona que te atenderá."
	closed        = "¡Gracias por escribirnos! Cuando quieras volver, envíame cualquier mensaje 📚"
	unsupportedIn = "Lo siento, no puedo procesar ese tipo de mensaje. Por favor, envíame un mensaje de texto. 😊"
)

// saved is the reply to a name the bot has taken.
func saved(name string) string {
	return fmt.Sprintf(nameSaved, name)
}

// turn is one message the user sends and the replies the bot gives, in order.
type turn struct {
	in  bot.Message
	out []string
}

func say(text string, out ...string) turn {
	return turn{bot.Message{Sender: alice, PushName: "Ana", Text: text}, out}
}

func photo(data string, out ...string) turn {
	media := &bot.Media{Kind: bot.MediaImage, MIME: "image/jpeg", Raw: []byte(data)}

	return turn{bot.Message{Sender: alice, PushName: "Ana", Media: media}, out}
}

func voiceNote(out ...string) turn {
	media := &bot.Media{Kind: bot.MediaAudio, MIME: "audio/ogg", Raw: []byte("ogg")}

	return turn{bot.Message{Sender: alice, PushName: "Ana", Media: media}, out}
}

// ending is what the bot remembers about the user when the script is over.
type ending struct {
	node     string
	name     string
	interest string
	course   string
	price    bool
	human    bool
}

type script struct {
	name string
	// flowFile is a FLOW_FILE to run instead of the example flow.
	flowFile string
	turns    []turn
	end      ending
	// vouchers are the contents of the vouchers the bot saved, sorted: the file
	// names end in a random number, so the order they were saved in is not kept.
	vouchers []string
}

func conversations() []script {
	return []script{
		{
			name: "a reader picks a genre club, confirms and pays with two photos",
			turns: []turn{
				say("hola", greetingIntro),
				say("2", genres),
				say("1", fictionClubs),
				say("Misteryo", mysteryClub),
				say("inscribirme", confirmMystery),
				say("quizás", confirmMystery2),
				say("Sí", payment),
				say("listo", waiting),
				photo("first receipt", received),
				photo("second receipt", receivedAnother),
				say("menú", mainMenuAna),
			},
			end:      ending{node: "MAIN_MENU", name: "Ana", interest: "advanced", course: "CLUB_MISTERIO"},
			vouchers: []string{"first receipt", "second receipt"},
		},
		{
			name: "a reader names a club, pays by choosing poetry, and asks for help at the payment step",
			turns: []turn{
				say("hola", greetingIntro),
				say("inscribirme", whichClub),
				say("2", chooseClub),
				say("historia", historicalClub),
				say("menú", mainMenuAna),
				say("2", genres),
				say("2", poetryClubs),
				say("volver", genres),
				say("2", poetryClubs),
				say("poesía", poetryClub),
				say("inscribirme", "Vas a inscribirte al club *Club Poesía*. ¿Confirmas? Responde *sí* o *cancelar*."),
				say("confirmo", payment),
				say("ayuda", assistance),
			},
			end: ending{node: "NEEDS_ASSISTANCE", name: "Ana", interest: "advanced", course: "CLUB_POESIA", human: true},
		},
		{
			name: "a newcomer asks the prices, cancels, and browses the catalog",
			turns: []turn{
				say("hola", greetingIntro),
				say("precios", prices),
				say("y los horarios?", schedule),
				say("inscribirme", whichClub),
				say("1", confirmBeginner),
				say("gracias", confirmBeginner2),
				say("cancelar", cancelled),
				say("1", beginnerClub),
				say("inicio", mainMenuAna),
				say("2", genres),
				say("catálogo", catalog),
				say("poemas", poetryClub),
				say("inicio", mainMenuAna),
			},
			end: ending{
				node: "MAIN_MENU", name: "Ana", interest: "advanced", course: "CONFIRM_ENROLLMENT_BEGINNER", price: true,
			},
		},
		{
			name: "a user renames themselves, gets it wrong, and has the name removed",
			turns: []turn{
				say("hola", greetingIntro),
				say("nuevos", beginnerClub),
				say("me llamo lucía gómez", saved("Lucía")),
				say("cambiar nombre", askName),
				photo("not a name", askNameAgain),
				say("12", invalidName),
				say("Marta", saved("Marta")),
				say("quita mi nombre", nameRemoved),
				say("menú", mainMenuAmigx),
				say("mi nombre es Rosa", saved("Rosa")),
			},
			end: ending{node: "NAME_CHANGE_CONFIRMED", name: "Rosa", interest: "beginner"},
		},
		{
			name: "a user asks for a person, sends a file, and is not heard again",
			turns: []turn{
				say("hola", greetingIntro),
				say("necesito ayuda", assistance),
				photo("a screenshot", attachment),
				say("menu", mainMenuAna),
				voiceNote(unsupportedIn),
				say("2", genres),
				say("1", fictionClubs),
				say("poemas", poetryClub),
				say("ayuda", assistance),
				say("urgente", urgent),
				say("gracias"),
				say("gracias"),
				say("gracias", assistance),
			},
			end: ending{node: "NEEDS_ASSISTANCE", name: "Ana", interest: "advanced", human: true},
		},
		{
			name: "a user says goodbye, returns, and is sent to a person after three wrong answers",
			turns: []turn{
				say("hola", greetingIntro),
				say("chau", closed),
				say("buenas tardes", mainMenuAna),
				say("zzzz", menuFallback+mainMenuAna),
				say("qqqq", menuFallback+mainMenuAna),
				say("xxxx", assistance),
				say("hasta luego", closed),
			},
			end: ending{node: "CONVERSATION_CLOSED", name: "Ana", human: true},
		},
		{
			name: "a flow file replaces the example flow",
			flowFile: `{"start_node":"HELLO","nodes":{
			"HELLO":{"message":{"type":"text","content":"Hello {{name}}"},"transitions":[{"condition":{"type":"exact","value":["1"]},"target":"ONE"}]},
			"ONE":{"message":{"type":"text","content":"You chose one"}},
			"NEEDS_ASSISTANCE":{"message":{"type":"text","content":"a person will help"}}}}`,
			turns: []turn{
				say("hi", "Hello Ana"),
				say("1", "You chose one"),
				say("1"),
			},
			end: ending{node: "ONE", name: "Ana"},
		},
	}
}

// TestConversations plays each script alone, so one can be run by its name.
func TestConversations(t *testing.T) {
	t.Parallel()

	for _, test := range conversations() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			test.play(t)
		})
	}
}

// The scripts together must enter every node of the example flow, so each
// condition, action and shared list of transitions has been used through the
// real wiring. The name must not contain TestConversations, or running one
// script by name would run this test too.
func TestScriptsEnterEveryExampleNode(t *testing.T) {
	var (
		lock    sync.Mutex
		entered = map[string]bool{}
	)

	// The group's subtests all finish before t.Run returns.
	t.Run("scripts", func(t *testing.T) {
		for _, test := range conversations() {
			if test.flowFile != "" {
				continue
			}

			t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				nodes := test.play(t)

				lock.Lock()
				defer lock.Unlock()

				for _, node := range nodes {
					entered[node] = true
				}
			})
		}
	})

	example, err := fsm.Example()
	if err != nil {
		t.Fatal(err)
	}

	for node := range example.Nodes {
		if !entered[node] {
			t.Errorf("no script enters node %s of the example flow", node)
		}
	}
}

// play runs the script against a bot with a real database and returns the
// nodes the bot stored messages in.
func (s script) play(t *testing.T) []string {
	t.Helper()

	vars := map[string]string{}

	if s.flowFile != "" {
		path := filepath.Join(t.TempDir(), "flow.json")

		err := os.WriteFile(path, []byte(s.flowFile), 0o600)
		if err != nil {
			t.Fatal(err)
		}

		vars[flow.KeyFile] = path
	}

	env := environment(t, vars)
	ctx, stop := context.WithCancel(t.Context())

	defer stop()

	client := fake.New()
	databases := make(chan *sql.DB, 1)
	done := make(chan error, 1)

	go func() {
		open := func(_ context.Context, database *sql.DB, _ *slog.Logger) (bot.Client, error) {
			databases <- database

			return client, nil
		}

		done <- flowcmd.Run(ctx, env, slog.New(slog.DiscardHandler), open)
	}()

	err := client.WaitConnected(ctx)
	if err != nil {
		t.Fatal(err)
	}

	database := <-databases

	var replied int

	for index := range s.turns {
		step := &s.turns[index]
		number := index + 1

		client.Deliver(step.in)

		// A turn stores its messages before it sends the replies. Once the
		// stored replies are the expected ones, the sends are the only thing
		// left, and a turn that says nothing is over.
		wantReplies := replied + len(step.out)

		waitFor(t, func() bool {
			return storedFrom(t, database, flow.Inbound) == number && storedFrom(t, database, flow.Outbound) == wantReplies
		})
		waitFor(t, func() bool { return len(client.Sent()) >= wantReplies })

		sent := client.Sent()[replied:]
		replied += len(sent)

		got := make([]string, 0, len(sent))
		for _, reply := range sent {
			if reply.To != alice {
				t.Errorf("turn %d: reply went to %s, want %s", number, reply.To, alice)
			}

			got = append(got, reply.Text)
		}

		if !slices.Equal(got, step.out) {
			t.Fatalf("turn %d, after %q:\nreplies = %q\nwant      %q", number, step.in.Text, got, step.out)
		}
	}

	// The database is closed when the bot stops, so read it first.
	s.checkEnding(t, database, env.String(flow.KeyVoucherDir, ""))

	nodes := nodesStored(t, database)

	stop()

	err = stopped(t, done)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}

	return nodes
}

func (s script) checkEnding(t *testing.T, database *sql.DB, voucherDir string) {
	t.Helper()

	var got ending

	err := database.QueryRowContext(t.Context(), `
		SELECT current_node, user_name, course_interest, selected_course_id, consulted_price, requires_human_agent
		FROM user_state WHERE user_id = ?`, alice).
		Scan(&got.node, &got.name, &got.interest, &got.course, &got.price, &got.human)
	if err != nil {
		t.Fatal(err)
	}

	if got != s.end {
		t.Errorf("the bot remembers\n%+v\nwant\n%+v", got, s.end)
	}

	vouchers := vouchersIn(t, voucherDir)
	if !slices.Equal(vouchers, s.vouchers) {
		t.Errorf("the bot saved vouchers %q, want %q", vouchers, s.vouchers)
	}
}

// vouchersIn returns the contents of the files in dir, sorted.
func vouchersIn(t *testing.T, dir string) []string {
	t.Helper()

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	vouchers := make([]string, 0, len(files))

	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(dir, file.Name())) //nolint:gosec // the directory is the test's own.
		if err != nil {
			t.Fatal(err)
		}

		vouchers = append(vouchers, string(data))
	}

	slices.Sort(vouchers)

	return vouchers
}

func storedFrom(t *testing.T, database *sql.DB, direction flow.Direction) int {
	t.Helper()

	var count int

	err := database.QueryRowContext(t.Context(),
		`SELECT COUNT(id) FROM conversation_history WHERE user_id = ? AND direction = ?`, alice, direction).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	return count
}

func nodesStored(t *testing.T, database *sql.DB) []string {
	t.Helper()

	rows, err := database.QueryContext(t.Context(), `SELECT DISTINCT node_id FROM conversation_history`)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		err := rows.Close()
		if err != nil {
			t.Error(err)
		}
	}()

	var nodes []string

	for rows.Next() {
		var node string

		err := rows.Scan(&node)
		if err != nil {
			t.Fatal(err)
		}

		nodes = append(nodes, node)
	}

	err = rows.Err()
	if err != nil {
		t.Fatal(err)
	}

	return nodes
}
