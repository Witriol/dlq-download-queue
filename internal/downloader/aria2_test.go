package downloader

import "testing"

func TestIsGIDNotFoundMessage(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{msg: "", want: false},
		{msg: "No such download", want: true},
		{msg: "GID cannot be found", want: true},
		{msg: "resource not found", want: true},
		{msg: "timeout while connecting", want: false},
	}
	for _, tt := range tests {
		if got := isGIDNotFoundMessage(tt.msg); got != tt.want {
			t.Fatalf("isGIDNotFoundMessage(%q)=%v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestIsActionNotAllowedMessage(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{msg: "", want: false},
		{msg: "GID#abc cannot be paused now", want: true},
		{msg: "GID#abc cannot be unpaused now", want: true},
		{msg: "GID#abc cannot be resumed now", want: true},
		{msg: "No such download", want: false},
	}
	for _, tt := range tests {
		if got := isActionNotAllowedMessage(tt.msg); got != tt.want {
			t.Fatalf("isActionNotAllowedMessage(%q)=%v, want %v", tt.msg, got, tt.want)
		}
	}
}
