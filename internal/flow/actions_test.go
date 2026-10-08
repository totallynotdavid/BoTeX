//nolint:goconst // Node names and texts repeat across table rows; literals keep each row readable.
package flow_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/flow"
	"github.com/totallynotdavid/botkit/internal/flow/fsm"
	"github.com/totallynotdavid/botkit/internal/whatsapp/fake"
)

// photo is an image message the fake client downloads as data.
func photo(data string) bot.Message {
	return bot.Message{PushName: "Ana", Media: &bot.Media{Kind: bot.MediaImage, MIME: "image/jpeg", Raw: []byte(data)}}
}

func TestApplyStateActions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action string
		before flow.State
		origin string
		want   flow.State
	}{
		{"empty action", "", flow.State{UserName: "Ana"}, "N", flow.State{UserName: "Ana"}},
		{"a new lead changes no state", "create_new_lead", flow.State{UserName: "Ana"}, "N", flow.State{UserName: "Ana"}},
		{"beginner interest", "update_lead_interest_beginner", flow.State{}, "N", flow.State{CourseInterest: "beginner"}},
		{"advanced interest replaces beginner", "update_lead_interest_advanced", flow.State{CourseInterest: "beginner"}, "N", flow.State{CourseInterest: "advanced"}},
		{"consulted price", "update_lead_consulted_price", flow.State{}, "N", flow.State{ConsultedPrice: true}},
		{"escalation", "escalate_to_human_agent", flow.State{}, "N", flow.State{RequiresHumanAgent: true}},
		{"clear the name", "clear_user_name", flow.State{UserName: "Ana"}, "N", flow.State{}},
		{"the selected course is the originating node", "set_selected_course", flow.State{}, "COURSE_B", flow.State{SelectedCourseID: "COURSE_B"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			state := test.before
			state.UserID = userAna
			test.want.UserID = userAna

			err := flow.NewActions(t.TempDir()).Apply(t.Context(), fake.New(), test.action, &state, bot.Message{}, test.origin)
			if err != nil {
				t.Fatalf("Apply(%q) error = %v", test.action, err)
			}

			if state != test.want {
				t.Errorf("state after %q = %+v, want %+v", test.action, state, test.want)
			}
		})
	}
}

func TestApplyUnknownAction(t *testing.T) {
	t.Parallel()

	state := &flow.State{UserName: "Ana"}

	err := flow.NewActions(t.TempDir()).Apply(t.Context(), fake.New(), "launch_rocket", state, bot.Message{}, "N")
	if !errors.Is(err, flow.ErrUnknownAction) || !strings.Contains(err.Error(), "launch_rocket") {
		t.Errorf("Apply() error = %v, want ErrUnknownAction naming the action", err)
	}

	if *state != (flow.State{UserName: "Ana"}) {
		t.Errorf("state = %+v, want it unchanged", state)
	}
}

func TestApplySaveUserName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		existing string
		want     string
		wantErr  error
	}{
		{"plain name", "Ana", "", "Ana", nil},
		{"surrounding whitespace", "  Ana  ", "", "Ana", nil},
		{"a full name is stored whole", "Ana María López", "", "Ana María López", nil},
		{"mi nombre es", "mi nombre es Ana", "", "Ana", nil},
		{"me llamo", "Me llamo Laura", "", "Laura", nil},
		{"llámame", "llámame Pepe", "", "Pepe", nil},
		{"the introduction ignores case", "MI NOMBRE ES Carla", "", "Carla", nil},
		{"the introduction is a word of its own", "me llamotas", "", "me llamotas", nil},
		{"an introduction in the middle stays", "hola, me llamo Ana", "", "hola, me llamo Ana", nil},
		{"empty keeps the old name", "", "Old", "Old", nil},
		{"blank keeps the old name", "   ", "Old", "Old", nil},
		{"an introduction alone keeps the old name", "mi nombre es", "Old", "Old", nil},
		{"an introduction and spaces keeps the old name", "me llamo   ", "Old", "Old", nil},
		{"no vowels", "xyz", "Old", "Old", flow.ErrInvalidName},
		{"too short", "Al", "Old", "Old", flow.ErrInvalidName},
		{"too many words", "uno dos tres cuatro cinco", "Old", "Old", flow.ErrInvalidName},
		{"an invalid name after the introduction", "me llamo xyz", "Old", "Old", flow.ErrInvalidName},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			state := &flow.State{UserID: userAna, UserName: test.existing}

			err := flow.NewActions(t.TempDir()).Apply(t.Context(), fake.New(), "save_user_name", state, bot.Message{Text: test.input}, "N")
			if !errors.Is(err, test.wantErr) {
				t.Errorf("Apply() error = %v, want %v", err, test.wantErr)
			}

			if state.UserName != test.want {
				t.Errorf("UserName = %q, want %q", state.UserName, test.want)
			}
		})
	}
}

