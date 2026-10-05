package store

import "time"

// SetNow replaces the clock repeats measure completion dates against.
func (s *Store) SetNow(now func() time.Time) {
	s.now = now
}
