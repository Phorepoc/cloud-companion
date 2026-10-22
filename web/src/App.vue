<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-layout view="hHh lpR fFf">
    <q-header elevated class="bg-primary text-white">
      <q-toolbar>
        <q-icon name="cloud" size="sm" class="q-mr-sm" />
        <q-toolbar-title>Phecloud</q-toolbar-title>
        <q-chip dense :color="state.status === 'online' ? 'green' : 'grey'" text-color="white" data-test="status">{{ state.status }}</q-chip>
      </q-toolbar>
      <q-tabs v-if="creds" align="left" dense>
        <q-route-tab to="/browse" label="Browser" icon="public" />
        <q-route-tab v-if="state.admin" to="/devices" label="Devices" icon="devices" data-test="tab-devices" />
        <q-route-tab to="/settings" label="Settings" icon="tune" data-test="tab-settings" />
      </q-tabs>
    </q-header>
    <q-page-container>
      <router-view />
    </q-page-container>
  </q-layout>
</template>

<script setup>
import { state, creds } from './cloud.js'
</script>
