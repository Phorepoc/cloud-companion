<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- The desktop's widgets (after the Etesian Belle widgets): clocks, unread mail, the agenda and the latest chats, on the wallpaper
     under every window. A row opens its section as a window. View → Edit widgets turns on the phone's edit chrome: an accent
     outline, a red ✕ squircle to remove, a faint 90 px grid to snap to, and the Add widgets sheet. In edit mode a widget moves by
     dragging or, focused, with the arrow keys; Delete removes it; Done or Esc ends editing. No long-press, no context menu. -->
<template>
  <div class="ph-wg" :class="{ 'is-editing': editing }" data-test="widgets">
    <div v-for="w in list" :key="w.id" class="ph-wg__w" :class="['ph-wg__w--' + w.kind, { 'is-dragging': drag?.id === w.id }]" :style="boxOf(w)"
         :tabindex="editing ? 0 : undefined" :role="editing ? 'group' : undefined" :aria-label="editing ? KINDS[w.kind].label + ' widget, arrow keys move it' : undefined"
         :data-test="'widget-' + w.kind" @pointerdown="startDrag($event, w)" @keydown="onKey($event, w)">
      <!-- clocks -->
      <div v-if="w.kind === 'clock-digital'" class="ph-wg__panel ph-wg__digital">
        <time :datetime="now.toISOString()"><b>{{ clock.hour }}</b><span class="ph-wg__colon">:</span><b>{{ clock.minute }}</b><small v-if="clock.period" class="ph-wg__period">{{ clock.period }}</small></time>
        <span class="ph-wg__sec">{{ dateText }}</span>
      </div>
      <div v-else-if="w.kind === 'clock-analog'" class="ph-wg__analog" role="img" :aria-label="'Clock, ' + clock.text">
        <!-- the Belle analog dial, as Nokia drew it (a 126 grid): a squircle face in warm charcoal, white hands, the blue second hand -->
        <svg viewBox="0 0 126 126" aria-hidden="true">
          <defs>
            <linearGradient id="ph-wg-bezel" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#5A5A55" /><stop offset=".5" stop-color="#3E3E39" /><stop offset="1" stop-color="#34342F" /></linearGradient>
            <linearGradient id="ph-wg-face" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#41413C" /><stop offset=".22" stop-color="#373732" /><stop offset=".59" stop-color="#32322D" /><stop offset="1" stop-color="#282823" /></linearGradient>
            <linearGradient id="ph-wg-win" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#2A2A26" /><stop offset="1" stop-color="#3C3C37" /></linearGradient>
          </defs>
          <path :d="DIAL.outer" fill="url(#ph-wg-bezel)" stroke="#1C1C19" stroke-width="1" />
          <path :d="DIAL.inner" fill="url(#ph-wg-face)" />
          <path :d="DIAL.inner" fill="none" stroke="#fff" stroke-opacity=".13" stroke-width=".8" />
          <line v-for="a in [0, 90, 180, 270]" :key="a" x1="63" y1="10.5" x2="63" y2="15.3" stroke="#fff" stroke-width="2.7" stroke-linecap="round" :transform="`rotate(${a} 63 63)`" />
          <line v-for="a in [30, 60, 120, 150, 210, 240, 300, 330]" :key="a" x1="63" y1="11.5" x2="63" y2="15.5" stroke="#D6D6D6" stroke-opacity=".4" stroke-width="1.6" stroke-linecap="round" :transform="`rotate(${a} 63 63)`" />
          <path :d="DIAL.win" fill="url(#ph-wg-win)" stroke="#000" stroke-opacity=".35" stroke-width=".8" />
          <text x="92" y="67.6" text-anchor="middle" font-size="12.5" fill="#D6D6D6" class="ph-wg__dialdate">{{ tick.getDate() }}</text>
          <g class="ph-wg__hands">
            <path class="ph-wg__hand--h" d="M60.5 68 L60.6 25.5 Q63 24.2 65.4 25.5 L65.5 68 Z" fill="#fff" :transform="`rotate(${hourDeg} 63 63)`" />
            <path class="ph-wg__hand--m" d="M61.7 68 L61.75 9.4 Q63 8.6 64.25 9.4 L64.3 68 Z" fill="#fff" :transform="`rotate(${minDeg} 63 63)`" />
            <circle cx="63" cy="63" r="5" fill="#fff" />
          </g>
          <line class="ph-wg__hand--s" x1="63" y1="84" x2="63" y2="9" stroke="#4791DC" stroke-width="1.1" :transform="`rotate(${secDeg} 63 63)`" />
          <circle cx="63" cy="63" r="2.2" fill="#4791DC" /><circle cx="63" cy="63" r="1" fill="#fff" />
        </svg>
      </div>

      <!-- mail -->
      <div v-else-if="w.kind === 'mail'" class="ph-wg__panel">
        <header class="ph-wg__head"><SectionTile :section="byId.mail" :size="32" /><b class="ph-wg__acct">{{ w.opt || 'Mail' }}</b>
          <button v-if="editing" type="button" class="ph-wg__arrow" aria-label="Pick the account" title="Pick the account" data-test="widget-mail-account" @pointerdown.stop @click="pickAccount(w)"><q-icon name="sym_o_settings" size="20px" /></button>
          <span v-if="mailOf(w).unread" class="ph-wg__count" data-test="widget-mail-count">{{ mailOf(w).unread }} unread</span></header>
        <p v-if="mailOf(w).error" class="ph-wg__none">{{ mailOf(w).error }}</p>
        <p v-else-if="!mailOf(w).items.length" class="ph-wg__none">No mail.</p>
        <!-- the newest 20, read or not (unread marked), scrolling: as the Belle Mail widget -->
        <div v-else class="ph-wg__list" data-test="widget-mail-list">
          <button v-for="m in mailOf(w).items" :key="m.id" type="button" class="ph-wg__row" :class="{ 'is-unread': m.unread }" :disabled="editing" @click="emit('open', 'mail')">
            <span class="ph-wg__title">{{ m.from || '(no sender)' }}<span class="ph-wg__sec ph-wg__when">{{ mailTime(m.date) }}</span></span>
            <span class="ph-wg__sec ph-wg__line">{{ m.subject || '(no subject)' }}</span>
          </button>
        </div>
      </div>

      <!-- agenda -->
      <div v-else-if="w.kind === 'agenda'" class="ph-wg__panel">
        <header class="ph-wg__head"><SectionTile :section="byId.calendar" :size="32" /><b>Today</b></header>
        <p v-if="cal.error" class="ph-wg__none">{{ cal.error }}</p>
        <p v-else-if="!today.length" class="ph-wg__none">Nothing planned today.</p>
        <div v-else class="ph-wg__list">
          <button v-for="e in today" :key="e.href || e.title + e.start" type="button" class="ph-wg__row" :disabled="editing" @click="emit('open', 'calendar')">
            <span class="ph-wg__title">{{ e.title || '(no title)' }}</span>
            <span class="ph-wg__sec ph-wg__line">{{ e.allday ? 'All day' : timeLabel(toDate(e.start)) }}{{ e.location ? ' · ' + e.location : '' }}</span>
          </button>
        </div>
      </div>

      <!-- chats -->
      <div v-else-if="w.kind === 'chats'" class="ph-wg__panel">
        <header class="ph-wg__head"><SectionTile :section="byId.chats" :size="32" /><b>Chats</b></header>
        <p v-if="chats.error" class="ph-wg__none">{{ chats.error }}</p>
        <p v-else-if="!chats.items.length" class="ph-wg__none">No messages yet.</p>
        <div v-else class="ph-wg__list">
          <button v-for="c in chats.items" :key="c.key" type="button" class="ph-wg__row" :disabled="editing" @click="emit('open', 'chats')">
            <span class="ph-wg__title">{{ c.name || c.number }}<span class="ph-wg__sec ph-wg__when">{{ ago(now, c.last_ts) }}</span></span>
            <span class="ph-wg__sec ph-wg__line">{{ c.last_dir === 'out' ? 'You: ' : '' }}{{ c.last }}</span>
          </button>
        </div>
      </div>

      <!-- latest mail, one at a time (Belle "Latest mail"): ▲ ▼ step through, disabled at the ends -->
      <div v-else-if="w.kind === 'mail-stack'" class="ph-wg__panel ph-wg__stack">
        <header class="ph-wg__head"><SectionTile :section="byId.mail" :size="32" /><b class="ph-wg__acct">{{ w.opt || 'Latest mail' }}</b>
          <button v-if="editing" type="button" class="ph-wg__arrow" aria-label="Pick the account" title="Pick the account" @pointerdown.stop @click="pickAccount(w)"><q-icon name="sym_o_settings" size="20px" /></button>
          <span class="ph-wg__nav">
            <button type="button" class="ph-wg__arrow" aria-label="Newer mail" :disabled="editing || (stackAt[w.id] || 0) === 0" @click="stackAt[w.id] = (stackAt[w.id] || 0) - 1"><q-icon name="sym_o_keyboard_arrow_up" size="22px" /></button>
            <button type="button" class="ph-wg__arrow" aria-label="Older mail" data-test="widget-stack-older" :disabled="editing || (stackAt[w.id] || 0) >= mailOf(w).items.length - 1" @click="stackAt[w.id] = (stackAt[w.id] || 0) + 1"><q-icon name="sym_o_keyboard_arrow_down" size="22px" /></button>
          </span></header>
        <p v-if="mailOf(w).error" class="ph-wg__none">{{ mailOf(w).error }}</p>
        <p v-else-if="!mailOf(w).items.length" class="ph-wg__none">No mail.</p>
        <button v-else type="button" class="ph-wg__row ph-wg__one" :disabled="editing" data-test="widget-stack-mail" @click="emit('open', 'mail')">
          <span class="ph-wg__title">{{ stackOf(w)?.from || '(no sender)' }}<span class="ph-wg__sec ph-wg__when">{{ (stackAt[w.id] || 0) + 1 }} / {{ mailOf(w).items.length }}</span></span>
          <span class="ph-wg__line">{{ stackOf(w)?.subject || '(no subject)' }}</span>
          <span class="ph-wg__sec ph-wg__clamp">{{ stackOf(w)?.preview }}</span>
        </button>
      </div>

      <!-- month (Belle calendar): today lit, a dot under days with events; tap opens Calendar -->
      <button v-else-if="w.kind === 'month'" type="button" class="ph-wg__panel ph-wg__month" :disabled="editing" @click="emit('open', 'calendar')">
        <b class="ph-wg__mtitle">{{ monthTitle }}</b>
        <span class="ph-wg__mgrid">
          <span v-for="d in WEEKDAYS" :key="'h' + d" class="ph-wg__sec">{{ d }}</span>
          <span v-for="(d, i) in monthDays" :key="i" class="ph-wg__day" :class="{ 'is-out': !d.inMonth, 'is-today': d.today }">{{ d.n }}<i v-if="d.events" /></span>
        </span>
      </button>

      <!-- contacts (Belle "Contacts, group"): squircle initials; tap opens Contacts -->
      <div v-else-if="w.kind === 'contacts'" class="ph-wg__panel ph-wg__people">
        <p v-if="people.error" class="ph-wg__none">{{ people.error }}</p>
        <p v-else-if="!people.items.length" class="ph-wg__none">No contacts yet.</p>
        <div v-else class="ph-wg__hlist">
          <button v-for="c in people.items" :key="c.href || c.name" type="button" class="ph-wg__person" :disabled="editing" :title="c.name" @click="emit('open', 'contacts')">
            <span class="hd-squircle ph-wg__avatar" :style="{ background: tint(c.name) }">{{ initials(c.name) }}</span><small>{{ c.given || c.name.split(' ')[0] }}</small>
          </button>
        </div>
      </div>

      <!-- notes (Belle Notes): yellow paper; ▲ ▼ browse, + writes a new one, the bin deletes the one shown. Kept in this browser. -->
      <div v-else-if="w.kind === 'notes'" class="ph-wg__notes">
        <textarea v-model="notes.list[notes.at]" class="ph-wg__paper" :readonly="editing" aria-label="Note" placeholder="Tap + to write a note." data-test="widget-note" @input="saveNotes" />
        <span class="ph-wg__notebar">
          <button type="button" aria-label="Previous note" :disabled="editing || notes.at === 0" @click="notes.at--"><q-icon name="sym_o_arrow_drop_up" size="28px" /></button>
          <button type="button" aria-label="Next note" :disabled="editing || notes.at >= notes.list.length - 1" @click="notes.at++"><q-icon name="sym_o_arrow_drop_down" size="28px" /></button>
          <button type="button" aria-label="New note" data-test="widget-note-add" :disabled="editing" @click="addNote"><q-icon name="sym_o_add" size="24px" /></button>
          <button type="button" aria-label="Delete note" :disabled="editing" @click="deleteNote"><q-icon name="sym_o_delete" size="22px" /></button>
        </span>
      </div>

      <!-- shortcuts (Belle shortcuts): four sections on their lit squircles -->
      <div v-else-if="w.kind === 'shortcuts'" class="ph-wg__panel ph-wg__shorts">
        <button v-for="sec in shortcutSections" :key="sec.id" type="button" class="ph-wg__short" :disabled="editing" @click="emit('open', sec.id)">
          <SectionTile :section="sec" :size="40" /><small>{{ sec.label }}</small>
        </button>
      </div>

      <!-- flip clock: the hours and minutes on two lit tiles, the date below -->
      <div v-else-if="w.kind === 'clock-flip'" class="ph-wg__flip">
        <span class="ph-wg__tile">{{ clock.hour }}</span><span class="ph-wg__tile">{{ clock.minute }}</span>
        <span class="ph-wg__sec ph-wg__flipdate">{{ dateText }}</span>
      </div>

      <!-- the time in words (Belle text clock) -->
      <div v-else-if="w.kind === 'clock-word'" class="ph-wg__word" role="img" :aria-label="'Clock, ' + clock.text">
        <b>{{ words[0] }}</b><span>{{ words[1] }}</span><small class="ph-wg__sec">{{ dateText }}</small>
      </div>

      <!-- weather (Belle Weather): now, today's high / low, the place (picks another), the next two days -->
      <div v-else-if="w.kind === 'weather'" class="ph-wg__weather">
        <div class="ph-wg__panel ph-wg__wxbody">
          <div class="ph-wg__wxnow">
            <div class="ph-wg__wxt"><span data-test="weather-temp">{{ forecast ? forecast.temperature + '°' : '--°' }}</span>
              <small v-if="forecast?.days[0]">{{ forecast.days[0].high }}° / {{ forecast.days[0].low }}°</small></div>
            <button type="button" class="ph-wg__wxplace" :disabled="editing" data-test="weather-place-open" @click="pickingPlace = true">{{ placeText }}</button>
          </div>
          <div v-for="d in (forecast?.days || []).slice(1, 3)" :key="d.date" class="ph-wg__wxday">
            <q-icon :name="weatherIcon(d.code)" size="34px" aria-hidden="true" /><span>{{ d.high }}° / {{ d.low }}°</span><span class="ph-wg__sec">{{ weekday(d.date) }}</span>
          </div>
        </div>
        <q-icon class="ph-wg__wxicon" :name="weatherIcon(forecast?.code ?? 3, forecast?.day ?? true)" size="76px" :aria-label="forecast?.description || 'Weather'" />
      </div>

      <!-- gear clock (Belle, belle-ref11): a round dial in a lighter metal rim with twelve ticks, toothed metal gears turning behind the
           hands, a thin white second hand, the day low on the left and the date at 3 o'clock in dark round bubbles -->
      <div v-else-if="w.kind === 'clock-gear'" class="ph-wg__gear" role="img" :aria-label="'Clock, ' + clock.text">
        <svg viewBox="0 0 126 126" aria-hidden="true">
          <defs>
            <radialGradient id="ph-wg-gface" cx=".5" cy=".4" r=".6"><stop offset="0" stop-color="#3A3B3F" /><stop offset="1" stop-color="#121214" /></radialGradient>
            <linearGradient id="ph-wg-grim" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#6A6B70" /><stop offset=".5" stop-color="#3A3B3F" /><stop offset="1" stop-color="#202124" /></linearGradient>
            <linearGradient id="ph-wg-gmetal" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#55565B" /><stop offset="1" stop-color="#2A2B2F" /></linearGradient>
          </defs>
          <circle cx="63" cy="63" r="62" fill="url(#ph-wg-grim)" stroke="#050506" />
          <circle cx="63" cy="63" r="53" fill="url(#ph-wg-gface)" stroke="#050506" stroke-width=".8" />
          <g v-for="(g, i) in GEARS" :key="i" :transform="`rotate(${(i % 2 ? -1 : 1) * secDeg * (20 / g.r)} ${g.x} ${g.y})`" class="ph-wg__cog">
            <path :d="g.d" fill="url(#ph-wg-gmetal)" stroke="#0E0E10" stroke-width=".5" />
            <circle :cx="g.x" :cy="g.y" :r="g.r * 0.55" fill="#1A1B1E" />
            <circle :cx="g.x" :cy="g.y" :r="g.r * 0.18" fill="#4A4B50" />
          </g>
          <line v-for="a in 12" :key="a" x1="63" y1="3.5" x2="63" :y2="a % 3 ? 7.5 : 9" stroke="#C8C9CD" stroke-width="1.4" stroke-linecap="round" :transform="`rotate(${a * 30} 63 63)`" />
          <g class="ph-wg__hands">
            <path d="M60.5 68 L60.6 25.5 Q63 24.2 65.4 25.5 L65.5 68 Z" fill="#fff" :transform="`rotate(${hourDeg} 63 63)`" />
            <path d="M61.7 68 L61.75 12 Q63 11.2 64.25 12 L64.3 68 Z" fill="#fff" :transform="`rotate(${minDeg} 63 63)`" />
          </g>
          <line class="ph-wg__hand--s" x1="63" y1="76" x2="63" y2="12" stroke="#E6E6E8" stroke-width=".8" :transform="`rotate(${secDeg} 63 63)`" />
          <circle cx="63" cy="63" r="5.5" fill="url(#ph-wg-grim)" stroke="#0E0E10" stroke-width=".6" /><circle cx="63" cy="63" r="1.6" fill="#D0D1D5" />
        </svg>
        <span class="ph-wg__gday">{{ tick.toLocaleDateString(undefined, { weekday: 'short' }) }}</span><span class="ph-wg__gdate">{{ tick.getDate() }}</span>
      </div>

      <!-- LCD clock (Belle, belle-ref10): green segment digits on black glass in a dark metal frame, the date beside -->
      <div v-else-if="w.kind === 'clock-lcd'" class="ph-wg__lcd" role="img" :aria-label="'Clock, ' + clock.text">
        <div class="ph-wg__lcdglass"><span class="ph-wg__lcdt">{{ clock.hour }}</span><span class="ph-wg__lcdc">:</span><span class="ph-wg__lcdt">{{ clock.minute }}</span>
          <span class="ph-wg__lcdd">{{ now.toLocaleDateString(undefined, { day: '2-digit', month: '2-digit' }) }}</span></div>
      </div>

      <!-- Today (Belle calendar card): white paper on rings, the next entry of the day; tap opens Calendar -->
      <button v-else-if="w.kind === 'today'" type="button" class="ph-wg__paperwrap" :disabled="editing" data-test="widget-today" @click="emit('open', 'calendar')">
        <i class="ph-wg__ring" style="left: 18%" /><i class="ph-wg__ring" style="right: 18%" />
        <span class="ph-wg__todaypaper">
          <span class="ph-wg__todayday">Today</span>
          <span v-if="cal.error" class="ph-wg__todayev">{{ cal.error }}</span>
          <span v-else-if="!nextToday" class="ph-wg__todayev">Nothing planned</span>
          <span v-else class="ph-wg__todayev"><b>{{ nextToday.allday ? 'All day' : timeLabel(toDate(nextToday.start)) }}</b><i class="ph-wg__cyan" /><span>{{ nextToday.title || '(no title)' }}</span></span>
        </span>
      </button>

      <!-- one contact (Belle contact shortcut): the photo, or squircle initials, with the name on a dark strip; tap opens Contacts -->
      <button v-else-if="w.kind === 'contact'" type="button" class="ph-wg__photo" :disabled="editing" data-test="widget-contact" @click="emit('open', 'contacts')">
        <span class="ph-wg__pinit hd-squircle" :style="{ background: tint(w.opt || '?') }">{{ initials(w.opt || '?') }}</span>
        <span class="ph-wg__strip">{{ w.opt || 'Pick a contact' }}</span>
      </button>

      <!-- web page (Belle web shortcut): the site's name on the strip; tap opens it in a new tab -->
      <button v-else-if="w.kind === 'web'" type="button" class="ph-wg__photo ph-wg__webtile" :disabled="editing" data-test="widget-web" @click="openPage(w.opt)">
        <img v-if="w.opt && !noIcon[w.opt]" :src="iconOf(w.opt)" alt="" class="ph-wg__favicon" @error="noIcon[w.opt] = true" />
        <q-icon v-else name="sym_o_language" size="56px" class="ph-wg__favicon" aria-hidden="true" />
        <span class="ph-wg__strip">{{ w.opt ? hostOf(w.opt) : 'Pick a page' }}</span>
      </button>

      <!-- feeds (Belle RSS widget): orange header with the feed count, the newest posts below; tap opens the post -->
      <div v-else-if="w.kind === 'feeds'" class="ph-wg__rss" data-test="widget-feeds">
        <header class="ph-wg__rsshead"><span><b>Feeds</b><small>{{ feedList.length === 1 ? feedList[0].title : feedList.length + ' feeds' }}</small></span>
          <button type="button" class="ph-wg__arrow" aria-label="Refresh" :disabled="editing" @click="loadFeeds"><q-icon name="sym_o_refresh" size="24px" /></button></header>
        <div class="ph-wg__rssbody">
          <p v-if="feedsError" class="ph-wg__rssnone">Could not load: {{ feedsError }}</p>
          <p v-else-if="!posts.length" class="ph-wg__rssnone">{{ feedList.length ? 'No posts yet.' : 'No feeds yet. Add them under Feeds in Events.' }}</p>
          <button v-for="p in posts.slice(0, 20)" :key="p.link + p.title" type="button" class="ph-wg__rssrow" :disabled="editing" data-test="widget-feed" @click="openPost(p)">
            <i class="ph-wg__rssdot" /><span class="ph-wg__rsst">{{ p.title || '(no title)' }}<small>{{ p.feed }}{{ postTs(p) ? ' · ' + ago(now, postTs(p)) : '' }}</small></span></button>
        </div>
      </div>

      <!-- music (Belle small, belle-music-widget-small.jpg): a dark glossy panel, the square art (as is) rising above its top edge, the song
           over previous / play / next in a recessed groove; nothing playing: the blue tile, "Go to Music library" -->
      <div v-else-if="w.kind === 'music'" class="ph-wg__music">
        <div class="ph-wg__mpanel" />
        <button type="button" class="ph-wg__mart" :disabled="editing" aria-label="Open Music" @click="emit('open', 'music')">
          <AlbumCover v-if="song" :album="song.album" :cover="song.cover" :size="124" /><span v-else class="ph-wg__mempty"><svg viewBox="0 0 24 24" :width="40" :height="40" aria-hidden="true"><path d="M9 17.5V5.5l11-2.5v12" fill="none" stroke="#050508" stroke-width="2" /><ellipse cx="6.5" cy="17.5" rx="3" ry="2.4" fill="#050508" /><ellipse cx="17.5" cy="15" rx="3" ry="2.4" fill="#050508" /></svg></span></button>
        <div class="ph-wg__mside"><span class="ph-wg__mtitle" data-test="widget-music-title">{{ song ? song.title + ' · ' + song.artist : 'Go to Music library' }}</span>
          <div class="ph-wg__mctl">
            <button v-for="(b, i) in musicButtons" :key="i" type="button" class="ph-wg__mbtn hd-squircle" :class="{ 'is-big': i === 1, 'is-dim': !song }" :aria-label="b.label"
                    :disabled="editing" :data-test="'widget-music-' + b.id" @click="b.do()"><svg viewBox="0 0 24 24" :width="i === 1 ? 22 : 15" :height="i === 1 ? 22 : 15"><path :d="b.d" fill="currentColor" /></svg></button>
          </div></div>
      </div>
      <!-- music, large (belle-music-widget-large.jpg): the square art stands up out of a glossy black tray that holds the buttons -->
      <div v-else-if="w.kind === 'music-large'" class="ph-wg__mlarge">
        <div class="ph-wg__mtray" />
        <button type="button" class="ph-wg__mlart" :disabled="editing" aria-label="Open Music" @click="emit('open', 'music')">
          <AlbumCover v-if="song" :album="song.album" :cover="song.cover" :size="150" /><span v-else class="ph-wg__mempty is-large"><svg viewBox="0 0 24 24" :width="64" :height="64" aria-hidden="true"><path d="M9 17.5V5.5l11-2.5v12" fill="none" stroke="#050508" stroke-width="2" /><ellipse cx="6.5" cy="17.5" rx="3" ry="2.4" fill="#050508" /><ellipse cx="17.5" cy="15" rx="3" ry="2.4" fill="#050508" /></svg><span>Go to Music library</span></span></button>
        <div v-if="song" class="ph-wg__mlt"><b>{{ song.title }}</b><u>{{ song.artist }}</u></div>
        <div class="ph-wg__mlctl">
          <button v-for="(b, i) in musicButtons" :key="i" type="button" class="ph-wg__mbtn hd-squircle is-l" :class="{ 'is-big': i === 1, 'is-dim': !song }" :aria-label="b.label"
                  :disabled="editing" @click="b.do()"><svg viewBox="0 0 24 24" :width="i === 1 ? 28 : 18" :height="i === 1 ? 28 : 18"><path :d="b.d" fill="currentColor" /></svg></button>
        </div>
      </div>

      <!-- devices: the paired devices, connected ones first (only the owner sees them) -->
      <div v-else-if="w.kind === 'devices'" class="ph-wg__panel" data-test="widget-devices">
        <header class="ph-wg__head"><SectionTile :section="byId.devices" :size="32" /><b>Devices</b></header>
        <p v-if="!state.admin" class="ph-wg__none">Only the owner sees the devices.</p>
        <p v-else-if="!devices.length" class="ph-wg__none">No devices yet.</p>
        <div v-else class="ph-wg__list">
          <button v-for="d in devices" :key="d.id" type="button" class="ph-wg__dev" :disabled="editing" @click="emit('open', 'devices')">
            <i class="ph-wg__devdot" :class="{ 'is-on': d.online }" /><span class="ph-wg__title">{{ d.name }}</span>
            <span class="ph-wg__sec">{{ d.online ? 'Connected' : d.seen ? 'Seen ' + ago(now, d.seen) : 'Never seen' }}</span></button>
        </div>
      </div>

      <template v-if="editing">
        <i class="ph-wg__outline" aria-hidden="true" />
        <button type="button" class="ph-wg__x" :aria-label="'Remove ' + KINDS[w.kind].label" title="Remove" :data-test="'widget-remove-' + w.kind"
                @pointerdown.stop @click="remove(w)">
          <span class="hd-squircle" aria-hidden="true"><q-icon name="sym_o_close" size="18px" /></span>
        </button>
      </template>
    </div>

    <!-- edit mode: add widgets -->
    <aside v-if="editing" class="ph-wg__sheet" aria-label="Add widgets" data-test="widget-sheet" @keydown.esc="emit('done')">
      <h2>Add widgets</h2>
      <section v-for="g in groups" :key="g.group">
        <h3>{{ g.group }}</h3>
        <button v-for="k in g.kinds" :key="k" type="button" class="ph-wg__add" :data-test="'widget-add-' + k" @click="add(k)">
          {{ KINDS[k].label }}<q-icon name="sym_o_add" size="20px" aria-hidden="true" />
        </button>
      </section>
      <p v-if="full" class="ph-wg__none" role="status">There is no room left. Remove or move a widget first.</p>
      <q-btn unelevated no-caps color="primary" label="Done" class="full-width q-mt-md" data-test="widgets-done" @click="emit('done')" />
    </aside>
    <WeatherPlaceDialog v-model="pickingPlace" />
    <!-- the setting of a contact or web page widget, asked when it is added -->
    <q-dialog :model-value="!!setting" @update:model-value="(v) => { if (!v) setting = null }">
      <q-card class="ph-wg__setcard" data-test="widget-setting">
        <q-card-section><b>{{ setting?.kind === 'web' ? 'Which page?' : setting?.kind === 'contact' ? 'Which contact?' : 'Which account?' }}</b></q-card-section>
        <q-card-section v-if="setting?.kind === 'web'">
          <q-input v-model="settingText" dense outlined autofocus label="Address" placeholder="https://example.org" data-test="widget-setting-url" @keyup.enter="saveSetting" />
        </q-card-section>
        <q-list v-else-if="setting?.kind !== 'contact'" dense class="ph-wg__setlist">
          <q-item v-for="a in ['', ...accounts]" :key="a" clickable data-test="widget-setting-account" :active="(setting?.opt || '') === a" @click="setting!.opt = a || undefined; setting = null; refresh()">
            <q-item-section>{{ a || 'Every inbox' }}</q-item-section></q-item>
        </q-list>
        <q-list v-else dense class="ph-wg__setlist">
          <q-item v-for="c in people.items" :key="c.href || c.name" clickable data-test="widget-setting-contact" @click="settingText = c.name; saveSetting()">
            <q-item-section>{{ c.name }}</q-item-section></q-item>
          <q-item v-if="!people.items.length"><q-item-section class="ph-muted">{{ people.error || 'No contacts yet.' }}</q-item-section></q-item>
        </q-list>
        <q-card-actions align="right">
          <q-btn flat no-caps label="Cancel" @click="setting = null" />
          <q-btn v-if="setting?.kind === 'web'" unelevated no-caps color="primary" label="Save" data-test="widget-setting-save" @click="saveSetting" />
        </q-card-actions>
      </q-card>
    </q-dialog>
  </div>
