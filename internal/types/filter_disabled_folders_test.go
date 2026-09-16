package types

import "testing"

func TestFilterDisabledFoldersDefault(t *testing.T) {
	if !FilterDisabledFoldersDefault(nil) {
		t.Fatal("omitted chat filter must exclude closed folders")
	}
	enabled := true
	if !FilterDisabledFoldersDefault(&enabled) {
		t.Fatal("explicit true must exclude closed folders")
	}
	disabled := false
	if FilterDisabledFoldersDefault(&disabled) {
		t.Fatal("explicit false must keep the opt-out")
	}
}
