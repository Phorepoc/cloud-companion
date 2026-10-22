// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package browser

import (
	"context"

	"github.com/chromedp/cdproto/input"
)

func inputInsert(ctx context.Context, text string) error {
	return input.InsertText(text).Do(ctx)
}