</template>

<script setup lang="ts">
import AlbumCover from '../components/AlbumCover.vue'
import { current as currentSong, next as musicNext, player, prev as musicPrev, toggle as musicToggle } from '../music'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { state, api } from '../cloud'
import { feedList, posts, feedsError, loadFeeds, openPost, postTs } from './feeds'
import { sections } from '../nav'
import { timeLabel, toDate, dayLabel } from '../dates'
import type { CalendarEvent, Contact, MailItem, SmsConversation } from '../proto'
import SectionTile from '../components/SectionTile.vue'
import { quiet, loadEvents, eventsOn, loadMail, mailAccounts, loadChats, loadContacts, ago } from './glance'
import { ymd } from '../dates'
import WeatherPlaceDialog from './WeatherPlaceDialog.vue'
import { forecast, noPlace, weatherError, loadWeather, weatherIcon } from './weather'
import { CELL, MARGIN, KINDS, loadWidgets, saveWidgets, gridSize, fits, freeSpot, type Widget, type WidgetKind } from './widgets'

const props = defineProps<{ now: Date; area: { w: number; h: number }; editing: boolean }>()
const emit = defineEmits<{ open: [id: string]; done: [] }>()
const byId = Object.fromEntries(sections.map((s) => [s.id, s]))

const list = reactive<Widget[]>(loadWidgets())
watch(list, () => saveWidgets(list), { deep: true })
const grid = computed(() => gridSize(props.area.w, props.area.h))
const full = ref(false)

