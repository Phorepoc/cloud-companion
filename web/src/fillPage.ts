// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

/** QPage style-fn for a page whose panes scroll on their own (SplitView): exactly the room the layout gives, never taller. */
export const fillPage = (offset: number, height: number) => ({ height: height - offset + 'px', minHeight: '0' })
