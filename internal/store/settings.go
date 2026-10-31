// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package store

// Server-wide settings (migration 3): one text value per name, for features with a little configuration of their own
// (the weather source). Secrets go in sealed (connectors.Vault) before they get here.

// Setting returns the value stored under name, or "" when there is none.
func (s *Store) Setting(name string) string {
	var v string
	if err := s.db.QueryRow(`SELECT COALESCE(value,'') FROM server_settings WHERE name=?`, name).Scan(&v); err != nil {
		return ""
	}
	return v
}

// SetSetting stores value under name (replacing what was there).
func (s *Store) SetSetting(name, value string) error {
	_, err := s.db.Exec(`INSERT INTO server_settings(name,value) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET value=excluded.value`, name, value)
	return err
}