const groups = computed(() => {
  const out: { group: string; kinds: WidgetKind[] }[] = []
  for (const k of Object.keys(KINDS) as WidgetKind[]) {
    const g = KINDS[k].group
    const at = out.find((x) => x.group === g)
    if (at) at.kinds.push(k); else out.push({ group: g, kinds: [k] })
  }
  return out
})

// ---- placing ----
const drag = ref<{ id: string; dx: number; dy: number } | null>(null) // the widget being dragged and its offset in px
const boxOf = (w: Widget) => {
  const k = KINDS[w.kind]
  const d = drag.value?.id === w.id ? drag.value : null
  return { left: MARGIN + w.x * CELL + (d ? d.dx : 0) + 'px', top: MARGIN + w.y * CELL + (d ? d.dy : 0) + 'px', width: k.w * CELL - 12 + 'px', height: k.h * CELL - 12 + 'px' }
}
function moveTo(w: Widget, x: number, y: number) {
  if (fits({ ...w, x, y }, list, grid.value.cols, grid.value.rows)) { w.x = x; w.y = y; return true }
  return false
}
function startDrag(e: PointerEvent, w: Widget) {
  if (!props.editing || e.button !== 0) return
  const sx = e.clientX, sy = e.clientY
  drag.value = { id: w.id, dx: 0, dy: 0 }
  const move = (ev: PointerEvent) => { if (drag.value) Object.assign(drag.value, { dx: ev.clientX - sx, dy: ev.clientY - sy }) }
  const up = (ev: PointerEvent) => {
    removeEventListener('pointermove', move); removeEventListener('pointerup', up)
    drag.value = null
    // snap to the nearest cell; a spot that is taken or off the grid leaves the widget where it was
    moveTo(w, w.x + Math.round((ev.clientX - sx) / CELL), w.y + Math.round((ev.clientY - sy) / CELL))
  }
  addEventListener('pointermove', move); addEventListener('pointerup', up)
}
function onKey(e: KeyboardEvent, w: Widget) {
  if (!props.editing || e.target !== e.currentTarget) return
  const step: Record<string, [number, number]> = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }
  if (step[e.key]) { e.preventDefault(); moveTo(w, w.x + step[e.key][0], w.y + step[e.key][1]) }
  else if (e.key === 'Delete' || e.key === 'Backspace') { e.preventDefault(); remove(w) }
  else if (e.key === 'Escape') emit('done')
}
function add(kind: WidgetKind) {
  const at = freeSpot(kind, list, grid.value.cols, grid.value.rows)
  full.value = !at
  if (at) list.push({ id: 'w' + Date.now().toString(36) + Math.random().toString(36).slice(2, 6), kind, ...at })
  if (at && (kind === 'contact' || kind === 'web')) {
    setting.value = list[list.length - 1]; settingText.value = ''
    if (kind === 'contact') loadContacts().then((c) => { people.items = c; people.error = '' }, (e) => { people.error = quiet(e, 'Contacts') })
  }
  if (at && kind === 'weather') loadWeather()
}
function remove(w: Widget) {
  const i = list.findIndex((x) => x.id === w.id)
  if (i >= 0) list.splice(i, 1)
  full.value = false
}

