package gomodel

import (
	"bufio"
	"strings"
	"testing"
)

func TestReadEvent(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("id: 42\nevent: audit.completed\ndata: {\"ok\":\ndata: true}\n\n"))
	event, err := ReadEvent(reader)
	if err != nil {
		t.Fatal(err)
	}
	if event.ID != "42" || event.Event != "audit.completed" || string(event.Data) != "{\"ok\":\ntrue}" {
		t.Fatalf("unexpected event: %+v", event)
	}
}