func TestApplySavePaymentVoucherStoresTheImage(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "nested", "vouchers")
	state := &flow.State{UserID: userAna}
	msg := photo("jpeg-bytes")
	msg.PushName = "Ana Pérez"

	err := flow.NewActions(dir).Apply(t.Context(), fake.New(), "save_payment_voucher", state, msg, "PAYMENT")
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if state.RequiresHumanAgent {
		t.Error("RequiresHumanAgent = true, want a saved voucher not to escalate")
	}

	if filepath.Dir(state.VoucherPath) != dir {
		t.Errorf("VoucherPath = %q, want a file in %q", state.VoucherPath, dir)
	}

	// {phone}_{name}_{unix seconds}_{random}.jpeg
	rest, ok := strings.CutPrefix(filepath.Base(state.VoucherPath), phoneAna+"_Ana_Prez_")
	if !ok || !regexp.MustCompile(`^\d{10}_\d+\.jpeg$`).MatchString(rest) {
		t.Errorf("voucher file name = %q, want {phone}_Ana_Prez_{unix seconds}_{random}.jpeg", filepath.Base(state.VoucherPath))
	}

	saved, err := os.ReadFile(filepath.Clean(state.VoucherPath))
	if err != nil || string(saved) != "jpeg-bytes" {
		t.Errorf("saved file = %q, %v; want the downloaded bytes", saved, err)
	}

	assertMode(t, state.VoucherPath, 0o600)
	assertMode(t, dir, 0o755)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode of %s = %o, want %o", path, got, want)
	}
}

func TestApplySavePaymentVoucherKeepsEveryVoucherOfOneUser(t *testing.T) {
	t.Parallel()

	const vouchers = 25

	actions := flow.NewActions(filepath.Join(t.TempDir(), "vouchers"))
	saved := make(map[string]string, vouchers)

	for seq := range vouchers {
		want := "voucher-" + strconv.Itoa(seq)
		state := &flow.State{UserID: userAna}

		err := actions.Apply(t.Context(), fake.New(), "save_payment_voucher", state, photo(want), "N")
		if err != nil {
			t.Fatalf("voucher %d: Apply() error = %v", seq, err)
		}

		if previous, taken := saved[state.VoucherPath]; taken {
			t.Fatalf("voucher %d was saved over %s at %s", seq, previous, state.VoucherPath)
		}

		saved[state.VoucherPath] = want
	}

	for path, want := range saved {
		got, err := os.ReadFile(filepath.Clean(path))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", path, got, err, want)
		}
	}
}

func TestApplySavePaymentVoucherFileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		user       bot.JID
		pushName   string
		wantPrefix string
	}{
		{"the phone is what precedes the @", "51999@s.whatsapp.net", "Ana", "51999_Ana_"},
		{"a user without an @ is used whole", "51999", "Ana", "51999_Ana_"},
		{"spaces become underscores", "1@s.whatsapp.net", "Ana Maria", "1_Ana_Maria_"},
		{"hyphens and underscores stay", "1@s.whatsapp.net", "Ana-Maria_2", "1_Ana-Maria_2_"},
		{"accented letters are dropped", "1@s.whatsapp.net", "María José", "1_Mara_Jos_"},
		{"punctuation alone falls back to user", "1@s.whatsapp.net", "!!!", "1_user_"},
		{"symbols split by a space fall back to user", "1@s.whatsapp.net", "!!! ???", "1_user_"},
		{"spaces around the name leave no underscores", "1@s.whatsapp.net", " Ana ", "1_Ana_"},
		{"an empty name falls back to user", "1@s.whatsapp.net", "", "1_user_"},
		{"path separators are dropped", "1@s.whatsapp.net", "../etc", "1_etc_"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			state := &flow.State{UserID: test.user}
			msg := photo("x")
			msg.PushName = test.pushName

			err := flow.NewActions(filepath.Join(t.TempDir(), "v")).Apply(t.Context(), fake.New(), "save_payment_voucher", state, msg, "N")
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}

			base := filepath.Base(state.VoucherPath)
			if !strings.HasPrefix(base, test.wantPrefix) || !strings.HasSuffix(base, ".jpeg") {
				t.Errorf("file name = %q, want prefix %q and suffix .jpeg", base, test.wantPrefix)
			}
		})
	}
}

func TestApplySavePaymentVoucherEscalatesWithoutAnImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  bot.Message
	}{
		{"a text message", bot.Message{Text: "here you go"}},
		{"a video", bot.Message{Media: &bot.Media{Kind: bot.MediaVideo, Raw: []byte("v")}}},
		{"a document", bot.Message{Media: &bot.Media{Kind: bot.MediaDocument, MIME: "image/png", Raw: []byte("d")}}},
		{"a sticker", bot.Message{Media: &bot.Media{Kind: bot.MediaSticker, Raw: []byte("s")}}},
		{"an empty message", bot.Message{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "vouchers")
			state := &flow.State{UserID: userAna}

			err := flow.NewActions(dir).Apply(t.Context(), fake.New(), "save_payment_voucher", state, test.msg, "N")
			if err != nil {
				t.Fatalf("Apply() error = %v, want none: the user moves on and a person takes over", err)
			}

			if !state.RequiresHumanAgent || state.VoucherPath != "" {
				t.Errorf("state = %+v, want an escalation and no voucher", state)
			}

			_, err = os.Stat(dir)
			if !os.IsNotExist(err) {
				t.Errorf("voucher directory: Stat error = %v, want it not created", err)
			}
		})
	}
}

func TestApplySavePaymentVoucherFailures(t *testing.T) {
	t.Parallel()

	blocker := filepath.Join(t.TempDir(), "file")

	err := os.WriteFile(blocker, nil, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		dir     string
		msg     bot.Message
		wantErr string
		// wantIs is a sentinel the error must wrap, when there is one.
		wantIs error
	}{
		{
			name:    "the download fails",
			dir:     t.TempDir(),
			msg:     bot.Message{Media: &bot.Media{Kind: bot.MediaImage, Raw: "not bytes"}},
			wantErr: "download voucher",
			wantIs:  fake.ErrForeignMedia,
		},
		{
			name:    "the directory cannot be created",
			dir:     filepath.Join(blocker, "vouchers"),
			msg:     photo("x"),
			wantErr: "create voucher directory",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			state := &flow.State{UserID: userAna}

			err := flow.NewActions(test.dir).Apply(t.Context(), fake.New(), "save_payment_voucher", state, test.msg, "N")
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Apply() error = %v, want one mentioning %q", err, test.wantErr)
			}

			if test.wantIs != nil && !errors.Is(err, test.wantIs) {
				t.Errorf("Apply() error = %v, want it to wrap %v", err, test.wantIs)
			}

			if state.VoucherPath != "" || state.RequiresHumanAgent {
				t.Errorf("state = %+v, want it unchanged", state)
			}
		})
	}
}