// ---- weather ----
const pickingPlace = ref(false)
const placeText = computed(() => forecast.value?.place.name || (noPlace.value ? 'Pick a place' : weatherError.value ? 'No weather' : 'Updating…'))
const weekday = (d: string) => new Date(d + 'T12:00:00').toLocaleDateString([], { weekday: 'short' })

// ---- clocks ----
// the time as the menu bar shows it (the browser's locale: 24 h, or 12 h with AM / PM)
const clock = computed(() => {
  const parts = new Intl.DateTimeFormat([], { hour: '2-digit', minute: '2-digit' }).formatToParts(props.now)
  const part = (t: string) => parts.find((p) => p.type === t)?.value || ''
  return { hour: part('hour'), minute: part('minute'), period: part('dayPeriod'), text: parts.map((p) => p.value).join('') }
})
const dateText = computed(() => dayLabel(props.now))
// the analog clock keeps its own second (the desktop's `now` moves every 15 s)
const tick = ref(new Date())
let ticking: ReturnType<typeof setInterval> | undefined
watch(() => list.some((w) => w.kind === 'clock-analog' || w.kind === 'clock-gear'), (on) => {
  clearInterval(ticking); ticking = undefined
  if (on) { tick.value = new Date(); ticking = setInterval(() => { tick.value = new Date() }, 1000) }
}, { immediate: true })
const secDeg = computed(() => tick.value.getSeconds() * 6)
const minDeg = computed(() => tick.value.getMinutes() * 6 + tick.value.getSeconds() / 10)
const hourDeg = computed(() => (tick.value.getHours() % 12) * 30 + tick.value.getMinutes() / 2 + tick.value.getSeconds() / 120)
/** A squircle |x|^n + |y|^n = 1 stretched to a w × h box at (x, y) (etesian-design squircleRectPath). */
function squircleRect(w: number, h: number, n: number, x = 0, y = 0): string {
  const pts: string[] = []
  for (let i = 0; i < 96; i++) {
    const t = (i / 96) * Math.PI * 2, c = Math.cos(t), s = Math.sin(t)
    pts.push(`${(x + w / 2 + (w / 2) * Math.sign(c) * Math.abs(c) ** (2 / n)).toFixed(2)} ${(y + h / 2 + (h / 2) * Math.sign(s) * Math.abs(s) ** (2 / n)).toFixed(2)}`)
  }
  return 'M' + pts.join('L') + 'Z'
}
/** A toothed gear at (x, y), radius r (the phone's gear dial draws the same). */
function gearPath(x: number, y: number, r: number, teeth: number): string {
  const pts: string[] = []
  for (let i = 0; i < teeth * 4; i++) { // each tooth: two points out, two in
    const a = (Math.PI * 2 * i) / (teeth * 4), rr = i % 4 < 2 ? r : r * 0.86
    pts.push(`${(x + rr * Math.cos(a)).toFixed(2)} ${(y + rr * Math.sin(a)).toFixed(2)}`)
  }
  return 'M' + pts.join('L') + 'Z'
}
/** the gear clock's gears (a 126 grid, as on the phone): x, y, radius and teeth */
const GEARS = [[83, 39, 26, 18], [39, 83, 24, 16], [63, 63, 22, 16], [87, 82, 14, 11]].map(([x, y, r, t]) => ({ x, y, r, d: gearPath(x, y, r, t) }))
/** the 701's dial: n 2.9 face and bezel, the date window at 3 o'clock */
const DIAL = { outer: squircleRect(126, 126, 2.9), inner: squircleRect(118, 118, 2.9, 4, 4), win: squircleRect(26, 26, 2.8, 79, 50) }

