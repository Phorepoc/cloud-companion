// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Where a page puts its action buttons: the title bar of the window it is in (desktop view) or the top bar of the app (classic layout). The shell provides the element;
// the page teleports its buttons into it, so the page itself has no toolbar row of its own.
import { inject, type InjectionKey, type Ref } from 'vue'

export const TITLE_ACTIONS: InjectionKey<Ref<HTMLElement | null>> = Symbol('titleActions')

/** The element to teleport the page's buttons into (null until the shell has mounted it, and where there is no shell). */
export const useTitleActions = () => inject(TITLE_ACTIONS, null)
