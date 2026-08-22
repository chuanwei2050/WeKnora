package service

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestSelectActiveProfileModelUsesSameRoleFromActiveProfile(t *testing.T) {
	requested := profileModel("online-chat", types.ModelProfileOnline, "chat")
	offlineChat := profileModel("offline-chat", types.ModelProfileOffline, "chat")
	offlineChat.IsDefault = true

	got, err := selectActiveProfileModel(
		[]*types.Model{requested, offlineChat}, types.ModelProfileOffline, requested,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != offlineChat.ID {
		t.Fatalf("selected model = %q, want %q", got.ID, offlineChat.ID)
	}
}

func TestSelectActiveProfileModelKeepsVerifierInsideVerifierGroup(t *testing.T) {
	requested := profileModel("online-verifier", types.ModelProfileOnline, "verifier_2")
	offlineChat := profileModel("offline-chat", types.ModelProfileOffline, "chat")

	_, err := selectActiveProfileModel(
		[]*types.Model{requested, offlineChat}, types.ModelProfileOffline, requested,
	)
	if err == nil || !strings.Contains(err.Error(), "verifier_2") {
		t.Fatalf("error = %v, want missing verifier_2", err)
	}
}

func TestSelectActiveProfileModelAllowsVerifierOneToReuseChat(t *testing.T) {
	requested := profileModel("online-verifier-1", types.ModelProfileOnline, "verifier_1")
	offlineChat := profileModel("offline-chat", types.ModelProfileOffline, "chat")

	got, err := selectActiveProfileModel(
		[]*types.Model{requested, offlineChat}, types.ModelProfileOffline, requested,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != offlineChat.ID {
		t.Fatalf("selected model = %q, want %q", got.ID, offlineChat.ID)
	}
}

func TestSelectActiveProfileModelIgnoresInactiveDefault(t *testing.T) {
	requested := profileModel("online-chat", types.ModelProfileOnline, "chat")
	inactiveDefault := profileModel("offline-default", types.ModelProfileOffline, "chat")
	inactiveDefault.IsDefault = true
	inactiveDefault.Status = types.ModelStatusDownloadFailed
	active := profileModel("offline-active", types.ModelProfileOffline, "chat")

	got, err := selectActiveProfileModel(
		[]*types.Model{requested, inactiveDefault, active}, types.ModelProfileOffline, requested,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != active.ID {
		t.Fatalf("selected model = %q, want %q", got.ID, active.ID)
	}
}

func profileModel(id string, profile types.ModelProfile, role string) *types.Model {
	return &types.Model{ID: id, Profile: profile, ProfileRole: role, Status: types.ModelStatusActive}
}