// ---- data: loaded for the kinds on the desktop, refreshed every two minutes ----
/** mail per account ('' = every inbox), for the mail widgets showing it */
const mailBy = reactive<Record<string, { items: MailItem[]; unread: number; error: string }>>({})
const mailOf = (w: Widget) => mailBy[w.opt || ''] || (mailBy[w.opt || ''] = { items: [], unread: 0, error: '' })
const cal = reactive({ items: [] as CalendarEvent[], error: '' })
const chats = reactive({ items: [] as SmsConversation[], error: '' })
const people = reactive({ items: [] as Contact[], error: '' })
const today = computed(() => eventsOn(cal.items, props.now))
const has = (...k: WidgetKind[]) => list.some((w) => k.includes(w.kind))
function refresh() {
  if (has('weather')) loadWeather()
  for (const acct of new Set(list.filter((w) => w.kind === 'mail' || w.kind === 'mail-stack').map((w) => w.opt || ''))) {
    const m = mailBy[acct] || (mailBy[acct] = { items: [], unread: 0, error: '' })
    loadMail(20, acct).then((r) => { m.unread = r.unread; m.items = r.items; m.error = '' }, (e) => { m.error = quiet(e, 'Mail') })
  }
  if (has('agenda', 'month')) loadEvents(monthStart.value, 42).then((ev) => { cal.items = ev; cal.error = '' }, (e) => { cal.error = quiet(e, 'The calendar') })
  if (has('chats')) loadChats(20).then((c) => { chats.items = c; chats.error = '' }, (e) => { chats.error = quiet(e, 'Chats') })
  if (has('today') && !has('agenda', 'month')) loadEvents(props.now, 1).then((ev) => { cal.items = ev; cal.error = '' }, (e) => { cal.error = quiet(e, 'The calendar') })
  if (has('feeds')) loadFeeds()
  if (has('devices') && state.admin) api.devices()
  if (has('contacts')) loadContacts().then((c) => { people.items = c; people.error = '' }, (e) => { people.error = quiet(e, 'Contacts') })
}
/** a mail's time: the time today, else the day and month */
function mailTime(date: string) {
  const d = new Date(date)
  if (isNaN(d.getTime())) return ''
  return ymd(d) === ymd(props.now) ? timeLabel(d) : d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
}

// today: the next entry still to come (or the last of the day)
const nextToday = computed(() => today.value.find((e) => e.allday || toDate(e.end || e.start) >= props.now) || null)

// mail widgets: one account's inbox, or every inbox (Plasma's Mail widget settings)
const accounts = ref<string[]>([])
async function pickAccount(w: Widget) {
  setting.value = w
  try { accounts.value = await mailAccounts() } catch { accounts.value = [] }
}

