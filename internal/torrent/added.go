package torrent

import (
	"time"

	dlog "github.com/Apollogeddon/distribyted/internal/log"
)

// linkTimes is the part of the database that keeps when each link was made (loader.DB). The
// Service works without it, as with the test databases that don't keep them.
type linkTimes interface {
	SetLinkAdded(newpath string, t time.Time) error
	ForgetLinkAdded(newpath string) error
	LinksAdded() (map[string]time.Time, error)
}

func (s *Service) linkTimes() linkTimes {
	lt, _ := s.db.(linkTimes)
	return lt
}

func (s *Service) linkAdded(p string, t time.Time) {
	if lt := s.linkTimes(); lt != nil {
		if err := lt.SetLinkAdded(p, t); err != nil {
			s.log.Warn().Err(err).Str(dlog.KeyPath, p).Msg("problem recording when the link was made")
		}
	}
}

func (s *Service) forgetLinkAdded(p string) {
	if lt := s.linkTimes(); lt != nil {
		_ = lt.ForgetLinkAdded(p)
	}
}

func (s *Service) moveLinkAdded(oldpath, newpath string) {
	lt := s.linkTimes()
	if lt == nil {
		return
	}
	all, err := lt.LinksAdded()
	if err != nil {
		return
	}
	if t, ok := all[oldpath]; ok {
		_ = lt.SetLinkAdded(newpath, t)
		_ = lt.ForgetLinkAdded(oldpath)
	}
}

// LinksAddedSince lists the links made since t, by path, with when each was made.
func (s *Service) LinksAddedSince(t time.Time) (map[string]time.Time, error) {
	lt := s.linkTimes()
	if lt == nil {
		return nil, nil
	}
	all, err := lt.LinksAdded()
	if err != nil {
		return nil, err
	}
	for p, at := range all {
		if at.Before(t) {
			delete(all, p)
		}
	}
	return all, nil
}

// ForgetLinksAddedBefore drops the times of links made before t, which the dashboard no
// longer lists.
func (s *Service) ForgetLinksAddedBefore(t time.Time) {
	lt := s.linkTimes()
	if lt == nil {
		return
	}
	all, err := lt.LinksAdded()
	if err != nil {
		return
	}
	for p, at := range all {
		if at.Before(t) {
			_ = lt.ForgetLinkAdded(p)
		}
	}
}
