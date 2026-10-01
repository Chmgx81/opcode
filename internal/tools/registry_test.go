package tools

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

// TestRegistryIsSafeToRegisterWhileItIsRead: a mid-turn trust grant
// registers the project's tools from the tea goroutine while the turn
// goroutine is inside Defs or Get building its next request. That is a
// genuine concurrent write to the slice both of them read — append
// reallocates, so the reader can be handed a torn header. The race
// detector is the assertion here; the test says nothing on its own.
func TestRegistryIsSafeToRegisterWhileItIsRead(t *testing.T) {
	var r Registry
	r.Register(ReadFile{})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			r.Register(ReadFile{})
		}
	}()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				r.Get("read_file")
				r.Defs()
				r.All()
			}
		}()
	}
	// Enough iterations for an unsynchronized registry to be caught;
	// short enough to stay fast.
	for i := 0; i < 2000; i++ {
		r.Defs()
	}
	close(stop)
	wg.Wait()

	// The registry is still coherent: every registered tool is
	// reachable, and the reads never saw a torn slice.
	if _, ok := r.Get("read_file"); !ok {
		t.Error("read_file went missing from the registry")
	}
	if got := len(r.All()); got != len(r.Defs()) {
		t.Errorf("All saw %d tools, Defs saw %d", got, len(r.Defs()))
	}
}

// TestRegisterKeepsRegistrationOrder: the order is the order the model
// is offered tools in, and the lock must not reshuffle it.
func TestRegisterKeepsRegistrationOrder(t *testing.T) {
	var r Registry
	for _, name := range []string{"a", "b", "c", "d"} {
		r.Register(namedTool{name: name})
	}
	var got []string
	for _, d := range r.Defs() {
		got = append(got, d.Name)
	}
	want := []string{"a", "b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("Defs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Defs = %v, want %v", got, want)
		}
	}
}

type namedTool struct{ name string }

func (t namedTool) Name() string              { return t.name }
func (namedTool) Description() string         { return "a tool with a chosen name" }
func (namedTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (namedTool) Tier() Tier                  { return TierReadOnly }
func (namedTool) Execute(ctx context.Context, a string) (string, error) {
	return "", nil
}