// contact and web page widgets: their setting
const setting = ref<Widget | null>(null), settingText = ref('')
function saveSetting() {
  const w = setting.value, t = settingText.value.trim()
  if (!w || !t) return
  w.opt = w.kind === 'web' && !/^https?:\/\//i.test(t) ? 'https://' + t : t
  setting.value = null
}
const noIcon = reactive<Record<string, boolean>>({}) // sites whose icon did not load show the globe
const hostOf = (u: string) => { try { return new URL(u).host.replace(/^www\./, '') } catch { return u } }
const iconOf = (u: string) => { try { return new URL('/favicon.ico', u).href } catch { return '' } }
function openPage(u?: string) { if (u && /^https?:\/\//i.test(u)) window.open(u, '_blank', 'noopener') }

// devices: connected ones (seen in the last two minutes) first, then by when they were last seen
const devices = computed(() => (state.deviceList || []).filter((d) => !d.revoked)
  .map((d) => ({ ...d, online: !!d.seen && Date.now() / 1000 - d.seen < 120 }))
  .sort((a, b) => Number(b.online) - Number(a.online) || b.seen - a.seen))

// latest mail, one at a time
const stackAt = reactive<Record<string, number>>({})
const stackOf = (w: Widget) => { const items = mailOf(w).items; return items[Math.min(stackAt[w.id] || 0, items.length - 1)] }

// month: six weeks from the Monday on or before the 1st
const WEEKDAYS = ['M', 'T', 'W', 'T', 'F', 'S', 'S']
const monthStart = computed(() => { const f = new Date(props.now.getFullYear(), props.now.getMonth(), 1); return new Date(f.getFullYear(), f.getMonth(), 1 - ((f.getDay() + 6) % 7)) })
const monthTitle = computed(() => props.now.toLocaleDateString(undefined, { month: 'long', year: 'numeric' }))
const monthDays = computed(() => {
  const busy = new Set(cal.items.map((e) => ymd(toDate(e.start))))
  return Array.from({ length: 42 }, (_, i) => {
    const d = new Date(monthStart.value.getFullYear(), monthStart.value.getMonth(), monthStart.value.getDate() + i)
    return { n: d.getDate(), inMonth: d.getMonth() === props.now.getMonth(), today: ymd(d) === ymd(props.now), events: busy.has(ymd(d)) }
  })
})

// contacts: initials on a squircle, a steady colour per name
const initials = (n: string) => n.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]).join('').toUpperCase()
const TINTS = ['#0067BC', '#09A7CC', '#EF5500', '#62B700', '#8E44AD', '#D81B60']
const tint = (n: string) => TINTS[[...n].reduce((a, c) => a + c.charCodeAt(0), 0) % TINTS.length]

// notes, kept in this browser
const NOTES_KEY = 'phecloud-desktop-notes'
const notes = reactive({ list: [''] as string[], at: 0 })
try { const saved = JSON.parse(localStorage.getItem(NOTES_KEY) || 'null'); if (Array.isArray(saved) && saved.length) notes.list = saved.map(String) } catch (e) { /* none yet */ }
function saveNotes() { try { localStorage.setItem(NOTES_KEY, JSON.stringify(notes.list)) } catch (e) { /* private mode */ } }
function addNote() { notes.list.push(''); notes.at = notes.list.length - 1; saveNotes() }
function deleteNote() { notes.list.splice(notes.at, 1); if (!notes.list.length) notes.list.push(''); notes.at = Math.min(notes.at, notes.list.length - 1); saveNotes() }

// shortcuts: the four primary sections
const shortcutSections = sections.filter((x) => x.primary).slice(0, 4)

// the time in words: "ten past" / "nine", or "quarter to" / "ten"
const NUM = ['twelve', 'one', 'two', 'three', 'four', 'five', 'six', 'seven', 'eight', 'nine', 'ten', 'eleven']
const words = computed(() => {
  const m = Math.round(props.now.getMinutes() / 5) * 5, h = props.now.getHours() + (m > 30 ? 1 : 0), hour = NUM[h % 12]
  const MIN: Record<number, string> = { 5: 'five', 10: 'ten', 15: 'quarter', 20: 'twenty', 25: 'twenty-five', 30: 'half' }
  if (m === 0 || m === 60) return [hour, "o'clock"]
  return m <= 30 ? [MIN[m] + ' past', hour] : [MIN[60 - m] + ' to', hour]
})
// a newly added kind loads at once
watch(() => list.map((w) => w.kind).sort().join(), refresh)
let timer: ReturnType<typeof setInterval> | undefined
// after a dropped connection comes back, catch up
watch(() => state.status, (s, old) => { if (s === 'online' && old !== 'online') refresh() })
onMounted(() => { refresh(); timer = setInterval(refresh, 120000) })
onBeforeUnmount(() => { clearInterval(timer); clearInterval(ticking) })

// the music widgets follow the app's one player
const song = computed(() => currentSong())
const musicButtons = computed(() => [
  { id: 'prev', label: 'Previous', d: 'M6 6h2v12H6zm3.5 6 8.5 6V6z', do: musicPrev },
  { id: 'toggle', label: player.playing ? 'Pause' : 'Play', d: player.playing ? 'M6 5h4v14H6zm8 0h4v14h-4z' : 'M8 5v14l11-7z', do: () => (song.value ? musicToggle() : emit('open', 'music')) },
  { id: 'next', label: 'Next', d: 'M16 6h2v12h-2zM6 18l8.5-6L6 6z', do: musicNext },
])
</script>

