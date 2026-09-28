package bot_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/totallynotdavid/botkit/internal/bot"
)

func TestSessionEndedError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  *bot.SessionEndedError
		want string
	}{
		{&bot.SessionEndedError{Reason: bot.NotPaired}, "whatsapp session ended: not_paired"},
		{
			&bot.SessionEndedError{Reason: bot.LoggedOut, Detail: "401: logged out from another device"},
			"whatsapp session ended: logged_out (401: logged out from another device)",
		},
		{&bot.SessionEndedError{Reason: bot.Replaced}, "whatsapp session ended: replaced"},
		{&bot.SessionEndedError{Reason: bot.Banned, Detail: "expires in 2h"}, "whatsapp session ended: banned (expires in 2h)"},
		{&bot.SessionEndedError{Reason: bot.Outdated}, "whatsapp session ended: outdated"},
		{
			&bot.SessionEndedError{Reason: bot.Refused, Detail: "CAT refresh failed"},
			"whatsapp session ended: refused (CAT refresh failed)",
		},
		{&bot.SessionEndedError{Reason: 0}, "whatsapp session ended: SessionEndReason(0)"},
	}

	for _, testCase := range tests {
		if got := testCase.err.Error(); got != testCase.want {
			t.Errorf("Error() = %q, want %q", got, testCase.want)
		}

		var ended *bot.SessionEndedError
		if !errors.As(fmt.Errorf("run: %w", testCase.err), &ended) || ended.Reason != testCase.err.Reason {
			t.Errorf("errors.As lost the reason through wrapping for %v", testCase.err)
		}
	}
}
