package llm

import "testing"

func TestAssemblerSingleCallSplitAcrossChunks(t *testing.T) {
	var asm toolCallAssembler
	asm.add(0, "call-1", "edit_file", `{"path": "ma`)
	asm.add(0, "", "", `in.go", `)
	asm.add(0, "", "", `"old": "a", "new": "b"}`)

	calls := asm.flush()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	c := calls[0]
	if c.ID != "call-1" || c.Name != "edit_file" {
		t.Errorf("ID/Name = %q/%q", c.ID, c.Name)
	}
	want := `{"path": "main.go", "old": "a", "new": "b"}`
	if c.Arguments != want {
		t.Errorf("Arguments = %q, want %q", c.Arguments, want)
	}
}

func TestAssemblerInterleavedCalls(t *testing.T) {
	// Two tools' fragments interleave; output order must be the order
	// each call's fragments first appeared.
	var asm toolCallAssembler
	asm.add(0, "call-a", "read_file", `{"path": "a`)
	asm.add(1, "call-b", "run_shell", `{"command": "ls`)
	asm.add(0, "", "", `.txt"}`)
	asm.add(1, "", "", ` -la"}`)

	calls := asm.flush()
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	if calls[0].ID != "call-a" || calls[0].Name != "read_file" ||
		calls[0].Arguments != `{"path": "a.txt"}` {
		t.Errorf("call 0 = %+v", calls[0])
	}
	if calls[1].ID != "call-b" || calls[1].Name != "run_shell" ||
		calls[1].Arguments != `{"command": "ls -la"}` {
		t.Errorf("call 1 = %+v", calls[1])
	}
}

func TestAssemblerWholeCallInOneChunk(t *testing.T) {
	// Local servers often send the whole call in one fragment; that must
	// work too, not just the fragmented case.
	var asm toolCallAssembler
	asm.add(0, "call-1", "read_file", `{"path": "x"}`)
	calls := asm.flush()
	if len(calls) != 1 || calls[0].Arguments != `{"path": "x"}` {
		t.Errorf("calls = %+v", calls)
	}
	if !asm.hasContent() {
		t.Error("hasContent = false after adding a fragment")
	}
}

func TestAssemblerEmpty(t *testing.T) {
	var asm toolCallAssembler
	if asm.hasContent() {
		t.Error("hasContent = true on a fresh assembler")
	}
	if calls := asm.flush(); len(calls) != 0 {
		t.Errorf("flush on empty assembler = %+v", calls)
	}
}