<style scoped>
/* the wallpaper layer: under the windows (which stack from z-index 1) */
.ph-wg { position: absolute; inset: 0; z-index: 0; }
.ph-wg.is-editing {
  background-image: linear-gradient(rgba(127, 127, 127, 0.14) 1px, transparent 1px), linear-gradient(90deg, rgba(127, 127, 127, 0.14) 1px, transparent 1px);
  background-size: 90px 90px; background-position: 24px 24px;
}
.ph-wg__w { position: absolute; color: #fff; font-family: var(--ph-font); border-radius: 12px; }
.ph-wg.is-editing .ph-wg__w { cursor: grab; touch-action: none; }
.ph-wg__w.is-dragging { z-index: 1; cursor: grabbing; opacity: 0.9; }
.ph-wg__w:focus-visible { outline: 2px solid #fff; outline-offset: 4px; }
/* the Belle panel: dark glass with a light top edge */
.ph-wg__panel {
  height: 100%; box-sizing: border-box; display: flex; flex-direction: column; gap: 4px; padding: 12px 14px; overflow: hidden;
  border-radius: 12px; border: 1px solid rgba(255, 255, 255, 0.15);
  background: linear-gradient(rgba(255, 255, 255, 0.12), rgba(255, 255, 255, 0) 50%), rgba(0, 0, 0, 0.72);
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.35);
}
.ph-wg__head { display: flex; align-items: center; gap: 10px; margin-bottom: 4px; font-size: var(--hd-font-size-default); }
.ph-wg__count { margin-left: auto; padding: 1px 10px; border-radius: 12px; background: #4591FF; font-size: var(--hd-font-size-xsmall); font-weight: 700; }
.ph-wg__none { margin: 4px 0; font-size: var(--hd-font-size-small); color: rgba(255, 255, 255, 0.6); }
.ph-wg__row {
  display: flex; flex-direction: column; min-width: 0; padding: 6px 4px; border: 0; border-top: 1px solid rgba(255, 255, 255, 0.12);
  background: none; color: inherit; font: inherit; text-align: left; cursor: pointer;
}
.ph-wg__row:hover:not(:disabled), .ph-wg__row:focus-visible { background: rgba(255, 255, 255, 0.1); }
.ph-wg__row:disabled { cursor: inherit; }
.ph-wg__title { display: flex; font-weight: 700; font-size: var(--hd-font-size-small); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ph-wg__when { margin-left: auto; padding-left: 8px; font-weight: 400; }
.ph-wg__sec { color: rgba(255, 255, 255, 0.6); }
.ph-wg__line { font-size: var(--hd-font-size-small); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
/* big clock: light numerals, the date under them */
.ph-wg__digital { align-items: center; justify-content: center; gap: 2px; }
.ph-wg__digital time { font: 300 72px/1 var(--ph-font-light); letter-spacing: 1px; }
.ph-wg__colon { opacity: 0.6; margin: 0 2px; }
.ph-wg__period { margin-left: 8px; font-size: 22px; opacity: 0.7; }
.ph-wg__digital b { font-weight: 300; }
/* analog: the Belle dial on its own */
.ph-wg__analog { height: 100%; display: grid; place-items: center; }
.ph-wg__analog svg { width: 100%; height: 100%; filter: drop-shadow(0 4px 10px rgba(0, 0, 0, 0.4)); }
.ph-wg__hands { filter: drop-shadow(0 1.2px 0.9px rgba(0, 0, 0, 0.55)); }
.ph-wg__dialdate { font-family: var(--ph-font); }
/* gear clock */
.ph-wg__gear { position: relative; height: 100%; aspect-ratio: 1; margin: auto; filter: drop-shadow(0 4px 10px rgba(0, 0, 0, 0.4)); }
.ph-wg__gear svg { position: absolute; inset: 0; width: 100%; height: 100%; }
.ph-wg__gday, .ph-wg__gdate { position: absolute; transform: translate(-50%, -50%); display: grid; place-items: center; border-radius: 50%;
  background: radial-gradient(#1A1B1E, #0B0B0C); box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.6); color: #D0D1D5; font-size: 13px; }
.ph-wg__gday { left: 31%; top: 70%; width: 20%; height: 20%; }
.ph-wg__gdate { left: 83%; top: 50%; width: 13%; height: 13%; }
/* LCD clock */
.ph-wg__lcd { width: fit-content; margin: auto; padding: 8px; border-radius: 14px; border: 1px solid #000; background: linear-gradient(#5A5B60, #2A2B2E 50%, #0E0E10); }
.ph-wg__lcdglass { display: flex; align-items: center; padding: 10px 10px 10px 14px; border-radius: 8px; border: 1px solid #46474B; background: linear-gradient(#2A2B2E, #050506); color: #23B14D; font-family: ui-monospace, monospace; }
.ph-wg__lcdt { display: grid; place-items: center; padding: 0 8px; border-radius: 6px; border: 1px solid #050506; background: linear-gradient(#1E1F22, #111214 50%, #1A1B1D); font-size: 56px; line-height: 1.15; text-shadow: 0 0 6px rgba(35, 177, 77, 0.5); }
.ph-wg__lcdc { padding: 0 3px; font-size: 30px; }
.ph-wg__lcdd { align-self: flex-end; margin-left: 10px; padding: 2px 5px; border-radius: 3px; border: 1px solid #1E3A24; background: #0B140D; font-size: 14px; }
/* Today card */
.ph-wg__paperwrap { position: relative; display: block; width: 100%; height: 100%; padding: 5px 0 0; border: 0; background: none; font: inherit; text-align: left; cursor: pointer; }
.ph-wg__ring { position: absolute; top: 0; z-index: 1; width: 8px; height: 14px; border-radius: 4px; border: 1px solid #6A6B70; background: linear-gradient(90deg, #8E8F94, #F4F4F6, #8E8F94); }
.ph-wg__todaypaper { display: flex; flex-direction: column; height: 100%; border-radius: 12px; border: 1px solid #BDBDC0; background: linear-gradient(#fff, #E6E6E8); color: #1A1A1A; box-shadow: 0 6px 0 -1px rgba(18, 18, 18, 0.94); }
.ph-wg__todayday { padding: 3px 0 2px; text-align: center; font-size: 15px; border-bottom: 1px solid #C9C9CC; }
.ph-wg__todayev { flex: 1; display: flex; align-items: center; gap: 8px; padding: 0 14px; font-size: 16px; overflow: hidden; white-space: nowrap; }
.ph-wg__todayev span { overflow: hidden; text-overflow: ellipsis; }
.ph-wg__cyan { width: 4px; align-self: stretch; margin: 8px 0; background: #09A7CC; }
/* contact and web page shortcuts: the picture as is, the name on a dark strip */
.ph-wg__photo { position: relative; display: grid; place-items: center; width: 100%; height: 100%; padding: 0; overflow: hidden; border: 0; border-radius: 6px; background: #3A3A3C; color: #fff; font: inherit; cursor: pointer; }
.ph-wg__webtile { background: linear-gradient(#fff, #DFE6EE); color: #2A6FB5; }
.ph-wg__favicon { width: 48px; height: 48px; margin-bottom: 30px; }
.ph-wg__pinit { display: grid; place-items: center; width: 64px; height: 64px; margin-bottom: 30px; font-size: 24px; font-weight: 700; }
.ph-wg__strip { position: absolute; left: 0; right: 0; bottom: 0; padding: 6px 10px 7px; background: rgba(0, 0, 0, 0.65); color: #fff; font-size: 16px; text-align: left; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
/* feeds (Belle RSS) */
.ph-wg__rss { display: flex; flex-direction: column; height: 100%; filter: drop-shadow(0 2px 3px rgba(0, 0, 0, 0.5)); }
.ph-wg__rsshead { display: flex; align-items: center; height: 52px; padding: 0 8px 0 14px; border-radius: 10px 10px 0 0; background: linear-gradient(#F7A13A, #E5620F); }
.ph-wg__rsshead > span { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.ph-wg__rsshead b { font-size: 18px; text-shadow: 0 1px 1px rgba(0, 0, 0, 0.4); }
.ph-wg__rsshead small { font-size: 13px; opacity: 0.9; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ph-wg__rssbody { flex: 1; min-height: 0; overflow-y: auto; border-radius: 0 0 10px 10px; background: linear-gradient(#fff, #E9E9EB); color: #1A1A1A; }
.ph-wg__rssnone { margin: 12px 14px; font-size: 14px; color: #555; }
.ph-wg__rssrow { display: flex; align-items: center; gap: 12px; width: 100%; min-height: 52px; padding: 8px 14px; border: 0; border-bottom: 1px solid #DCDCDE; background: none; color: inherit; font: inherit; font-size: 15px; text-align: left; cursor: pointer; }
.ph-wg__rssrow:hover { background: rgba(229, 98, 15, 0.08); }
.ph-wg__rssdot { flex: none; width: 8px; height: 8px; border-radius: 50%; background: #E5620F; }
.ph-wg__rsst { display: flex; flex-direction: column; min-width: 0; }
.ph-wg__rsst small { font-size: 12px; color: #666; }
/* devices */
.ph-wg__dev { display: flex; align-items: center; gap: 10px; width: 100%; padding: 8px 4px; border: 0; border-bottom: 1px solid rgba(255, 255, 255, 0.1); background: none; color: #fff; font: inherit; text-align: left; cursor: pointer; }
.ph-wg__dev:hover { background: rgba(255, 255, 255, 0.08); }
.ph-wg__dev .ph-wg__title { flex: 1; }
.ph-wg__devdot { width: 8px; height: 8px; border-radius: 50%; background: rgba(255, 255, 255, 0.3); }
.ph-wg__devdot.is-on { background: #8FD65A; }
.ph-wg__setcard { min-width: 340px; }
.ph-wg__acct { flex: 1; min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ph-wg__setlist { max-height: 50vh; overflow-y: auto; }
/* edit chrome, as on the phone: accent outline and a red ✕ squircle at the top right */
.ph-wg__outline { position: absolute; inset: -4px; border: 2px solid #4591FF; border-radius: 14px; pointer-events: none; }
.ph-wg__x { position: absolute; top: -14px; right: -14px; padding: 0; border: 0; background: none; cursor: pointer; }
.ph-wg__x .hd-squircle { display: grid; place-items: center; width: 30px; height: 30px; color: #fff; background: linear-gradient(#F78673, #EF2C10); }
.ph-wg__x:focus-visible { outline: 2px solid #fff; outline-offset: 2px; border-radius: 8px; }
/* the Add widgets sheet: an overlay card at the right edge */
.ph-wg__sheet {
  position: absolute; z-index: 9300; top: 12px; right: 12px; bottom: 12px; width: 300px; overflow-y: auto; padding: var(--ph-s4);
  border-radius: var(--ph-r-card); background: #141414; color: #fff; box-shadow: var(--hd-shadow-overlay);
}
.ph-wg__sheet h2 { margin: 0 0 var(--ph-s2); font: 300 var(--ph-text-title)/1.2 var(--ph-font-light); }
.ph-wg__sheet h3 { margin: var(--ph-s3) 0 var(--ph-s1); font: 700 var(--hd-font-size-small)/1.2 var(--ph-font); color: var(--ph-chrome-fg-muted); }
.ph-wg__add {
  display: flex; align-items: center; justify-content: space-between; width: 100%; padding: 8px 10px; border: 0; border-radius: 8px;
  background: none; color: inherit; font: inherit; text-align: left; cursor: pointer;
}
.ph-wg__add:hover, .ph-wg__add:focus-visible { background: rgba(255, 255, 255, 0.1); }
.ph-wg__add .q-icon { color: #4591FF; }
/* the list widgets hold more than fits (up to 20) and scroll, with a thin scrollbar so there is clearly more */
.ph-wg__list { flex: 1; min-height: 0; display: flex; flex-direction: column; overflow-y: auto; overscroll-behavior: contain; margin: 0 -8px; padding: 0 8px;
  scrollbar-width: thin; scrollbar-color: rgba(255, 255, 255, 0.35) transparent; }
.ph-wg__list > .ph-wg__row { flex: none; }
.ph-wg__row.is-unread .ph-wg__title { font-weight: 600; }
.ph-wg__row.is-unread::before { content: ''; position: absolute; left: -2px; top: 14px; width: 6px; height: 6px; border-radius: 50%; background: #4591FF; }
.ph-wg__list .ph-wg__row { position: relative; }
.ph-wg__nav { margin-left: auto; display: flex; gap: 2px; }
.ph-wg__arrow { display: grid; place-items: center; width: 30px; height: 30px; padding: 0; border: 0; -webkit-mask: var(--ph-squircle) center / contain no-repeat; mask: var(--ph-squircle) center / contain no-repeat; background: rgba(255, 255, 255, 0.1); color: #fff; cursor: pointer; }
.ph-wg__arrow:disabled { opacity: 0.3; cursor: default; }
.ph-wg__one { flex: 1; }
.ph-wg__clamp { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; white-space: normal; }
/* month */
.ph-wg__month { width: 100%; text-align: left; color: #fff; font: inherit; cursor: pointer; }
.ph-wg__mtitle { font-weight: 400; font-size: var(--hd-font-size-default); }
.ph-wg__mgrid { flex: 1; display: grid; grid-template-columns: repeat(7, 1fr); align-items: center; justify-items: center; font-size: 13px; }
.ph-wg__day { position: relative; display: grid; place-items: center; width: 26px; height: 22px; }
.ph-wg__day.is-out { color: rgba(255, 255, 255, 0.35); }
.ph-wg__day.is-today { background: linear-gradient(#6AA8FF, #2E72E0); -webkit-mask: var(--ph-squircle) center / contain no-repeat; mask: var(--ph-squircle) center / contain no-repeat; }
.ph-wg__day i { position: absolute; bottom: 0; width: 4px; height: 4px; border-radius: 50%; background: #4591FF; }
/* contacts */
.ph-wg__people { justify-content: center; padding: 6px 10px; }
.ph-wg__hlist { display: flex; gap: 6px; overflow-x: auto; scrollbar-width: none; }
.ph-wg__person { flex: none; display: flex; flex-direction: column; align-items: center; gap: 2px; width: 58px; padding: 0; border: 0; background: none; color: #fff; font: inherit; cursor: pointer; }
.ph-wg__avatar { display: grid; place-items: center; width: 44px; height: 44px; font-size: 16px; font-weight: 600; }
.ph-wg__person small { max-width: 100%; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; font-size: 12px; }
/* notes: yellow paper and a dark bar */
.ph-wg__notes { height: 100%; display: flex; flex-direction: column; filter: drop-shadow(0 2px 3px rgba(0, 0, 0, 0.5)); }
.ph-wg__paper { flex: 1; resize: none; padding: 12px 14px; border: 0; border-radius: 4px 4px 0 0; background: linear-gradient(#FBF1BE, #F0DA85); color: #222; font: inherit; font-size: 16px; outline: none; }
.ph-wg__notebar { display: flex; justify-content: space-around; height: 40px; margin: 0 8px; border-radius: 0 0 12px 12px; background: linear-gradient(#2A2A2C, #121214); }
.ph-wg__notebar button { flex: 1; padding: 0; border: 0; background: none; color: #fff; cursor: pointer; } .ph-wg__notebar button:disabled { opacity: 0.3; cursor: default; }
/* shortcuts */
.ph-wg__shorts { flex-direction: row; align-items: center; justify-content: space-around; padding: 6px; }
.ph-wg__short { display: flex; flex-direction: column; align-items: center; gap: 2px; padding: 0; border: 0; background: none; color: #fff; font: inherit; font-size: 11px; cursor: pointer; }
/* flip clock: two lit tiles split by a hinge line */
.ph-wg__flip { height: 100%; display: grid; grid-template-columns: 1fr 1fr; grid-template-rows: 1fr auto; gap: 8px; }
.ph-wg__tile { position: relative; display: grid; place-items: center; border-radius: 14px; border: 1.5px solid #000; font-size: 72px; font-weight: 300;
  background: linear-gradient(rgba(255, 255, 255, 0.12), rgba(255, 255, 255, 0) 50%, rgba(0, 0, 0, 0.3) 50%), linear-gradient(#3A3B3F, #151518); }
.ph-wg__tile::after { content: ''; position: absolute; left: 0; right: 0; top: 50%; height: 1px; background: rgba(0, 0, 0, 0.8); }
.ph-wg__flipdate { grid-column: span 2; text-align: center; }
/* clock in words */
.ph-wg__word { height: 100%; display: flex; flex-direction: column; justify-content: center; padding: 0 8px; text-shadow: 0 1px 3px rgba(0, 0, 0, 0.6); }
.ph-wg__weather { position: relative; height: 100%; }
.ph-wg__wxbody { position: absolute; inset: 26px 0 0; display: flex; flex-direction: row; height: auto; padding: 0; }
.ph-wg__wxnow { flex: 1.25; display: flex; flex-direction: column; min-width: 0; }
.ph-wg__wxt { flex: 1; display: flex; align-items: flex-end; gap: 10px; padding-left: 12px; }
.ph-wg__wxt span { font-size: 44px; font-weight: 300; line-height: 1; } .ph-wg__wxt small { margin-bottom: 4px; font-size: 15px; opacity: 0.75; }
.ph-wg__wxplace { height: 32px; margin: 6px; border-radius: 4px; border: 1px solid rgba(255, 255, 255, 0.25); color: inherit; font: inherit; font-size: 15px;
  background: linear-gradient(rgba(64, 65, 68, 0.88), rgba(21, 21, 23, 0.88)); cursor: pointer; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ph-wg__wxday { flex: 0.75; display: flex; flex-direction: column; align-items: center; justify-content: center; font-size: 14px; }
.ph-wg__wxicon { position: absolute; top: 0; left: 56px; color: #fff; filter: drop-shadow(0 3px 3px rgba(0, 0, 0, 0.5)); }
.ph-wg__word b { font-size: 40px; font-weight: 300; line-height: 1.05; } .ph-wg__word span { font-size: 40px; font-weight: 600; line-height: 1.05; }
/* music (Belle small and large) */
.ph-wg__music { position: relative; height: 100%; display: flex; align-items: flex-end; gap: 12px; padding: 0 10px 10px; color: #fff; }
.ph-wg__mpanel { position: absolute; left: 0; right: 0; bottom: 0; top: 44px; border-radius: 12px; border: 1px solid #000;
  background: linear-gradient(#3C3C40, #18181A 22%, #0B0B0C 70%, #1A1A1C); box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.25); }
.ph-wg__mart, .ph-wg__mlart { position: relative; flex: none; padding: 0; border: 0; background: none; cursor: pointer; box-shadow: 0 2px 6px rgba(0, 0, 0, 0.6); }
.ph-wg__mart { width: 124px; height: 124px; }
.ph-wg__mempty { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px; width: 124px; height: 124px;
  background: radial-gradient(circle at 50% 40%, #2F5FD0, #0D2A82 70%, #061650); color: #fff; font-size: 15px; }
.ph-wg__mempty svg { filter: drop-shadow(0 1px 1px rgba(255, 255, 255, 0.5)); }
.ph-wg__mempty.is-large { width: 150px; height: 150px; }
.ph-wg__mside { position: relative; flex: 1; min-width: 0; display: flex; flex-direction: column; align-items: center; gap: 4px; }
.ph-wg__mtitle { max-width: 100%; font-size: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ph-wg__mctl { display: flex; align-items: center; justify-content: space-around; width: 100%; height: 46px; padding: 0 6px; border-radius: 12px;
  background: linear-gradient(#050506, #141416); box-shadow: inset 0 2px 4px rgba(0, 0, 0, 0.9), 0 1px 0 rgba(255, 255, 255, 0.12); }
.ph-wg__mbtn { display: grid; place-items: center; width: 30px; height: 30px; padding: 0; border: 0; color: #fff; cursor: pointer;
  background: linear-gradient(#4A4A4F, #1E1E21 50%, #0C0C0D); }
.ph-wg__mbtn.is-big { width: 38px; height: 38px; }
.ph-wg__mbtn.is-l { width: 38px; height: 38px; } .ph-wg__mbtn.is-l.is-big { width: 50px; height: 50px; }
.ph-wg__mbtn.is-dim { opacity: 0.55; }
.ph-wg__mlarge { position: relative; height: 100%; display: flex; flex-direction: column; align-items: center; color: #fff; }
.ph-wg__mtray { position: absolute; left: 0; right: 0; bottom: 0; height: 52%; border-radius: 14px; border: 1px solid #000;
  background: linear-gradient(#3C3C40, #18181A 18%, #0B0B0C 60%, #1A1A1C); box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.25); }
.ph-wg__mlart { width: 150px; height: 150px; margin-top: 4px; }
.ph-wg__mlt { position: relative; display: flex; gap: 10px; align-items: baseline; max-width: 100%; margin-top: 4px; font-size: 14px; white-space: nowrap; overflow: hidden; }
.ph-wg__mlt u { color: #4A9BFF; font-size: 13px; text-decoration: none; }
.ph-wg__mlctl { position: relative; margin-top: auto; margin-bottom: 10px; display: flex; align-items: center; gap: 18px; }
</style>
