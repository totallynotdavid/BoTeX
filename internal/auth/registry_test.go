package auth_test

import (
	"errors"
	"testing"

	"github.com/totallynotdavid/botkit/internal/auth"
)

const (
	registryUser  = "15550000011@s.whatsapp.net"
	registryOther = "15550000012@s.whatsapp.net"
	registryGroup = "120363000000000011@g.us"
)

func TestRegisterRejectsAJIDThatCanNeverMatch(t *testing.T) {
	t.Parallel()

	service, _ := newService(t)

	for _, jid := range []string{"", "15550000011", "15550000011:7@s.whatsapp.net", "a b@s.whatsapp.net"} {
		err := service.RegisterUser(t.Context(), jid, rankUser, "cli")
		if !errors.Is(err, auth.ErrInvalidJID) {
			t.Errorf("RegisterUser(%q) = %v, want ErrInvalidJID", jid, err)
		}
	}

	for _, jid := range []string{"", "nonsense", registryUser} {
		err := service.RegisterGroup(t.Context(), jid, "cli")
		if !errors.Is(err, auth.ErrInvalidJID) {
			t.Errorf("RegisterGroup(%q) = %v, want ErrInvalidJID", jid, err)
		}
	}
}

func TestRegisterUserRefusesAnActiveUserAndAnUnknownRank(t *testing.T) {
	t.Parallel()

	service, _ := newService(t)

	err := service.RegisterUser(t.Context(), registryUser, rankUser, "cli")
	if err != nil {
		t.Fatal(err)
	}

	err = service.RegisterUser(t.Context(), registryUser, rankAdmin, "cli")
	if !errors.Is(err, auth.ErrUserExists) {
		t.Errorf("second RegisterUser() = %v, want ErrUserExists", err)
	}

	err = service.RegisterUser(t.Context(), registryOther, "ghost", "cli")
	if !errors.Is(err, auth.ErrRankNotFound) {
		t.Errorf("RegisterUser() with an unknown rank = %v, want ErrRankNotFound", err)
	}
}

func TestDeactivatedUserLosesAccessAndRegistersAgain(t *testing.T) {
	t.Parallel()

	service, _ := newService(t)

	err := service.RegisterUser(t.Context(), registryUser, rankAdmin, "cli")
	if err != nil {
		t.Fatal(err)
	}

	err = service.DeactivateUser(t.Context(), registryUser)
	if err != nil {
		t.Fatal(err)
	}

	decision, err := service.Authorize(t.Context(), registryUser, "", cmdLatex)
	if err != nil || decision != auth.UserNotRegistered {
		t.Errorf("Authorize() after DeactivateUser = %v, %v; want UserNotRegistered", decision, err)
	}

	err = service.DeactivateUser(t.Context(), registryUser)
	if !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("second DeactivateUser() = %v, want ErrUserNotFound", err)
	}

	err = service.DeactivateUser(t.Context(), registryOther)
	if !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("DeactivateUser() of a stranger = %v, want ErrUserNotFound", err)
	}

	err = service.RegisterUser(t.Context(), registryUser, rankUser, "cli")
	if err != nil {
		t.Fatalf("RegisterUser() of a deactivated user = %v", err)
	}

	user, err := service.GetUser(t.Context(), registryUser)
	if err != nil || user.Rank != rankUser {
		t.Errorf("GetUser() = %+v, %v; want the new rank %s", user, err, rankUser)
	}
}

func TestListUsersHoldsOnlyActiveUsers(t *testing.T) {
	t.Parallel()

	service, _ := newService(t)

	for _, jid := range []string{registryUser, registryOther} {
		err := service.RegisterUser(t.Context(), jid, rankUser, "cli")
		if err != nil {
			t.Fatal(err)
		}
	}

	err := service.DeactivateUser(t.Context(), registryUser)
	if err != nil {
		t.Fatal(err)
	}

	users, err := service.ListUsers(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(users) != 1 || users[0].ID != registryOther || users[0].Rank != rankUser || users[0].RegisteredBy != "cli" {
		t.Errorf("ListUsers() = %+v, want only %s as %s registered by cli", users, registryOther, rankUser)
	}
}

func TestGroupLifecycle(t *testing.T) {
	t.Parallel()

	service, _ := newService(t)

	err := service.RegisterGroup(t.Context(), registryGroup, "cli")
	if err != nil {
		t.Fatal(err)
	}

	err = service.RegisterGroup(t.Context(), registryGroup, "cli")
	if !errors.Is(err, auth.ErrGroupExists) {
		t.Errorf("second RegisterGroup() = %v, want ErrGroupExists", err)
	}

	groups, err := service.ListGroups(t.Context())
	if err != nil || len(groups) != 1 || groups[0].ID != registryGroup {
		t.Errorf("ListGroups() = %+v, %v; want the group", groups, err)
	}
}

func TestDeactivatedGroupLeavesTheListAndRegistersAgain(t *testing.T) {
	t.Parallel()

	service, _ := newService(t)

	err := service.RegisterGroup(t.Context(), registryGroup, "cli")
	if err != nil {
		t.Fatal(err)
	}

	err = service.DeactivateGroup(t.Context(), registryGroup)
	if err != nil {
		t.Fatal(err)
	}

	err = service.DeactivateGroup(t.Context(), registryGroup)
	if !errors.Is(err, auth.ErrGroupNotRegistered) {
		t.Errorf("second DeactivateGroup() = %v, want ErrGroupNotRegistered", err)
	}

	groups, err := service.ListGroups(t.Context())
	if err != nil || len(groups) != 0 {
		t.Errorf("ListGroups() after DeactivateGroup = %+v, %v; want none", groups, err)
	}

	err = service.RegisterGroup(t.Context(), registryGroup, "cli")
	if err != nil {
		t.Errorf("RegisterGroup() of a deactivated group = %v", err)
	}
}
