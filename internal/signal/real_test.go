// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build signal

// Against a real signal-cli-rest-api with no account linked (testdata/signal/test.sh): what can be checked without a phone, which
// is the API's answers, the link QR code and a clear "not linked yet".

package signal

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestRealAPIWithoutAccount(t *testing.T) {
	api := os.Getenv("SIGNAL_API")
	png, err := LinkQR(context.Background(), api)
	if err != nil || len(png) < 200 {
		t.Fatalf("QR code: %d bytes, %v", len(png), err)
	}
	if _, err := Test(context.Background(), Cfg{API: api, Number: "+306900000001"}); err == nil || !strings.Contains(err.Error(), "not linked yet") {
		t.Fatalf("an unlinked number: %v", err)
	}
	x := &Conn{Acc: "a", Name: "sig", Cfg: Cfg{API: api, Number: "+306900000001"}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := x.Start(); err == nil || !strings.Contains(err.Error(), "not linked yet") {
		t.Fatalf("starting an unlinked account: %v", err)
	}
	t.Log("the real API gives a link QR code and says an unlinked number is not linked")
}
