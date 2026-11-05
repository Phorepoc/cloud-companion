// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package telegram

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"os"
	"sync"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"rsc.io/qr"
)

// Login is a sign-in with a QR code, as Telegram Desktop's: Cloud Companion shows a code, you scan it on your phone (Telegram →
// Settings → Devices → Link Desktop Device), and type your two-step password if you set one. The session is then kept in dir,
// named after your user ID.
type Login struct {
	mu     sync.Mutex
	state  string // waiting (show the QR code), password (needs the two-step password), done, failed
	qr     []byte // PNG
	err    string
	user   int64
	name   string
	pass   chan string
	cancel context.CancelFunc
}

// LoginState is what the dialog shows.
type LoginState struct {
	State  string `json:"state"`
	PNG    []byte `json:"png,omitempty"`
	Error  string `json:"error,omitempty"`
	UserID int64  `json:"user_id,omitempty"`
	Name   string `json:"name,omitempty"`
}

// NewLogin starts a sign-in; it gives up after 5 minutes.
func NewLogin(dir string, apiID int, hash string) (*Login, error) {
	return newLogin(dir, apiID, hash, nil)
}

func newLogin(dir string, apiID int, hash string, opts func(*telegram.Options)) (*Login, error) {
	if apiID == 0 || hash == "" {
		return nil, errors.New("the API ID and hash are needed (my.telegram.org → API development tools)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	l := &Login{state: "starting", pass: make(chan string, 1), cancel: cancel}
	go l.run(ctx, dir, apiID, hash, opts)
	return l, nil
}

func (l *Login) set(f func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f()
}

// State is where the sign-in is.
func (l *Login) State() LoginState {
	l.mu.Lock()
	defer l.mu.Unlock()
	return LoginState{State: l.state, PNG: l.qr, Error: l.err, UserID: l.user, Name: l.name}
}

// Password gives the two-step password the sign-in asked for.
func (l *Login) Password(p string) {
	select {
	case l.pass <- p:
	default:
	}
}

// Cancel ends the sign-in.
func (l *Login) Cancel() { l.cancel() }

func (l *Login) run(ctx context.Context, dir string, apiID int, hash string, opts func(*telegram.Options)) {
	defer l.cancel()
	mem := &session.StorageMemory{}
	d := tg.NewUpdateDispatcher()
	loggedIn := qrlogin.OnLoginToken(d)
	o := telegram.Options{SessionStorage: mem, Device: device, UpdateHandler: d}
	if opts != nil {
		opts(&o)
		o.SessionStorage = mem
	}
	cl := telegram.NewClient(apiID, hash, o)
	err := cl.Run(ctx, func(ctx context.Context) error {
		_, err := cl.QR().Auth(ctx, loggedIn, func(ctx context.Context, t qrlogin.Token) error {
			img, err := t.Image(qr.M)
			if err != nil {
				return err
			}
			var b bytes.Buffer
			if err := png.Encode(&b, img); err != nil {
				return err
			}
			l.set(func() { l.state, l.qr = "waiting", b.Bytes() })
			return nil
		})
		for tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
			l.set(func() { l.state, l.qr = "password", nil })
			select {
			case <-ctx.Done():
				return ctx.Err()
			case p := <-l.pass:
				_, err = cl.Auth().Password(ctx, p)
				if errors.Is(err, auth.ErrPasswordInvalid) {
					l.set(func() { l.err = "wrong password, try again" })
					err = tgerr.New(401, "SESSION_PASSWORD_NEEDED")
				}
			}
		}
		if err != nil {
			return err
		}
		me, err := cl.Self(ctx)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := mem.WriteFile(sessionFile(dir, me.ID), 0o600); err != nil {
			return err
		}
		l.set(func() { l.state, l.user, l.name, l.err = "done", me.ID, name(me), "" })
		return nil
	})
	if err != nil {
		msg := err.Error()
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			msg = "the QR code was not scanned in time; start again"
		case tgerr.Is(err, "API_ID_INVALID"):
			msg = "Telegram does not know this API ID and hash"
		}
		l.set(func() { l.state, l.err = "failed", msg })
	}
}
