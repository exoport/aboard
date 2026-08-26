//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/exoport/aboard/pkg/aboard"
)

// The notify channel, end to end: a real session parked on `aboard wait`, the
// button in the browser, and the session exiting 0 with the poke it was waiting
// for.
//
// The button's whole claim is "someone is listening". A waiter is an OPEN
// CONNECTION, so the count cannot go stale — if the session dies, the connection
// dies and the button greys out on its own — and that is what makes the claim
// honest rather than hopeful. Nothing here starts a session: the button only
// releases one that already chose to listen, which is the decision recorded in
// docs/explanation/why-nothing-in-the-ui-starts-a-session.md.
//
// The waiter is `aboard.Wait` called in a goroutine rather than the binary in a
// subprocess. It is the same function `aboard wait` is a two-line wrapper around,
// so the code under test is identical, and it saves building and finding a
// binary from inside a test that is already driving a browser.
func TestTheNotifyButtonReleasesAWaitingSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	var out bytes.Buffer
	const who = "agent-e2e-waiter"
	const why = "checking the notify channel"

	done := make(chan waitResult, 1)
	go func() {
		code, err := aboard.Wait(ctx, board, "", who, "poke", why, 30*time.Second, &out)
		done <- waitResult{code: code, err: err}
	}()
	// Whatever happens, do not leave a waiter parked for the tests that follow:
	// TestTheNotifyButtonIsDisabledWithNobodyWaiting would then be asserting the
	// opposite of what is true. (The deferred cancel above already does this; a
	// second one is harmless and says why it matters here.)
	defer cancel()

	eventually(t, "the waiting session to register", func() bool { return waiterCount(t) == 1 })

	s := open(t, "")
	poke := s.page.Locator("#poke")
	if err := expect.Locator(poke).ToBeEnabled(); err != nil {
		t.Fatalf("the button is not live with a session waiting: %v", err)
	}
	if err := expect.Locator(poke).ToHaveAttribute("data-live", "yes"); err != nil {
		t.Errorf("the lit dot is not lit: %v", err)
	}
	// The label names WHO is waiting and how long they have left — a button that
	// said only "notify" would be asking the human to guess what they are
	// interrupting.
	if err := expect.Locator(poke).ToContainText("notify " + who); err != nil {
		t.Errorf("the button does not name the waiting session: %v", err)
	}
	if err := expect.Locator(poke).ToContainText("·"); err != nil {
		t.Errorf("the button shows no countdown: %v", err)
	}
	title, err := poke.GetAttribute("title")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(title, why) {
		t.Errorf("the tooltip does not say what the session is waiting for: %q", title)
	}

	if err := poke.Click(); err != nil {
		t.Fatalf("pressing notify: %v", err)
	}
	// What the button must do after the press: stop claiming a session is
	// listening. It is the honest half, and it is the half that matters — a
	// board with nothing waiting is simply not listening, and saying otherwise
	// is the failure this whole feature was built to avoid.
	//
	// NOT asserted: the "notified 1 session" label the click handler sets. It is
	// written and then overwritten before a human could read it — releasing the
	// waiter changes the waiter count, which broadcasts on the SSE stream, which
	// calls refreshWaiters(), which repaints from scratch. Measured here with a
	// 15 ms polling loop that never once caught it. Recorded rather than
	// asserted or "fixed": whether the human should get a moment of confirmation
	// is a judgement about the interface, not a defect this suite gets to decide.
	if err := expect.Locator(poke).ToBeDisabled(); err != nil {
		t.Errorf("the button still looks live after everyone was released: %v", err)
	}
	if err := expect.Locator(poke).ToContainText("no session waiting"); err != nil {
		t.Errorf("the button does not say the board has stopped listening: %v", err)
	}

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("the waiting session failed: %v", res.err)
		}
		if res.code != aboard.ExitOK {
			t.Errorf("the released session exited %d, want %d", res.code, aboard.ExitOK)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the waiting session was never released")
	}

	printed := out.String()
	if !strings.Contains(printed, `"event":"poke"`) {
		t.Errorf("the session did not print the poke it was released by: %q", printed)
	}
	if !strings.Contains(printed, `"by":"human"`) {
		t.Errorf("the poke does not say the human sent it: %q", printed)
	}
	eventually(t, "nobody to be left waiting", func() bool { return waiterCount(t) == 0 })
}

type waitResult struct {
	code int
	err  error
}

func waiterCount(t *testing.T) int {
	t.Helper()
	var body struct {
		Waiting int `json:"waiting"`
	}
	getJSON(t, "/waiters", &body)
	return body.Waiting
}