func TestApplySavePaymentVoucherFileFailure(t *testing.T) {
	t.Parallel()

	// Permission bits do not stop root, so the failure cannot be provoked there.
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}

	dir := filepath.Join(t.TempDir(), "vouchers")

	err := os.Mkdir(dir, 0o500)
	if err != nil {
		t.Fatal(err)
	}

	state := &flow.State{UserID: userAna}

	err = flow.NewActions(dir).Apply(t.Context(), fake.New(), "save_payment_voucher", state, photo("x"), "N")
	if err == nil || !strings.Contains(err.Error(), "save voucher file") {
		t.Errorf("Apply() error = %v, want a file error", err)
	}

	if state.VoucherPath != "" {
		t.Errorf("VoucherPath = %q, want it unset", state.VoucherPath)
	}
}

func TestCheckAgreesWithApply(t *testing.T) {
	t.Parallel()

	actions := []string{
		"launch_rocket",
		"create_new_lead", "clear_user_name", "save_user_name", "set_selected_course",
		"save_payment_voucher", "update_lead_interest_beginner", "update_lead_interest_advanced",
		"update_lead_consulted_price", "escalate_to_human_agent",
	}

	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			t.Parallel()

			applied := flow.NewActions(t.TempDir()).Apply(t.Context(), fake.New(), action, &flow.State{}, photo("x"), "N")
			applyKnows := !errors.Is(applied, flow.ErrUnknownAction)

			parsed, err := fsm.Parse([]byte(`{"start_node":"A","nodes":{"A":` + node(action) + `,"NEEDS_ASSISTANCE":` + node("") + `}}`))
			if err != nil {
				t.Fatal(err)
			}

			checkKnows := flow.NewActions(t.TempDir()).Check(parsed) == nil
			if applyKnows != checkKnows || applyKnows != (action != "launch_rocket") {
				t.Errorf("Apply knows %q: %t, Check knows it: %t", action, applyKnows, checkKnows)
			}
		})
	}
}

// node is the JSON of a node that runs action on entering.
func node(action string) string {
	return `{"message":{"type":"text","content":"x"},"action":"` + action + `"}`
}

func TestCheckAcceptsTheEngagingFlow(t *testing.T) {
	t.Parallel()

	engaging, err := fsm.EngagingExample()
	if err != nil {
		t.Fatal(err)
	}

	err = flow.NewActions(t.TempDir()).Check(engaging)
	if err != nil {
		t.Errorf("Check(engaging) = %v, want none", err)
	}
}

func TestCheckNamesEveryUnknownAction(t *testing.T) {
	t.Parallel()

	const data = `{
		"start_node": "A",
		"global_transitions": [
			{"condition": {"type": "exact", "value": ["ayuda"]}, "target": "NEEDS_ASSISTANCE", "action": "escalate_to_human_agent"},
			{"condition": {"type": "exact", "value": ["fin"]}, "target": "NEEDS_ASSISTANCE", "action": "global_typo"}
		],
		"transition_groups": {
			"menu": [{"condition": {"type": "any_text"}, "target": "B", "action": "group_typo"}]
		},
		"nodes": {
			"A": {
				"message": {"type": "text", "content": "x"},
				"action": "node_typo",
				"include_transitions": "menu",
				"transitions": [{"condition": {"type": "any_text"}, "target": "B", "action": "transition_typo"}]
			},
			"B": {"message": {"type": "text", "content": "x"}, "include_transitions": "menu"},
			"NEEDS_ASSISTANCE": {"message": {"type": "text", "content": "x"}}
		}
	}`

	parsed, err := fsm.Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}

	err = flow.NewActions(t.TempDir()).Check(parsed)
	if !errors.Is(err, flow.ErrUnknownAction) {
		t.Fatalf("Check() = %v, want ErrUnknownAction", err)
	}

	for _, want := range []string{
		`global_transitions[1]: unknown action: "global_typo"`,
		`group "menu"[0]: unknown action: "group_typo"`,
		`node "A": unknown action: "node_typo"`,
		`node "A" transitions[0]: unknown action: "transition_typo"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Check() = %v, want it to contain %q", err, want)
		}
	}

	if got := strings.Count(err.Error(), "group_typo"); got != 1 {
		t.Errorf("a group's action is reported %d times, want once, not once per node that includes it", got)
	}
}
