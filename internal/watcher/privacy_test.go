package watcher

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	cases := map[string]string{
		"Inbox - budi.santoso@zahir.co.id - Outlook":    "Inbox - [email] - Outlook",
		"Invoice 1234567890 - Excel":                    "Invoice [num] - Excel",
		"Call +62 812-3456-7890":                        "Call +[num]",
		"https://x.com/a?token=abc and more":            "https://x.com/a and more",
		"key sk-ant-api03-abcdefghijklmnop in terminal": "key [secret] in terminal",
		"main.go - petai - Visual Studio Code":          "main.go - petai - Visual Studio Code",
		"Room 12345":                                    "Room 12345",
	}
	for in, want := range cases {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Redact(strings.Repeat("a", 300)); len(got) > 170 {
		t.Error("not truncated")
	}
}

func TestBlocked(t *testing.T) {
	bl := []string{"1password", "bank", "bca", "klikbca", "bri", "incognito"}
	if !Blocked("1Password.exe", "Vault", bl) {
		t.Error("app match")
	}
	if !Blocked("chrome.exe", "KlikBCA Individual", bl) {
		t.Error("klikbca")
	}
	if !Blocked("chrome.exe", "BCA - Internet Banking", bl) {
		t.Error("word match")
	}
	if !Blocked("msedge.exe", "New tab - Incognito", bl) {
		t.Error("incognito")
	}
	if Blocked("code.exe", "fabric.go - Visual Studio Code", bl) {
		t.Error("false positive on short word")
	}
	if !Blocked("chrome.exe", "Mandiri Online Banking", bl) {
		t.Error("bank substring")
	}
}
